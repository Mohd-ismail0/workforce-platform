package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeRunner configures a CLI runner that launches a real subprocess harness.
func fakeRunner(t *testing.T, mode string, extraEnv ...string) (Runner, string) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("testdata", "harness.py"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("testdata harness missing: %v", err)
	}
	workRoot := t.TempDir()
	// These tests deliberately exercise the UNISOLATED path, so they acknowledge it.
	t.Setenv("WORKFORCE_RUNNER_ALLOW_UNISOLATED", "1")
	t.Setenv("WORKFORCE_RUNNER_WORK_ROOT", workRoot)
	t.Setenv("WORKFORCE_RUNNER_CLI_IDS", "fake")
	t.Setenv("WORKFORCE_RUNNER_CLI_FAKE_COMMAND", "python3 "+script)
	t.Setenv("WORKFORCE_RUNNER_CLI_FAKE_TIMEOUT", "3s")
	t.Setenv("HARNESS_MODE", mode)
	t.Setenv("WORKFORCE_RUNNER_CLI_FAKE_ENV", strings.Join(append([]string{"HARNESS_MODE"}, extraEnv...), ","))

	r, err := New("fake")
	if err != nil {
		t.Fatalf("configured runner not discoverable: %v", err)
	}
	return r, workRoot
}

func sampleRequest() Request {
	return Request{
		OrgID: "org-1", TaskID: "task-1", RunID: "run-abc", AgentID: "agent-1",
		Harness: "fake", Intent: "send supplier follow-up",
		Records: []RecordRef{{ID: "mail-1", Integration: "mail", Version: 3, Data: map[string]any{}}},
	}
}

func TestCLIRunnerIsAdvertisedAndBinds(t *testing.T) {
	fakeRunner(t, "success")

	m := Manifest{}
	for _, a := range Available() {
		if a.ID == "fake" {
			m = a
		}
	}
	if m.ID == "" {
		t.Fatal("configured runner is not advertised by Available()")
	}
	if m.Simulation {
		t.Fatal("a configured external process must not be advertised as a simulation")
	}
	if m.Command == "" {
		t.Fatal("manifest should expose the configured binary name for diagnostics")
	}
	// Only the binary name may be exposed, never the full argument list.
	if strings.Contains(m.Command, " ") || strings.Contains(m.Command, "testdata") {
		t.Fatalf("manifest leaked command arguments: %q", m.Command)
	}
	if !Supported("fake") {
		t.Fatal("Supported() must accept a configured runner")
	}
	if Supported("claude-code") {
		t.Fatal("Supported() must not claim a runner that is not configured")
	}
	// With no configuration the simulator must remain the only runner.
	for _, id := range []string{"WORKFORCE_RUNNER_CLI_IDS", "WORKFORCE_RUNNER_CLI_FAKE_COMMAND"} {
		os.Unsetenv(id)
	}
	if len(Available()) != 1 || Available()[0].ID != "simulator" {
		t.Fatalf("unconfigured default must be simulator only, got %v", Available())
	}
}

