package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"workforce.local/platform/internal/platform"
)

// otherOrgFixture creates an unrelated tenant, for cross-org isolation checks.
func otherOrgFixture(t *testing.T, s *Store) string {
	t.Helper()
	o := platform.NewID()
	if e := s.WithOrg(context.Background(), o, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(), "INSERT INTO organizations(id,name) VALUES($1,'gate other org')", o)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	return o
}

// gateRunSetup creates everything a run needs: an owner, a task with a scoped
// inventory record, a simulator agent, and an active harness release.
func gateRunSetup(t *testing.T, s *Store, org, owner string) (Task, Agent) {
	t.Helper()
	ctx := context.Background()
	tk, e := s.CreateTask(ctx, org, "gate task", "", owner, "", "")
	if e != nil {
		t.Fatalf("create task: %v", e)
	}
	// The simulator's clarification path prepares a supplier message against a mail
	// record, so a scoped mail record must exist for a resumed run to succeed.
	seedInventory(t, s, org)
	if e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "INSERT INTO simulator_records(id,org_id,integration,version,data) VALUES($1,$2,'mail',1,'{}')", platform.NewID(), org)
		return e
	}); e != nil {
		t.Fatalf("seed mail record: %v", e)
	}
	ag, e := s.CreateAgent(ctx, org, "Gate Agent", "simulator", owner, []string{"prepare_proposal"})
	if e != nil {
		t.Fatalf("create agent: %v", e)
	}
	harnessReleaseInState(t, s, org, owner)
	return tk, ag
}

// parkRun drives a run to the parked state and returns the run and its gate.
func parkRun(t *testing.T, s *Store, org, owner, task, agent string) (AgentRun, Gate) {
	t.Helper()
	ctx := context.Background()
	run, e := s.CreateAgentRun(ctx, org, task, owner, agent, "clarify the supplier quantity")
	if e != nil {
		t.Fatalf("create run: %v", e)
	}
	if e := s.ExecuteAgentRun(ctx, org, run.ID); e != nil {
		t.Fatalf("execute run: %v", e)
	}
	got, e := s.GetAgentRun(ctx, org, run.ID)
	if e != nil {
		t.Fatal(e)
	}
	if got.Status != "waiting" {
		t.Fatalf("run status = %q, want waiting (a parked run is not a failure)", got.Status)
	}
	gates, e := s.ListGates(ctx, org, owner)
	if e != nil {
		t.Fatal(e)
	}
	for _, g := range gates {
		if g.RunID == run.ID {
			return got, g
		}
	}
	t.Fatalf("no gate was created for the parked run")
	return AgentRun{}, Gate{}
}

func TestWaitingRunParksWithoutFailing(t *testing.T) {
	s, org, req, app := fixture(t)
	ctx := context.Background()
	tk, ag := gateRunSetup(t, s, org, req)

	run, gate := parkRun(t, s, org, req, tk.ID, ag.ID)

	if run.FailureReason != "" {
		t.Fatalf("a parked run must not carry a failure reason, got %q", run.FailureReason)
	}
	if run.ProposalID != "" {
		t.Fatal("a parked run must not have published a proposal")
	}
	if gate.Prompt == "" {
		t.Fatal("the gate must state what it is asking for")
	}
	var schema struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if e := json.Unmarshal(gate.InputSchema, &schema); e != nil {
		t.Fatalf("the gate's input schema must be valid JSON: %v", e)
	}
	if len(schema.Properties) == 0 || len(schema.Required) == 0 {
		t.Fatalf("the gate must declare the fields it needs: %s", gate.InputSchema)
	}
	// An unaddressed question must land on the accountable owner, never nowhere.
	if gate.RespondentID != req {
		t.Fatalf("respondent = %q, want the task owner %q", gate.RespondentID, req)
	}
	// A different colleague must not see someone else's question.
	others, e := s.ListGates(ctx, org, app)
	if e != nil {
		t.Fatal(e)
	}
	for _, g := range others {
		if g.ID == gate.ID {
			t.Fatal("a non-respondent must not see another person's gate")
		}
	}
	// The question must be readable by its respondent.
	again, e := s.GetGate(ctx, org, gate.ID)
	if e != nil || again.ID != gate.ID {
		t.Fatalf("respondent cannot read their own gate: %v", e)
	}
}

