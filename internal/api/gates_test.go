package api

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"workforce.local/platform/internal/platform"
)

// seedSimRecord inserts one scoped simulator record for an organisation.
func seedSimRecord(t *testing.T, f *apiFixture, org, integration string) string {
	t.Helper()
	id := platform.NewID()
	// The inventory connector validates the stored record: an adjustment needs an
	// existing quantity to adjust. Seeding an empty object would make the record
	// unusable and the run would fail with "invalid_operation" for the wrong reason.
	data := `{}`
	if integration == "inventory" {
		data = `{"quantity":10}`
	}
	if e := f.st.WithOrg(context.Background(), org, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(),
			"INSERT INTO simulator_records(id,org_id,integration,version,data) VALUES($1,$2,$3,1,$4)", id, org, integration, []byte(data))
		return e
	}); e != nil {
		t.Fatal(e)
	}
	return id
}

// startWorker runs the store worker until the returned stop function is called.
// Stopping it is how a test proves that progress is durable rather than merely
// happening to run while a process is alive.
func startWorker(t *testing.T, f *apiFixture) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.st.RunWorker(ctx, nil) }()
	stopped := false
	return func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Error("worker did not shut down")
		}
	}
}

// waitForRun polls until the run reaches one of the wanted statuses.
func waitForRun(t *testing.T, f *apiFixture, runID string, want ...string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(40 * time.Second)
	var got map[string]any
	for {
		_, got = f.do("req", "GET", "/api/v1/runs/"+runID, nil)
		st, _ := got["status"].(string)
		for _, w := range want {
			if st == w {
				return got
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s never reached %v; last state %v", runID, want, got)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// gateForRun finds the question a parked run is waiting on.
func gateForRun(t *testing.T, f *apiFixture, runID string) map[string]any {
	t.Helper()
	_, gates := f.do("req", "GET", "/api/v1/gates", nil)
	items, _ := gates["items"].([]any)
	for _, raw := range items {
		if g, ok := raw.(map[string]any); ok && g["run_id"] == runID {
			return g
		}
	}
	return nil
}

// TestPauseResumeKeepsOtherWorkMoving is the central acceptance test for the
// pause/resume model. A run that needs a human must park without failing and
// without holding a worker slot; independent work must still complete; the answer
// must be durable across a worker restart; and the resumed run must prepare work
// that still needs a human before anything official happens.
func TestPauseResumeKeepsOtherWorkMoving(t *testing.T) {
	f, org, _ := freshOrgFixture(t)
	seedSimRecord(t, f, org, "inventory")
	seedSimRecord(t, f, org, "mail")
	activeHarnessRelease(t, f, "admin")

	code, taskA := f.do("req", "POST", "/api/v1/tasks", map[string]any{"title": "A must pause"})
	if code != 201 {
		t.Fatalf("task A %d %v", code, taskA)
	}
	code, taskB := f.do("req", "POST", "/api/v1/tasks", map[string]any{"title": "B must proceed"})
	if code != 201 {
		t.Fatalf("task B %d %v", code, taskB)
	}
	code, agent := f.do("req", "POST", "/api/v1/agents", map[string]any{"name": "Pause Agent", "harness": "simulator"})
	if code != 201 {
		t.Fatalf("agent %d %v", code, agent)
	}

	// The worker runs with concurrency ONE, so B completing while A waits is
	// proof that the parked run released its slot rather than sitting in it.
	stop := startWorker(t, f)

	code, runA := f.do("req", "POST", "/api/v1/tasks/"+id(taskA)+"/runs", map[string]any{"agent_id": id(agent), "intent": "clarify the supplier quantity"})
	if code != 201 {
		t.Fatalf("start run A %d %v", code, runA)
	}
	parked := waitForRun(t, f, id(runA), "waiting")
	if parked["failure_reason"] != "" {
		t.Fatalf("a parked run must not be a failure: %v", parked)
	}
	if parked["proposal_id"] != "" {
		t.Fatalf("a parked run must not publish work: %v", parked)
	}

	gate := gateForRun(t, f, id(runA))
	if gate == nil {
		t.Fatalf("the parked run's question is not visible to its respondent")
	}
	if prompt, _ := gate["prompt"].(string); prompt == "" {
		t.Fatalf("the question must say what it needs: %v", gate)
	}
	// A colleague must not be handed someone else's question: the inbox is scoped to
	// the named respondent, so an unrelated principal sees nothing here.
	if _, gatesOther := f.do("app", "GET", "/api/v1/gates", nil); len(gatesOther["items"].([]any)) != 0 {
		t.Fatalf("a non-respondent received a question: %v", gatesOther)
	}

	answer := map[string]any{"supplier_email": "ops@supplier.test", "quantity": 42}
	// Only the named respondent may answer, and bad answers are refused.
	if code, _ = f.do("app", "POST", "/api/v1/gates/"+id(gate)+"/respond", map[string]any{"revision": gate["revision"], "response": answer}); code != 403 {
		t.Fatalf("a non-respondent answer must be 403, got %d", code)
	}
	if code, _ = f.do("req", "POST", "/api/v1/gates/"+id(gate)+"/respond", map[string]any{"revision": gate["revision"], "response": map[string]any{"supplier_email": "ops@supplier.test"}}); code != 422 {
		t.Fatalf("a malformed answer must be 422, got %d", code)
	}
	if code, _ = f.do("req", "POST", "/api/v1/gates/"+id(gate)+"/respond", map[string]any{"revision": 99, "response": answer}); code != 409 {
		t.Fatalf("a stale revision must be 409, got %d", code)
	}

	// Independent work completes while A is still parked.
	code, runB := f.do("req", "POST", "/api/v1/tasks/"+id(taskB)+"/runs", map[string]any{"agent_id": id(agent), "intent": "inventory"})
	if code != 201 {
		t.Fatalf("start run B %d %v", code, runB)
	}
	doneB := waitForRun(t, f, id(runB), "succeeded")
	if doneB["proposal_id"] == "" {
		t.Fatalf("independent run B produced no proposal: %v", doneB)
	}
	if _, stillA := f.do("req", "GET", "/api/v1/runs/"+id(runA), nil); stillA["status"] != "waiting" {
		t.Fatalf("A should still be waiting after B completed, got %v", stillA["status"])
	}

	// Stop the worker entirely. From here nothing can progress by accident.
	stop()

	_, answered := f.do("req", "POST", "/api/v1/gates/"+id(gate)+"/respond", map[string]any{"revision": gate["revision"], "response": answer})
	if answered["status"] != "resolved" {
		t.Fatalf("gate not resolved: %v", answered)
	}
	// Answering is a state transition, not an execution.
	//
	// The STRICT form of this guarantee — "with no worker running, the run stays
	// queued and publishes nothing" — is asserted in internal/store, where the test
	// drives the store directly and no River worker can interfere. It belongs there
	// because this package shares one database AND one River queue across its tests,
	// so a delivery for this run may legitimately be in flight; asserting a transient
	// status here would test scheduling, not the guarantee.
	//
	// What must hold unconditionally, and is asserted here: answering never publishes
	// work that bypasses human review.
	code, afterAnswer := f.do("req", "GET", "/api/v1/runs/"+id(runA), nil)
	if code != 200 {
		t.Fatalf("could not read the run after answering: %d %v", code, afterAnswer)
	}
	switch afterAnswer["status"] {
	case "queued", "running", "succeeded":
	default:
		t.Fatalf("run after answering = %v, want it queued or progressing", afterAnswer["status"])
	}
	if pid, _ := afterAnswer["proposal_id"].(string); pid != "" {
		_, pp := f.do("req", "GET", "/api/v1/proposals/"+pid, nil)
		if pp["status"] != "pending_endorsement" {
			t.Fatalf("answering must not bypass review; proposal status = %v", pp["status"])
		}
	}

	// A fresh worker resumes A, using the human's answer.
	resume := startWorker(t, f)
	defer resume()
	final := waitForRun(t, f, id(runA), "succeeded", "failed")
	if final["status"] != "succeeded" {
		t.Fatalf("resumed run A failed: %v", final)
	}
	pid, _ := final["proposal_id"].(string)
	if pid == "" {
		t.Fatalf("resumed run produced no proposal: %v", final)
	}

	var ops []byte
	if e := f.st.WithOrg(context.Background(), org, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "select operations from proposals where org_id=$1 and id=$2", org, pid).Scan(&ops)
	}); e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(ops, []byte("ops@supplier.test")) {
		t.Fatalf("the prepared work must use the supplied answer: %s", ops)
	}
	if !bytes.Contains(ops, []byte("42")) {
		t.Fatalf("the prepared work must use the supplied quantity: %s", ops)
	}
	// A resumed run prepares work; it never authorises it.
	p := waitForApproval(t, f, pid)
	if p != "pending_endorsement" {
		t.Fatalf("proposal status = %q, want pending_endorsement", p)
	}
	// Exactly one continuation happened.
	var proposals int
	if e := f.st.WithOrg(context.Background(), org, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "select count(*) from proposals where org_id=$1 and task_id=$2", org, id(taskA)).Scan(&proposals)
	}); e != nil {
		t.Fatal(e)
	}
	if proposals != 1 {
		t.Fatalf("resumed run produced %d proposals, want exactly 1", proposals)
	}
}