func TestCLIRunnerParsesDraftFromRealSubprocess(t *testing.T) {
	r, _ := fakeRunner(t, "success")
	res, err := r.Run(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusSucceeded || res.Draft == nil {
		t.Fatalf("status=%q draft=%v", res.Status, res.Draft)
	}
	if len(res.Draft.Operations) != 1 {
		t.Fatalf("operations=%d", len(res.Draft.Operations))
	}
	op := res.Draft.Operations[0]
	if op.Integration != "mail" || op.Action != "send" || op.TargetID != "mail-1" {
		t.Fatalf("unexpected operation: %+v", op)
	}
	if op.ExpectedVersion != 3 {
		t.Fatalf("expected_version=%d, want the scoped record version 3", op.ExpectedVersion)
	}
	// The human-facing duplicate guard depends on a business-fact key.
	if !strings.HasPrefix(op.BusinessKey, "supplier-followup:") {
		t.Fatalf("business key should identify the real operation, got %q", op.BusinessKey)
	}
	if strings.Contains(op.BusinessKey, "run-abc") {
		t.Fatalf("business key must not be run-scoped: %q", op.BusinessKey)
	}
}

func TestCLIRunnerRejectsRunScopedBusinessKey(t *testing.T) {
	r, _ := fakeRunner(t, "runscoped")
	res, err := r.Run(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusFailed || res.FailureReason != FailInvalidOutput {
		t.Fatalf("a run-scoped business key must be rejected, got %q/%q", res.Status, res.FailureReason)
	}
	if res.Draft != nil {
		t.Fatal("no draft may survive a rejected business key")
	}
}

func TestCLIRunnerWaitingProducesGate(t *testing.T) {
	r, _ := fakeRunner(t, "waiting")
	res, err := r.Run(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusWaiting || res.Gate == nil {
		t.Fatalf("status=%q gate=%v", res.Status, res.Gate)
	}
	if res.Gate.Prompt == "" {
		t.Fatal("gate must ask a question")
	}
	if res.Draft != nil {
		t.Fatal("a waiting run must not produce a draft")
	}
	var schema struct {
		Required []string `json:"required"`
	}
	if json.Unmarshal(res.Gate.InputSchema, &schema) != nil || len(schema.Required) == 0 {
		t.Fatalf("gate must carry a usable input schema: %s", res.Gate.InputSchema)
	}
}

func TestCLIRunnerStrictOutputErrors(t *testing.T) {
	cases := []struct {
		mode string
		want string
	}{
		{"garbage", FailInvalidOutput},
		{"nonzero", FailNonzeroExit},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			r, _ := fakeRunner(t, c.mode)
			res, err := r.Run(context.Background(), sampleRequest())
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if res.Status != StatusFailed || res.FailureReason != c.want {
				t.Fatalf("got %q/%q, want failed/%s", res.Status, res.FailureReason, c.want)
			}
			// Diagnostics must never leak into a user-visible field.
			if strings.Contains(res.FailureReason, "exploded") || strings.Contains(res.FailureReason, "stderr") {
				t.Fatalf("failure_reason leaked harness diagnostics: %q", res.FailureReason)
			}
		})
	}
}

func TestCLIRunnerTimeoutKillsProcessAndCleansUp(t *testing.T) {
	r, workRoot := fakeRunner(t, "slow")
	// Override the timeout to something short so the test is quick.
	t.Setenv("WORKFORCE_RUNNER_CLI_FAKE_TIMEOUT", "1s")
	r2, err := New("fake")
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	res, err := r2.Run(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusFailed || res.FailureReason != FailTimeout {
		t.Fatalf("got %q/%q, want failed/%s", res.Status, res.FailureReason, FailTimeout)
	}
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Fatalf("timeout did not interrupt the child; took %s", elapsed)
	}
	// The per-run scratch directory must not survive the run.
	entries, err := os.ReadDir(workRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		var left []string
		for _, e := range entries {
			left = append(left, e.Name())
		}
		t.Fatalf("scratch directories leaked: %v", left)
	}
	_ = r
}

func TestCLIRunnerOutputCapKillsProcess(t *testing.T) {
	r, _ := fakeRunner(t, "overflow")
	t.Setenv("WORKFORCE_RUNNER_CLI_FAKE_MAXOUTPUT", "65536")
	r2, err := New("fake")
	if err != nil {
		t.Fatal(err)
	}
	res, err := r2.Run(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusFailed || res.FailureReason != FailOutputOverflow {
		t.Fatalf("got %q/%q, want failed/%s", res.Status, res.FailureReason, FailOutputOverflow)
	}
	_ = r
}

func TestCLIRunnerMissingBinaryFailsClosed(t *testing.T) {
	t.Setenv("WORKFORCE_RUNNER_CLI_IDS", "ghost")
	t.Setenv("WORKFORCE_RUNNER_CLI_GHOST_COMMAND", "workforce-no-such-binary-xyz")
	t.Setenv("WORKFORCE_RUNNER_ALLOW_UNISOLATED", "1")
	r, err := New("ghost")
	if err != nil {
		t.Fatal(err)
	}
	res, err := r.Run(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusFailed || res.FailureReason != FailHarnessUnavailable {
		t.Fatalf("got %q/%q, want failed/%s", res.Status, res.FailureReason, FailHarnessUnavailable)
	}
	if res.Draft != nil {
		t.Fatal("an unavailable harness must never fall back to producing work")
	}
}

// TestCLIRunnerEnvironmentIsIsolated is the security claim, tested rather than
// asserted: the harness must not inherit our process's environment.
func TestCLIRunnerEnvironmentIsIsolated(t *testing.T) {
	out := filepath.Join(t.TempDir(), "env.json")
	t.Setenv("DATABASE_URL", "postgres://test-user:not-a-real-secret@db.invalid:5432/workforce")
	t.Setenv("ANTHROPIC_API_KEY", "sk-should-not-be-inherited")
	t.Setenv("HARNESS_ENV_OUT", out)

	r, _ := fakeRunner(t, "envdump", "HARNESS_ENV_OUT")
	res, err := r.Run(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusSucceeded {
		t.Fatalf("envdump run: %q/%q", res.Status, res.FailureReason)
	}

	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("harness did not record its environment: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"DATABASE_URL", "ANTHROPIC_API_KEY", "MIGRATION_DATABASE_URL"} {
		if _, present := got[forbidden]; present {
			t.Fatalf("child inherited %s — the environment is not isolated", forbidden)
		}
	}
	if got["HOME"] == "" {
		t.Fatal("child should get a scratch HOME")
	}
	if got["HOME"] == os.Getenv("HOME") {
		t.Fatalf("child inherited the developer HOME (%q)", got["HOME"])
	}
	if !strings.HasSuffix(got["HOME"], "run-") && !strings.Contains(got["HOME"], "workforce-runs") && !strings.Contains(got["HOME"], "Test") {
		// The scratch dir is removed after the run, so assert it looks like ours.
		t.Logf("child HOME was %q", got["HOME"])
	}
}

// TestCLIRunnerSendsInstructionsAndRecords verifies the harness actually receives the
// context and the output contract, so the protocol is real and not assumed.
func TestCLIRunnerSendsInstructionsAndRecords(t *testing.T) {
	out := filepath.Join(t.TempDir(), "stdin.json")
	t.Setenv("HARNESS_STDIN_OUT", out)
	r, _ := fakeRunner(t, "stdin", "HARNESS_STDIN_OUT")
	if _, err := r.Run(context.Background(), sampleRequest()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("harness did not record stdin: %v", err)
	}
	var doc cliInputDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.RunID != "run-abc" || doc.TaskID != "task-1" {
		t.Fatalf("run context not delivered: %+v", doc)
	}
	if len(doc.Records) != 1 || doc.Records[0].ID != "mail-1" {
		t.Fatalf("scoped records not delivered: %+v", doc.Records)
	}
	for _, want := range []string{"status", "business_key", "expected_version", "input_schema"} {
		if !strings.Contains(doc.Instructions, want) {
			t.Fatalf("output contract does not mention %q", want)
		}
	}
	// The contract must not offer the harness a way to authorise its own work.
	if !strings.Contains(doc.Instructions, "cannot approve") {
		t.Fatal("contract should state the harness cannot approve or execute")
	}
}

func TestSplitWordsHandlesQuotesAndEscapes(t *testing.T) {
	got, err := splitWords(`claude -p --allowedTools "Read Grep" my\ file`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"claude", "-p", "--allowedTools", "Read Grep", "my file"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("word %d = %q, want %q", i, got[i], want[i])
		}
	}
	for _, bad := range []string{`cmd "unbalanced`, `cmd trailing\`} {
		if _, err := splitWords(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
	// No shell metacharacter may survive as a command word.
	words, err := splitWords("cmd; rm -rf /")
	if err != nil {
		t.Fatal(err)
	}
	if len(words) < 2 || words[0] != "cmd;" {
		t.Fatalf("expected literal words, got %v", words)
	}
}

// TestCLIRunnerSweepsAbandonedScratchDirs covers cleanup after a hard kill: a worker
// killed with SIGKILL never runs its deferred cleanup, so abandoned workspaces must
// be swept rather than accumulating forever.
func TestCLIRunnerSweepsAbandonedScratchDirs(t *testing.T) {
	r, workRoot := fakeRunner(t, "success")

	stale := filepath.Join(workRoot, "run-abandoned")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(workRoot, "run-inflight")
	if err := os.MkdirAll(fresh, 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := r.Run(context.Background(), sampleRequest()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("an abandoned workspace should be swept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("a recent workspace must NOT be swept: it may belong to a live run")
	}
}

// TestCLIRunnerEmitsEmptyArraysNotNull pins the protocol guarantee that cost real
// debugging time: with no inputs, the runner must send [] rather than null, because
// `doc.get("inputs", [])` returns None (not []) when the key exists with a null value,
// and every harness author would otherwise have to special-case it.
func TestCLIRunnerEmitsEmptyArraysNotNull(t *testing.T) {
	out := filepath.Join(t.TempDir(), "stdin.json")
	t.Setenv("HARNESS_STDIN_OUT", out)
	r, _ := fakeRunner(t, "stdin", "HARNESS_STDIN_OUT")

	req := sampleRequest()
	req.Inputs = nil
	req.Records = nil
	if _, err := r.Run(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("harness did not record stdin: %v", err)
	}
	for _, bad := range []string{`"inputs":null`, `"records":null`} {
		if bytes.Contains(raw, []byte(bad)) {
			t.Fatalf("protocol must not publish %s; a harness in another language trips on null", bad)
		}
	}
	for _, want := range []string{`"inputs":[]`, `"records":[]`} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Fatalf("expected %s in the published context, got: %s", want, raw)
		}
	}
}

// TestCLIRunnerFailsClosedWithoutIsolationAcknowledgment is the safety claim, tested:
// a configured harness must NOT launch until an operator explicitly accepts that it
// runs unsandboxed as this account. Without the acknowledgment nothing starts — no
// process, no proposal — and the failure carries a distinct public code.
func TestCLIRunnerFailsClosedWithoutIsolationAcknowledgment(t *testing.T) {
	script, err := filepath.Abs(filepath.Join("testdata", "harness.py"))
	if err != nil {
		t.Fatal(err)
	}
	// Prove the harness WOULD work, so the refusal below is provably about consent
	// and not about a broken configuration.
	t.Setenv("WORKFORCE_RUNNER_ALLOW_UNISOLATED", "1")
	t.Setenv("WORKFORCE_RUNNER_CLI_IDS", "consent")
	t.Setenv("WORKFORCE_RUNNER_CLI_CONSENT_COMMAND", "python3 "+script)
	t.Setenv("WORKFORCE_RUNNER_CLI_CONSENT_ENV", "HARNESS_MODE")
	t.Setenv("HARNESS_MODE", "success")
	t.Setenv("WORKFORCE_RUNNER_WORK_ROOT", t.TempDir())
	ok, err := New("consent")
	if err != nil {
		t.Fatal(err)
	}
	if res, _ := ok.Run(context.Background(), sampleRequest()); res.Status != StatusSucceeded {
		t.Fatalf("harness should work once acknowledged: %q/%q", res.Status, res.FailureReason)
	}

	// Now withdraw consent: the same runner must refuse before launching.
	t.Setenv("WORKFORCE_RUNNER_ALLOW_UNISOLATED", "")
	res, err := ok.Run(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("refusal must be a business outcome, not an error: %v", err)
	}
	if res.Status != StatusFailed || res.FailureReason != FailIsolationRequired {
		t.Fatalf("got %q/%q, want failed/%s", res.Status, res.FailureReason, FailIsolationRequired)
	}
	if res.Draft != nil {
		t.Fatal("nothing may be produced while the boundary is unacknowledged")
	}

	// Consent is a strict allowlist, so near-miss values must NOT be read as consent.
	// (Trailing whitespace IS tolerated: env vars routinely pick it up, and refusing
	// it would be a confusing footgun while adding no safety — an unrelated value
	// still cannot sneak through.)
	for _, notConsent := range []string{"", "0", "yes", "maybe", "false", "2", "no"} {
		t.Setenv("WORKFORCE_RUNNER_ALLOW_UNISOLATED", notConsent)
		if res, _ := ok.Run(context.Background(), sampleRequest()); res.FailureReason != FailIsolationRequired {
			t.Fatalf("%q must not be read as consent (got %q)", notConsent, res.FailureReason)
		}
	}
	// Deliberate forms of consent are honoured, including with surrounding whitespace.
	for _, consent := range []string{"1", "true", "TRUE", "True", " true "} {
		t.Setenv("WORKFORCE_RUNNER_ALLOW_UNISOLATED", consent)
		if res, _ := ok.Run(context.Background(), sampleRequest()); res.Status != StatusSucceeded {
			t.Fatalf("%q should be accepted as consent (got %q/%q)", consent, res.Status, res.FailureReason)
		}
	}
}

// TestCLIRunnerUnwrapsClaudeJSONEnvelope covers the shape `claude -p
// --output-format json` actually prints: one envelope object whose "result" field
// carries the agent's final text. Without unwrapping, configuring a real Claude Code
// harness would be impossible.
func TestCLIRunnerUnwrapsClaudeJSONEnvelope(t *testing.T) {
	r, _ := fakeRunner(t, "claudelike")
	res, err := r.Run(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusSucceeded || res.Draft == nil {
		t.Fatalf("status=%q reason=%q draft=%v", res.Status, res.FailureReason, res.Draft)
	}
	if len(res.Draft.Operations) != 1 {
		t.Fatalf("operations=%d", len(res.Draft.Operations))
	}
	op := res.Draft.Operations[0]
	if op.Integration != "mail" || op.TargetID != "mail-1" || op.ExpectedVersion != 3 {
		t.Fatalf("unexpected operation: %+v", op)
	}
	if op.BusinessKey == "" || strings.Contains(op.BusinessKey, "run-abc") {
		t.Fatalf("business key must identify the real operation: %q", op.BusinessKey)
	}
}

// TestCLIRunnerUnwrapsCodexJSONLStream covers the shape `codex exec --json` prints: a
// stream of events, the last carrying the final message. The stream must be scanned,
// not rejected for having more than one line.
func TestCLIRunnerUnwrapsCodexJSONLStream(t *testing.T) {
	r, _ := fakeRunner(t, "codexlike")
	res, err := r.Run(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusSucceeded || res.Draft == nil {
		t.Fatalf("status=%q reason=%q draft=%v", res.Status, res.FailureReason, res.Draft)
	}
	if op := res.Draft.Operations[0]; op.Integration != "mail" {
		t.Fatalf("unexpected operation: %+v", op)
	}
}

// TestCLIRunnerTreatsErrorEnvelopeAsFailure: a harness that exits 0 but reports its
// own failure must not be mined for a draft, and its diagnostics must not leak.
func TestCLIRunnerTreatsErrorEnvelopeAsFailure(t *testing.T) {
	r, _ := fakeRunner(t, "claudeerror")
	res, err := r.Run(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Status != StatusFailed {
		t.Fatalf("status=%q, want failed", res.Status)
	}
	if res.FailureReason != FailHarnessReportedError {
		t.Fatalf("reason=%q, want %s", res.FailureReason, FailHarnessReportedError)
	}
	if res.Draft != nil {
		t.Fatal("a reported failure must not yield a draft")
	}
	for _, leak := range []string{"authentication", "provider", "session-abc"} {
		if strings.Contains(strings.ToLower(res.FailureReason), leak) {
			t.Fatalf("failure_reason leaked harness diagnostics: %q", res.FailureReason)
		}
	}
}

// TestExtractProtocolShapes pins the parser directly, including the bounded-depth
// guarantee so a hostile envelope cannot recurse without limit.
func TestExtractProtocolShapes(t *testing.T) {
	direct := `{"status":"failed","failure_reason":"no_suitable_record"}`
	if r, err := extractProtocol([]byte(direct)); err != nil || r.FailureReason != "no_suitable_record" {
		t.Fatalf("direct: %v %+v", err, r)
	}
	nested := `{"type":"result","result":"{\"status\":\"waiting\",\"gate\":{\"prompt\":\"q\"}}"}`
	if r, err := extractProtocol([]byte(nested)); err != nil || r.Status != StatusWaiting {
		t.Fatalf("nested string: %v %+v", err, r)
	}
	// The documented TERMINAL field may carry the document as a nested object...
	objField := `{"type":"result","is_error":false,"result":{"status":"failed","failure_reason":"x"}}`
	if r, err := extractProtocol([]byte(objField)); err != nil || r.FailureReason != "x" {
		t.Fatalf("documented terminal field (object form): %v %+v", err, r)
	}
	// ...but a GENERIC name must not be unwrapped. A harness that runs tools emits
	// output/message/text/content constantly, so accepting those risks treating
	// quoted tool output or an early partial message as the authoritative answer.
	generic := `{"output":{"status":"failed","failure_reason":"x"}}`
	if _, err := extractProtocol([]byte(generic)); err == nil {
		t.Fatal("a protocol document inside a generic field must NOT be accepted")
	}
	multi := "{\"type\":\"progress\"}\n" + `{"status":"failed","failure_reason":"last"}`
	if r, err := extractProtocol([]byte(multi)); err != nil || r.FailureReason != "last" {
		t.Fatalf("jsonl last-wins: %v %+v", err, r)
	}
	for _, bad := range []string{"", "   ", "not json", `{"unrelated":true}`} {
		if _, err := extractProtocol([]byte(bad)); err == nil {
			t.Fatalf("%q must not yield a protocol document", bad)
		}
	}
	// Deep nesting must terminate rather than recurse forever.
	deep := `{"status":"failed","failure_reason":"x"}`
	for i := 0; i < 8; i++ {
		deep = `{"result":` + strconv.Quote(deep) + `}`
	}
	if _, err := extractProtocol([]byte(deep)); err != nil {
		// Not finding it is acceptable; hanging or panicking is not.
		t.Logf("deep nesting resolved as no-protocol: %v", err)
	}
}

// TestCLIRunnerRejectsProtocolInGenericFields pins the narrow-unwrapping rule. Only
// vendor-documented terminal fields may be unwrapped: a harness that runs tools emits
// message/text/content constantly, so accepting those risks treating quoted tool
// output or an early partial message as the authoritative final answer.
func TestCLIRunnerRejectsProtocolInGenericFields(t *testing.T) {
	r, _ := fakeRunner(t, "genericnoise")
	res, err := r.Run(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Draft != nil {
		t.Fatal("a protocol document found only in a generic field must NOT be accepted")
	}
	if res.Status != StatusFailed || res.FailureReason != FailInvalidOutput {
		t.Fatalf("got %q/%q, want failed/%s", res.Status, res.FailureReason, FailInvalidOutput)
	}
}

// TestCLIRunnerStreamFailureInvalidatesEarlierSuccess: a stream that appears to
// succeed and then reports an error must not yield the success.
func TestCLIRunnerStreamFailureInvalidatesEarlierSuccess(t *testing.T) {
	r, _ := fakeRunner(t, "streamfail")
	res, err := r.Run(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Draft != nil {
		t.Fatal("a later failure must not be masked by an earlier apparent success")
	}
	if res.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", res.Status)
	}
}

// TestExtractProtocolStreamFailureWins pins the ordering rule directly.
func TestExtractProtocolStreamFailureWins(t *testing.T) {
	ok := `{"status":"succeeded","summary":"s","operations":[{"integration":"mail","action":"send","business_key":"k","target_id":"t","expected_version":1,"payload":{}}]}`
	bad := `{"is_error":true,"terminal_reason":"api_error","result":"boom"}`
	stream := ok + "\n" + bad
	got, err := extractProtocol([]byte(stream))
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if got.Status != StatusFailed {
		t.Fatalf("a later failure must win over an earlier success; got %q", got.Status)
	}
	// Order reversed: failure first, then an apparent success. Failure still wins.
	rev, err := extractProtocol([]byte(bad + "\n" + ok))
	if err != nil {
		t.Fatalf("reversed stream: %v", err)
	}
	if rev.Status != StatusFailed {
		t.Fatalf("a failure anywhere in the stream must win; got %q", rev.Status)
	}
}

// TestExtractProtocolIgnoresGenericFields pins the field allowlist.
func TestExtractProtocolIgnoresGenericFields(t *testing.T) {
	doc := `{"status":"failed","failure_reason":"x"}`
	for _, field := range []string{"message", "text", "content", "output", "last_message"} {
		wrapped := `{"type":"assistant","` + field + `":` + strconv.Quote(doc) + `}`
		if _, err := extractProtocol([]byte(wrapped)); err == nil {
			t.Fatalf("field %q must not be unwrapped as the authoritative answer", field)
		}
	}
	// The documented terminal field IS unwrapped.
	ok := `{"type":"result","subtype":"success","is_error":false,"result":` + strconv.Quote(doc) + `}`
	got, err := extractProtocol([]byte(ok))
	if err != nil || got.FailureReason != "x" {
		t.Fatalf("documented terminal field must be unwrapped: %v %+v", err, got)
	}
}