func TestAnsweringTheGateResumesTheRunAndUsesTheAnswer(t *testing.T) {
	s, org, req, _ := fixture(t)
	ctx := context.Background()
	tk, ag := gateRunSetup(t, s, org, req)
	run, gate := parkRun(t, s, org, req, tk.ID, ag.ID)

	answer := json.RawMessage(`{"supplier_email":"ops@supplier.test","quantity":42}`)
	resolved, e := s.ResolveGate(ctx, org, gate.ID, req, gate.Revision, answer)
	if e != nil {
		t.Fatalf("the named respondent must be able to answer: %v", e)
	}
	if resolved.Status != "resolved" {
		t.Fatalf("gate status = %q, want resolved", resolved.Status)
	}

	// Answering is a state transition, not an execution. This test drives the store
	// directly with no worker running, so the run must sit in 'queued' and nothing
	// may have been published: the answer alone never creates or authorises work.
	mid, e := s.GetAgentRun(ctx, org, run.ID)
	if e != nil {
		t.Fatal(e)
	}
	if mid.Status != "queued" {
		t.Fatalf("run status after answering = %q, want queued", mid.Status)
	}
	if mid.ProposalID != "" {
		var pstatus string
		if e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, "select status from proposals where org_id=$1 and id=$2", org, mid.ProposalID).Scan(&pstatus)
		}); e != nil {
			t.Fatal(e)
		}
		if pstatus != "pending_endorsement" {
			t.Fatalf("run output must still await a human, got %q", pstatus)
		}
	}

	// Now the continuation runs, using the human's values.
	if e := s.ExecuteAgentRun(ctx, org, run.ID); e != nil {
		t.Fatalf("resumed run failed: %v", e)
	}
	final, e := s.GetAgentRun(ctx, org, run.ID)
	if e != nil {
		t.Fatal(e)
	}
	if final.Status != "succeeded" {
		t.Fatalf("resumed run status = %q (reason %q), want succeeded", final.Status, final.FailureReason)
	}
	if final.ProposalID == "" {
		t.Fatal("the resumed run did not link a proposal")
	}

	var ops []byte
	var pstatus string
	if e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "select operations,status from proposals where org_id=$1 and id=$2", org, final.ProposalID).Scan(&ops, &pstatus)
	}); e != nil {
		t.Fatal(e)
	}
	// The human's answer must be visible in the prepared work, not decorative.
	if !bytes.Contains(ops, []byte("ops@supplier.test")) {
		t.Fatalf("the prepared work does not use the supplied recipient: %s", ops)
	}
	if !bytes.Contains(ops, []byte("42")) {
		t.Fatalf("the prepared work does not use the supplied quantity: %s", ops)
	}
	// A run prepares; it never authorises. The output must still need a human.
	if pstatus != "pending_endorsement" {
		t.Fatalf("proposal status = %q, want pending_endorsement", pstatus)
	}
	if n := countProposals(t, s, org, tk.ID); n != 1 {
		t.Fatalf("resumed run produced %d proposals, want exactly 1", n)
	}
}

func TestGateResponseAuthorizationAndValidation(t *testing.T) {
	s, org, req, app := fixture(t)
	ctx := context.Background()
	tk, ag := gateRunSetup(t, s, org, req)
	_, gate := parkRun(t, s, org, req, tk.ID, ag.ID)
	good := json.RawMessage(`{"supplier_email":"ops@supplier.test","quantity":42}`)

	// Someone else cannot answer on the respondent's behalf, even a colleague.
	if _, e := s.ResolveGate(ctx, org, gate.ID, app, gate.Revision, good); !errors.Is(e, ErrGateNotRespondent) {
		t.Fatalf("a non-respondent answer must be refused, got %v", e)
	}
	// Malformed answers must never reach the run.
	for name, bad := range map[string]json.RawMessage{
		"missing required field": []byte(`{"supplier_email":"a@b.test"}`),
		"undeclared field":       []byte(`{"supplier_email":"a@b.test","quantity":1,"sneaky":"x"}`),
		"wrong type":             []byte(`{"supplier_email":"a@b.test","quantity":"lots"}`),
		"not an object":          []byte(`[1,2,3]`),
	} {
		if _, e := s.ResolveGate(ctx, org, gate.ID, req, gate.Revision, bad); !errors.Is(e, ErrGateInvalidResponse) {
			t.Fatalf("%s must be refused with ErrGateInvalidResponse, got %v", name, e)
		}
	}
	// A stale revision must be refused rather than silently applied.
	if _, e := s.ResolveGate(ctx, org, gate.ID, req, gate.Revision+1, good); !errors.Is(e, ErrGateRevision) {
		t.Fatalf("a stale revision must be refused, got %v", e)
	}
	// Another organisation must not even see the gate.
	if _, e := s.GetGate(ctx, otherOrgFixture(t, s), gate.ID); !errors.Is(e, ErrGateNotFound) {
		t.Fatalf("cross-org gate read must be not-found, got %v", e)
	}

	if _, e := s.ResolveGate(ctx, org, gate.ID, req, gate.Revision, good); e != nil {
		t.Fatalf("a valid answer must be accepted: %v", e)
	}
	// An identical replay is idempotent: same answer, no second continuation.
	replay, e := s.ResolveGate(ctx, org, gate.ID, req, gate.Revision, good)
	if e != nil {
		t.Fatalf("an identical replay must succeed idempotently, got %v", e)
	}
	if replay.Status != "resolved" {
		t.Fatalf("replay status = %q", replay.Status)
	}
	// A different answer after resolution must not silently overwrite the record.
	if _, e := s.ResolveGate(ctx, org, gate.ID, req, gate.Revision, json.RawMessage(`{"supplier_email":"other@b.test","quantity":1}`)); !errors.Is(e, ErrGateAlreadyResolved) {
		t.Fatalf("a conflicting second answer must be refused, got %v", e)
	}
}