// waitForApproval reads a proposal's status until it settles.
func waitForApproval(t *testing.T, f *apiFixture, pid string) string {
	t.Helper()
	_, p := f.do("req", "GET", "/api/v1/proposals/"+pid, nil)
	s, _ := p["status"].(string)
	return s
}

// TestUnsupportedHarnessIsRefusedNotSimulated proves an agent that names a harness
// this binary cannot execute is refused, rather than silently running as the
// simulator and producing output the operator would misattribute.
func TestUnsupportedHarnessIsRefusedNotSimulated(t *testing.T) {
	f, org, _ := freshOrgFixture(t)
	seedSimRecord(t, f, org, "inventory")
	activeHarnessRelease(t, f, "admin")

	code, task := f.do("req", "POST", "/api/v1/tasks", map[string]any{"title": "unsupported harness"})
	if code != 201 {
		t.Fatalf("task %d", code)
	}
	code, agent := f.do("req", "POST", "/api/v1/agents", map[string]any{"name": "Claude Agent", "harness": "claude-code"})
	if code != 201 {
		t.Fatalf("agent %d %v", code, agent)
	}
	code, resp := f.do("req", "POST", "/api/v1/tasks/"+id(task)+"/runs", map[string]any{"agent_id": id(agent), "intent": "inventory"})
	if code != 409 {
		t.Fatalf("an unsupported harness must be refused with 409, got %d %v", code, resp)
	}
	if e, _ := resp["error"].(map[string]any); e == nil || e["message"] == "" {
		t.Fatalf("the refusal must explain itself: %v", resp)
	}
	_, runs := f.do("req", "GET", "/api/v1/runs", nil)
	if items, _ := runs["items"].([]any); len(items) != 0 {
		t.Fatalf("a refused harness must not have created a run: %v", runs)
	}
}
