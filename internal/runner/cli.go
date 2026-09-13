package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"workforce.local/platform/internal/connectors"
	"workforce.local/platform/internal/platform"
)

// Public failure codes. A user-visible field must never carry harness output,
// stderr or backend text — only one of these.
const (
	FailHarnessUnavailable = "harness_unavailable"
	FailTimeout            = "timeout"
	FailOutputOverflow     = "output_overflow"
	FailInvalidOutput      = "invalid_output"
	FailNonzeroExit        = "nonzero_exit"
	FailInternal           = "internal_error"
)

const maxCLIOperations = 8

// cliInstructions is the exact output contract handed to the harness. It is part of
// the protocol, not decoration: a harness that does not follow it is rejected.
const cliInstructions = `You are preparing work for human review. You cannot execute anything.

Reply on stdout with ONE JSON object and nothing else, in exactly one of these shapes:

{"status":"succeeded","summary":"<short description>","operations":[{"integration":"<name>","action":"<name>","business_key":"<stable id>","target_id":"<record id>","expected_version":<int>,"payload":{...}}]}

{"status":"waiting","summary":"<why you are asking>","gate":{"kind":"clarification|selection|missing_information","prompt":"<question>","input_schema":{"properties":{"field":{"type":"string|integer|number|boolean"}},"required":["field"]}}}

{"status":"failed","failure_reason":"<short public code>"}

Rules:
- Propose work only against the records supplied in "records"; never invent an id.
- expected_version must be the version you were given for that record.
- business_key must identify the REAL-WORLD operation being proposed (for example
  "supplier-followup:ACME-2026-0042"), so the same real operation cannot be applied
  twice. It must NOT contain this run's id: a run-scoped key defeats duplicate
  detection and will be rejected.
- If you need information you do not have, reply "waiting" with a gate. Do not guess.
- You cannot approve, endorse or execute anything; a human does that.
- Write nothing but the JSON object to stdout. Diagnostics may go to stderr.`

type cliRunner struct {
	id        string
	command   []string
	timeout   time.Duration
	maxOutput int64
	envAllow  []string
}

func (c cliRunner) Manifest() Manifest {
	bin := ""
	if len(c.command) > 0 {
		bin = filepath.Base(c.command[0])
	}
	return Manifest{
		ID: c.id, Name: "Configured harness (" + bin + ")", Kind: "harness", Version: "cli",
		// A configured external process is not a simulation.
		Simulation:    false,
		Capabilities:  []string{"prepare_proposal", "request_input"},
		MaxOperations: maxCLIOperations,
		Command:       bin,
	}
}

// cliConfigs reads operator configuration. Runner definitions live entirely in the
// environment so that no executable path is compiled in or stored in the registry:
// an "active" release means the operator promoted metadata, not that we installed or
// trust any code.
func cliConfigs() []cliRunner {
	ids := splitList(os.Getenv("WORKFORCE_RUNNER_CLI_IDS"))
	out := make([]cliRunner, 0, len(ids))
	for _, id := range ids {
		key := strings.ToUpper(strings.ReplaceAll(id, "-", "_"))
		words, err := splitWords(os.Getenv("WORKFORCE_RUNNER_CLI_" + key + "_COMMAND"))
		if err != nil || len(words) == 0 {
			continue
		}
		r := cliRunner{
			id: id, command: words,
			timeout:   120 * time.Second,
			maxOutput: 1 << 20,
			envAllow:  splitList(os.Getenv("WORKFORCE_RUNNER_CLI_" + key + "_ENV")),
		}
		if raw := os.Getenv("WORKFORCE_RUNNER_CLI_" + key + "_TIMEOUT"); raw != "" {
			if d, e := time.ParseDuration(raw); e == nil && d > 0 {
				if d > 10*time.Minute {
					d = 10 * time.Minute
				}
				r.timeout = d
			}
		}
		if raw := os.Getenv("WORKFORCE_RUNNER_CLI_" + key + "_MAXOUTPUT"); raw != "" {
			if n, e := parseBytes(raw); e == nil && n > 0 {
				r.maxOutput = n
			}
		}
		out = append(out, r)
	}
	return out
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseBytes(s string) (int64, error) {
	var n int64
	_, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n)
	return n, err
}