func TestCancelledTaskCancelsItsRunsWithoutRetrying(t *testing.T) {
	s, org, req, _ := fixture(t)
	ctx := context.Background()
	tk, ag := gateRunSetup(t, s, org, req)

	run, e := s.CreateAgentRun(ctx, org, tk.ID, req, ag.ID, "inventory")
	if e != nil {
		t.Fatal(e)
	}
	// Cancelling the work cancels the run outright: it is a deliberate human
	// decision, not a failure, and it must never be retried afterwards.
	if e := s.CancelTask(ctx, org, tk.ID, tk.Version); e != nil {
		t.Fatalf("cancel: %v", e)
	}
	if e := s.ExecuteAgentRun(ctx, org, run.ID); e != nil {
		t.Fatalf("executing a cancelled run must be a quiet no-op, not a retryable error: %v", e)
	}
	got, e := s.GetAgentRun(ctx, org, run.ID)
	if e != nil {
		t.Fatal(e)
	}
	if got.Status != "cancelled" {
		t.Fatalf("status = %q, want cancelled", got.Status)
	}
	if got.FinishedAt == "" {
		t.Fatal("a cancelled run must be stamped finished so it is visibly resolved")
	}
	if n := countProposals(t, s, org, tk.ID); n != 0 {
		t.Fatalf("nothing may be published for a cancelled task, got %d proposals", n)
	}
}

// TestRunLosesRaceToTaskCancellation covers the in-flight case: the run was already
// claimed and produced a draft, but the task was cancelled before publication. The
// run must record a terminal outcome rather than publishing work for dead business.
func TestRunLosesRaceToTaskCancellation(t *testing.T) {
	s, org, req, _ := fixture(t)
	ctx := context.Background()
	tk, ag := gateRunSetup(t, s, org, req)

	run, e := s.CreateAgentRun(ctx, org, tk.ID, req, ag.ID, "inventory")
	if e != nil {
		t.Fatal(e)
	}
	// Mark the task terminal directly, leaving the run claimable: this reproduces the
	// race where cancellation lands between claim and publication.
	if e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "update tasks set status='cancelled',version=version+1 where org_id=$1 and id=$2", org, tk.ID)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if e := s.ExecuteAgentRun(ctx, org, run.ID); e != nil {
		t.Fatalf("a lost race is a recorded outcome, not a retryable error: %v", e)
	}
	got, e := s.GetAgentRun(ctx, org, run.ID)
	if e != nil {
		t.Fatal(e)
	}
	if got.Status != "failed" {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if got.FailureReason != "task_no_longer_active" {
		t.Fatalf("failure_reason = %q, want task_no_longer_active", got.FailureReason)
	}
	if n := countProposals(t, s, org, tk.ID); n != 0 {
		t.Fatalf("no work may be published for a cancelled task, got %d proposals", n)
	}
}

func TestConcurrentExecutionPublishesExactlyOneProposal(t *testing.T) {
	s, org, req, _ := fixture(t)
	ctx := context.Background()
	tk, ag := gateRunSetup(t, s, org, req)

	run, e := s.CreateAgentRun(ctx, org, tk.ID, req, ag.ID, "inventory")
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = s.ExecuteAgentRun(ctx, org, run.ID)
		}(i)
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("concurrent attempt %d returned %v", i, e)
		}
	}
	if n := countProposals(t, s, org, tk.ID); n != 1 {
		t.Fatalf("concurrent delivery produced %d proposals, want exactly 1", n)
	}
	got, e := s.GetAgentRun(ctx, org, run.ID)
	if e != nil {
		t.Fatal(e)
	}
	if got.Status != "succeeded" {
		t.Fatalf("status = %q, want succeeded", got.Status)
	}
}

