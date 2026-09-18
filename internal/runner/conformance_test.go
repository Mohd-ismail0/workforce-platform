package runner

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// twoHarnesses configures BOTH test doubles at once under distinct ids.
//
// One harness proves the execution boundary works. Two harnesses that share
// nothing but the protocol are what show the boundary is a CONTRACT rather than
// one adapter's shape: a second double that differed only by a mode flag would
// have proved nothing, because a mode cannot expose a baked-in assumption about
// the envelope, the question shape or the process tree.
func twoHarnesses(t *testing.T, modeA, modeB string, extraEnvB ...string) (Runner, Runner) {
	t.Helper()
	a, err := filepath.Abs(filepath.Join("testdata", "harness.py"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := filepath.Abs(filepath.Join("testdata", "harness_b.py"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{a, b} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("testdata harness missing: %v", err)
		}
	}
	t.Setenv("WORKFORCE_RUNNER_ALLOW_UNISOLATED", "1")
	t.Setenv("WORKFORCE_RUNNER_WORK_ROOT", t.TempDir())
	t.Setenv("WORKFORCE_RUNNER_CLI_IDS", "alpha,beta")
	t.Setenv("WORKFORCE_RUNNER_CLI_ALPHA_COMMAND", "python3 "+a)
	t.Setenv("WORKFORCE_RUNNER_CLI_ALPHA_TIMEOUT", "3s")
	t.Setenv("WORKFORCE_RUNNER_CLI_ALPHA_ENV", "HARNESS_MODE")
	t.Setenv("WORKFORCE_RUNNER_CLI_BETA_COMMAND", "python3 "+b)
	t.Setenv("WORKFORCE_RUNNER_CLI_BETA_TIMEOUT", "3s")
	t.Setenv("WORKFORCE_RUNNER_CLI_BETA_ENV", strings.Join(append([]string{"HARNESS_MODE_B"}, extraEnvB...), ","))
	t.Setenv("HARNESS_MODE", modeA)
	t.Setenv("HARNESS_MODE_B", modeB)

	ra, err := New("alpha")
	if err != nil {
		t.Fatalf("alpha not discoverable: %v", err)
	}
	rb, err := New("beta")
	if err != nil {
		t.Fatalf("beta not discoverable: %v", err)
	}
	return ra, rb
}

// Both harnesses must produce the same protocol shape through the same boundary,
// despite nothing about them being alike.
func TestBothHarnessesExecuteTheSameContract(t *testing.T) {
	ctx := context.Background()
	ra, rb := twoHarnesses(t, "success", "success")

	for name, r := range map[string]Runner{"alpha": ra, "beta": rb} {
		res, err := r.Run(ctx, sampleRequest())
		if err != nil {
			t.Fatalf("%s: run failed: %v", name, err)
		}
		if res.Status != StatusSucceeded {
			t.Fatalf("%s: status %q want succeeded (%s)", name, res.Status, res.FailureReason)
		}
		if res.Draft == nil {
			t.Fatalf("%s: succeeded with no draft", name)
		}
		ops := res.Draft.Operations
		if len(ops) != 1 {
			t.Fatalf("%s: %d operations want 1", name, len(ops))
		}
		op := ops[0]
		if op.Integration != "mail" || op.Action != "send" {
			t.Fatalf("%s: unexpected operation %s.%s", name, op.Integration, op.Action)
		}
		// The protocol's non-negotiable field: a BUSINESS-FACT key, so two
		// independent runs proposing the same real-world action collide. A
		// harness cannot opt out of that by being written differently.
		if op.BusinessKey == "" {
			t.Fatalf("%s: draft has no business_key", name)
		}
		if strings.Contains(op.BusinessKey, "run-abc") {
			t.Fatalf("%s: business_key leaked the run id, which defeats duplicate detection: %q", name, op.BusinessKey)
		}
	}
}

// The second harness wraps the protocol document TWICE. Unwrapping that only
// handled one layer would have looked like a general solution against the first
// double and been wrong.
func TestSecondHarnessNestedEnvelopeIsUnwrapped(t *testing.T) {
	_, rb := twoHarnesses(t, "success", "success")
	res, err := rb.Run(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if res.Status != StatusSucceeded || res.Draft == nil {
		t.Fatalf("a doubly-nested envelope was not unwrapped: status=%q draft=%v", res.Status, res.Draft)
	}
	if !strings.Contains(res.Draft.Summary, "stock_mismatch") {
		t.Fatalf("unexpected summary from the nested envelope: %q", res.Draft.Summary)
	}
}

// A different QUESTION shape must survive the same gate path. The first double
// asks for a string and an integer; this one asks for a named choice.
func TestSecondHarnessAsksADifferentGate(t *testing.T) {
	_, rb := twoHarnesses(t, "success", "selection")
	res, err := rb.Run(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if res.Status != StatusWaiting {
		t.Fatalf("status %q want waiting", res.Status)
	}
	if res.Gate == nil {
		t.Fatal("a waiting result carried no gate")
	}
	if res.Gate.Kind != "selection" {
		t.Fatalf("gate kind %q want selection", res.Gate.Kind)
	}
	schema := string(res.Gate.InputSchema)
	if !strings.Contains(schema, "enum") {
		t.Fatalf("the selection schema did not survive the boundary: %s", schema)
	}
}

// The second harness has NO native continuation: it is invoked fresh and redoes
// the work from the supplied answer. That is the platform's baseline, and the
// run must succeed without anyone pretending in-memory state was resumed.
func TestSecondHarnessResumesWithoutNativeContinuation(t *testing.T) {
	_, rb := twoHarnesses(t, "success", "selection")
	req := sampleRequest()
	req.Inputs = []Input{{Name: "selection", Value: `"price_mismatch"`}}
	res, err := rb.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if res.Status != StatusSucceeded || res.Draft == nil {
		t.Fatalf("a fresh invocation with the answer supplied did not succeed: %q", res.Status)
	}
	if !strings.Contains(res.Draft.Summary, "price_mismatch") {
		t.Fatalf("the supplied answer was not applied: %q", res.Draft.Summary)
	}
}

// A killed run must take its whole process TREE with it. This is the containment
// property that cannot be shown by a harness which never forks: the second double
// spawns a grandchild, and after the timeout that grandchild must be gone.
func TestTimeoutKillsTheWholeProcessTree(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	_, rb := twoHarnesses(t, "success", "children", "HARNESS_B_CHILD_PID_OUT")
	t.Setenv("HARNESS_B_CHILD_PID_OUT", pidFile)
	t.Setenv("WORKFORCE_RUNNER_CLI_BETA_TIMEOUT", "1s")

	res, err := rb.Run(context.Background(), sampleRequest())
	if err == nil && res.Status == StatusSucceeded {
		t.Fatal("a harness that hangs past its timeout was reported as succeeding")
	}

	raw, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatalf("the harness never recorded its child: %v", readErr)
	}
	pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw)))
	if convErr != nil || pid <= 0 {
		t.Fatalf("unreadable child pid %q", raw)
	}
	// SIGKILL to the process group is delivered with the parent's death; give the
	// kernel a moment to reap before insisting.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); err != nil {
			return // ESRCH: the grandchild is gone, which is the point.
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("the harness's child (pid %d) survived the run being killed: the process tree was not contained", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