// splitWords splits a command line into words honouring quotes and backslash escapes.
// It performs NO expansion: no variables, no globbing, no shell. Operator input is
// split here and passed to exec as argv, so task data can never reach a shell.
func splitWords(s string) ([]string, error) {
	runes := []rune(s)
	var words []string
	var cur strings.Builder
	inWord := false
	var quote rune
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\\':
			// Consume the next rune literally, so "\ " is a space inside a word.
			if i+1 >= len(runes) {
				return nil, errors.New("trailing backslash in command")
			}
			i++
			cur.WriteRune(runes[i])
			inWord = true
		case r == '\'' || r == '"':
			quote = r
			inWord = true
		case r == ' ' || r == '\t' || r == '\n':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, errors.New("unbalanced quote in command")
	}
	if inWord {
		words = append(words, cur.String())
	}
	for _, w := range words {
		if w == "" {
			return nil, errors.New("empty word in command")
		}
	}
	return words, nil
}

// capWriter bounds captured output. On overflow it signals so the caller can kill
// the process group rather than buffering an unbounded stream.
type capWriter struct {
	mu         sync.Mutex
	buf        bytes.Buffer
	limit      int64
	overflowed bool
	onOverflow func()
}

func (w *capWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	if w.overflowed {
		w.mu.Unlock()
		return len(p), nil
	}
	remaining := w.limit - int64(w.buf.Len())
	if remaining <= 0 {
		w.overflowed = true
		cb := w.onOverflow
		w.mu.Unlock()
		if cb != nil {
			cb()
		}
		return len(p), nil
	}
	if int64(len(p)) > remaining {
		w.buf.Write(p[:remaining])
		w.overflowed = true
		cb := w.onOverflow
		w.mu.Unlock()
		if cb != nil {
			cb()
		}
		return len(p), nil
	}
	w.buf.Write(p)
	w.mu.Unlock()
	return len(p), nil
}

func (w *capWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func (w *capWriter) Overflowed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.overflowed
}

type cliInputDoc struct {
	OrgID        string      `json:"org_id"`
	TaskID       string      `json:"task_id"`
	RunID        string      `json:"run_id"`
	AgentID      string      `json:"agent_id"`
	Harness      string      `json:"harness"`
	Intent       string      `json:"intent"`
	Records      []RecordRef `json:"records"`
	Inputs       []Input     `json:"inputs"`
	Instructions string      `json:"instructions"`
}

type cliOp struct {
	Integration     string          `json:"integration"`
	Action          string          `json:"action"`
	BusinessKey     string          `json:"business_key"`
	TargetID        string          `json:"target_id"`
	ExpectedVersion int             `json:"expected_version"`
	Payload         json.RawMessage `json:"payload"`
}