// TestExpiredClaimIsRecoveredAfterWorkerDeath is the crash-resume proof. A worker
// that claims a run and then dies must not strand it forever: a bare 'queued'-only
// claim could never pick the run up again, so the claim is a bounded lease.
func TestExpiredClaimIsRecoveredAfterWorkerDeath(t *testing.T) {
	s, org, req, _ := fixture(t)
	ctx := context.Background()
	tk, ag := gateRunSetup(t, s, org, req)

	run, e := s.CreateAgentRun(ctx, org, tk.ID, req, ag.ID, "inventory")
	if e != nil {
		t.Fatal(e)
	}
	// Simulate the crashed worker: claimed, never finished, lease long expired.
	if e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "update agent_runs set status='running',claim_token='dead-worker',claim_expires_at=clock_timestamp()-interval '10 minutes' where org_id=$1 and id=$2", org, run.ID)
		return e
	}); e != nil {
		t.Fatal(e)
	}

	if e := s.ExecuteAgentRun(ctx, org, run.ID); e != nil {
		t.Fatalf("a healthy worker must recover the abandoned run: %v", e)
	}
	got, e := s.GetAgentRun(ctx, org, run.ID)
	if e != nil {
		t.Fatal(e)
	}
	if got.Status != "succeeded" {
		t.Fatalf("recovered run status = %q (reason %q), want succeeded", got.Status, got.FailureReason)
	}
	if n := countProposals(t, s, org, tk.ID); n != 1 {
		t.Fatalf("recovery produced %d proposals, want exactly 1", n)
	}
}

// TestLiveLeaseIsNotStolen proves recovery does not break fencing: a run whose
// lease is still valid belongs to its current holder and must not be re-executed.
func TestLiveLeaseIsNotStolen(t *testing.T) {
	s, org, req, _ := fixture(t)
	ctx := context.Background()
	tk, ag := gateRunSetup(t, s, org, req)

	run, e := s.CreateAgentRun(ctx, org, tk.ID, req, ag.ID, "inventory")
	if e != nil {
		t.Fatal(e)
	}
	if e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "update agent_runs set status='running',claim_token='live-worker',claim_expires_at=clock_timestamp()+interval '5 minutes' where org_id=$1 and id=$2", org, run.ID)
		return e
	}); e != nil {
		t.Fatal(e)
	}

	if e := s.ExecuteAgentRun(ctx, org, run.ID); e != nil {
		t.Fatalf("a live lease must not surface an error: %v", e)
	}
	if n := countProposals(t, s, org, tk.ID); n != 0 {
		t.Fatalf("a live lease must not be stolen, but %d proposals were published", n)
	}
	got, _ := s.GetAgentRun(ctx, org, run.ID)
	if got.Status != "running" {
		t.Fatalf("status = %q, want the current holder to still own it", got.Status)
	}
}

// TestGateReadIsAuthorizedNotJustTenantScoped proves a question and its recorded
// answer are visible only to people entitled to see them. Tenant isolation alone
// would let any colleague read what a specific person was asked and what they said.
func TestGateReadIsAuthorizedNotJustTenantScoped(t *testing.T) {
	s, org, req, app := fixture(t)
	ctx := context.Background()
	tk, ag := gateRunSetup(t, s, org, req)
	_, gate := parkRun(t, s, org, req, tk.ID, ag.ID)

	// The named respondent can read their own question.
	if _, e := s.ReadGate(ctx, org, gate.ID, req); e != nil {
		t.Fatalf("respondent cannot read their own gate: %v", e)
	}
	// An unrelated colleague in the same org must NOT read it. Not-found rather than
	// forbidden, so the gate's existence is not leaked either.
	if _, e := s.ReadGate(ctx, org, gate.ID, app); !errors.Is(e, ErrGateNotFound) {
		t.Fatalf("an unrelated colleague read another person's gate: %v", e)
	}
	// An admin may read it for oversight.
	admin := platform.NewID()
	if e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "INSERT INTO principals(id,org_id,name,role) VALUES($1,$2,'Gate Reader Admin','admin')", admin, org)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ReadGate(ctx, org, gate.ID, admin); e != nil {
		t.Fatalf("an admin must be able to read a gate for oversight: %v", e)
	}
	// Another organisation sees nothing at all.
	if _, e := s.ReadGate(ctx, otherOrgFixture(t, s), gate.ID, admin); !errors.Is(e, ErrGateNotFound) {
		t.Fatalf("cross-org gate read must be not-found, got %v", e)
	}
}

// TestGateIntegerAnswersAreNotSilentlyAltered guards the exactness of numbers a human
// stated. JSON numbers decode to float64 by default, which rounds integers above 2^53.
func TestGateIntegerAnswersAreNotSilentlyAltered(t *testing.T) {
	schema := json.RawMessage(`{"properties":{"quantity":{"type":"integer"}},"required":["quantity"]}`)
	if e := validateGateResponse(schema, json.RawMessage(`{"quantity":9007199254740991}`)); e != nil {
		t.Fatalf("2^53-1 is exactly representable and must be accepted: %v", e)
	}
	for name, bad := range map[string]string{
		"above 2^53": `{"quantity":9007199254740993}`,
		"huge":       `{"quantity":1e300}`,
		"fractional": `{"quantity":2.5}`,
		"null":       `{"quantity":null}`,
		"wrong type": `{"quantity":"many"}`,
	} {
		if e := validateGateResponse(schema, json.RawMessage(bad)); !errors.Is(e, ErrGateInvalidResponse) {
			t.Fatalf("%s must be refused, got %v", name, e)
		}
	}
	for name, bad := range map[string]string{
		"array root":       `[1,2]`,
		"undeclared":       `{"quantity":1,"extra":true}`,
		"missing-required": `{}`,
	} {
		if e := validateGateResponse(schema, json.RawMessage(bad)); !errors.Is(e, ErrGateInvalidResponse) {
			t.Fatalf("%s must be refused, got %v", name, e)
		}
	}
}

// TestIdenticalAnswerReplaySurvivesJSONReformatting covers the idempotent replay path.
// jsonb normalises key order and whitespace, so comparing raw bytes would reject a
// legitimate retry of the very same answer.
func TestIdenticalAnswerReplaySurvivesJSONReformatting(t *testing.T) {
	s, org, req, _ := fixture(t)
	ctx := context.Background()
	tk, ag := gateRunSetup(t, s, org, req)
	_, gate := parkRun(t, s, org, req, tk.ID, ag.ID)

	first := json.RawMessage(`{"supplier_email":"ops@supplier.test","quantity":42}`)
	if _, e := s.ResolveGate(ctx, org, gate.ID, req, gate.Revision, first); e != nil {
		t.Fatalf("first answer: %v", e)
	}
	replay := json.RawMessage(`{ "quantity" : 42 , "supplier_email" : "ops@supplier.test" }`)
	if _, e := s.ResolveGate(ctx, org, gate.ID, req, gate.Revision, replay); e != nil {
		t.Fatalf("an identical answer must replay idempotently regardless of formatting: %v", e)
	}
	if _, e := s.ResolveGate(ctx, org, gate.ID, req, gate.Revision, json.RawMessage(`{"supplier_email":"other@x.test","quantity":1}`)); !errors.Is(e, ErrGateAlreadyResolved) {
		t.Fatalf("a genuinely different answer must be refused, got %v", e)
	}
}

// TestRevokedHarnessBlocksPublication proves revocation is re-checked at PUBLICATION
// time, not only when a run is admitted. A harness withdrawn while a run is in flight
// must not publish work, and the recorded reason must be a safe public code.
func TestRevokedHarnessBlocksPublication(t *testing.T) {
	s, org, req, _ := fixture(t)
	ctx := context.Background()
	tk, ag := gateRunSetup(t, s, org, req)

	run, e := s.CreateAgentRun(ctx, org, tk.ID, req, ag.ID, "inventory")
	if e != nil {
		t.Fatal(e)
	}
	// Revoke the bound harness release between create and execute.
	if e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "update registry_releases set state='revoked' where org_id=$1 and kind='harness' and state='active'", org)
		return e
	}); e != nil {
		t.Fatal(e)
	}

	if e := s.ExecuteAgentRun(ctx, org, run.ID); e != nil {
		t.Fatalf("a revoked harness is a recorded outcome, not an executor error: %v", e)
	}
	got, e := s.GetAgentRun(ctx, org, run.ID)
	if e != nil {
		t.Fatal(e)
	}
	if got.Status != "failed" {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if got.FailureReason == "" {
		t.Fatal("a failed run must record a public reason")
	}
	// A user-visible field must never leak backend text.
	if strings.Contains(got.FailureReason, "SQLSTATE") || strings.Contains(got.FailureReason, "pgx") {
		t.Fatalf("failure_reason leaks backend detail: %q", got.FailureReason)
	}
	if got.ProposalID != "" {
		t.Fatal("a revoked harness must not publish work")
	}
	if n := countProposals(t, s, org, tk.ID); n != 0 {
		t.Fatalf("revocation must block publication, got %d proposals", n)
	}
}