type cliGate struct {
	Kind        string          `json:"kind"`
	Prompt      string          `json:"prompt"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type cliReply struct {
	Status        string   `json:"status"`
	Summary       string   `json:"summary"`
	FailureReason string   `json:"failure_reason"`
	Operations    []cliOp  `json:"operations"`
	Gate          *cliGate `json:"gate"`
}

func (c cliRunner) Run(ctx context.Context, req Request) (Result, error) {
	bin, err := exec.LookPath(c.command[0])
	if err != nil {
		// Fail closed and visibly: an unpromoted/uninstalled harness must not look
		// like a business failure, and must never fall back to the simulator.
		return Result{Status: StatusFailed, FailureReason: FailHarnessUnavailable}, nil
	}

	workRoot := os.Getenv("WORKFORCE_RUNNER_WORK_ROOT")
	if workRoot == "" {
		workRoot = filepath.Join(os.TempDir(), "workforce-runs")
	}
	if err := os.MkdirAll(workRoot, 0o700); err != nil {
		return Result{Status: StatusFailed, FailureReason: FailInternal}, nil
	}
	scratch, err := os.MkdirTemp(workRoot, "run-")
	if err != nil {
		return Result{Status: StatusFailed, FailureReason: FailInternal}, nil
	}
	defer os.RemoveAll(scratch)

	doc, _ := json.Marshal(cliInputDoc{
		OrgID: req.OrgID, TaskID: req.TaskID, RunID: req.RunID, AgentID: req.AgentID,
		Harness: req.Harness, Intent: req.Intent, Records: req.Records, Inputs: req.Inputs,
		Instructions: cliInstructions,
	})

	runCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	cmd := exec.Command(bin, c.command[1:]...)
	cmd.Dir = scratch
	// Explicit allowlist only. Never os.Environ(): that would hand the harness the
	// developer's HOME, SSH agent, git credentials and our database URL.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + scratch}
	for _, name := range c.envAllow {
		if v, ok := os.LookupEnv(name); ok {
			cmd.Env = append(cmd.Env, name+"="+v)
		}
	}
	cmd.Stdin = bytes.NewReader(doc)
	// Own process group so cancellation and timeout kill the whole tree, not just
	// the direct child.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	killGroup := func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}
	out := &capWriter{limit: c.maxOutput, onOverflow: killGroup}
	errBuf := &capWriter{limit: 8 << 10}
	cmd.Stdout = out
	cmd.Stderr = errBuf

	if err := cmd.Start(); err != nil {
		return Result{Status: StatusFailed, FailureReason: FailHarnessUnavailable}, nil
	}

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()

	var werr error
	select {
	case werr = <-waitErr:
	case <-runCtx.Done():
		killGroup()
		<-waitErr
		if out.Overflowed() {
			return Result{Status: StatusFailed, FailureReason: FailOutputOverflow}, nil
		}
		return Result{Status: StatusFailed, FailureReason: FailTimeout}, nil
	}

	if out.Overflowed() {
		killGroup()
		return Result{Status: StatusFailed, FailureReason: FailOutputOverflow}, nil
	}
	if werr != nil {
		// stderr is deliberately discarded: it is diagnostics and may contain
		// provider text that must never reach a user-visible field.
		return Result{Status: StatusFailed, FailureReason: FailNonzeroExit}, nil
	}

	var reply cliReply
	if err := json.Unmarshal(bytes.TrimSpace([]byte(out.String())), &reply); err != nil {
		return Result{Status: StatusFailed, FailureReason: FailInvalidOutput}, nil
	}

	switch reply.Status {
	case StatusSucceeded:
		draft, perr := c.toDraft(reply, req.RunID)
		if perr != nil {
			return Result{Status: StatusFailed, FailureReason: FailInvalidOutput}, nil
		}
		return Result{Status: StatusSucceeded, Summary: reply.Summary, Draft: draft}, nil
	case StatusWaiting:
		if reply.Gate == nil || strings.TrimSpace(reply.Gate.Prompt) == "" {
			return Result{Status: StatusFailed, FailureReason: FailInvalidOutput}, nil
		}
		return Result{
			Status:  StatusWaiting,
			Summary: reply.Summary,
			Gate: &GateRequest{
				Kind: normalizeGateKind(reply.Gate.Kind), Prompt: reply.Gate.Prompt,
				InputSchema: reply.Gate.InputSchema,
			},
		}, nil
	case StatusFailed:
		reason := strings.TrimSpace(reply.FailureReason)
		if reason == "" {
			reason = FailInvalidOutput
		}
		return Result{Status: StatusFailed, FailureReason: sanitizeReason(reason)}, nil
	default:
		return Result{Status: StatusFailed, FailureReason: FailInvalidOutput}, nil
	}
}

// toDraft converts untrusted harness operations into our typed form. Anything
// malformed is rejected here; the proposal layer validates them again.
func (c cliRunner) toDraft(reply cliReply, runID string) (*ProposalDraft, error) {
	if len(reply.Operations) == 0 {
		return nil, errors.New("no operations")
	}
	if len(reply.Operations) > maxCLIOperations {
		return nil, errors.New("too many operations")
	}
	ops := make([]connectors.Operation, 0, len(reply.Operations))
	for _, o := range reply.Operations {
		if strings.TrimSpace(o.Integration) == "" || strings.TrimSpace(o.Action) == "" ||
			strings.TrimSpace(o.TargetID) == "" || strings.TrimSpace(o.BusinessKey) == "" {
			return nil, errors.New("incomplete operation")
		}
		if o.ExpectedVersion < 1 {
			return nil, errors.New("unversioned operation")
		}
		// A run-scoped key would let two independent runs propose the same real
		// operation and both be applied, defeating duplicate detection entirely.
		if runID != "" && strings.Contains(o.BusinessKey, runID) {
			return nil, errors.New("business key must identify the real operation, not this run")
		}
		if !json.Valid(o.Payload) {
			return nil, errors.New("invalid payload")
		}
		ops = append(ops, connectors.Operation{
			ID: platform.NewID(), Integration: o.Integration, Action: o.Action,
			BusinessKey: o.BusinessKey, TargetID: o.TargetID,
			ExpectedVersion: o.ExpectedVersion, Payload: o.Payload,
		})
	}
	summary := strings.TrimSpace(reply.Summary)
	if summary == "" {
		summary = "Prepared by " + c.id
	}
	return &ProposalDraft{Summary: summary, Operations: ops}, nil
}

func normalizeGateKind(k string) string {
	switch k {
	case "clarification", "selection", "missing_information":
		return k
	default:
		return "missing_information"
	}
}

// sanitizeReason keeps a harness-supplied reason to a short public code so raw
// provider diagnostics cannot leak into a user-visible field.
func sanitizeReason(s string) string {
	if len(s) > 64 {
		s = s[:64]
	}
	for _, r := range s {
		ok := r == '_' || r == '-' || r == '.' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return FailInvalidOutput
		}
	}
	return s
}
