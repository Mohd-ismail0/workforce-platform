package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"workforce.local/platform/internal/platform"
)

// harnessRelease creates a harness release in the requested state. It is the only
// thing that can let an agent run start, so every gating test drives it explicitly.
func harnessRelease(t *testing.T, s *Store, org, actor, role, state string) RegistryRelease {
	t.Helper()
	ctx := context.Background()
	u := platform.NewID()
	rel, e := s.CreateRegistryRelease(ctx, org, actor, RegistryCreate{
		Family: "harness-fam-" + u[:8], Kind: "harness", Version: "1.0.0-" + u[8:16],
		Digest: "sha256:" + u, RequestedCapabilities: []string{"prepare_proposal"},
		Manifest:   json.RawMessage(`{"runner_id":"simulator"}`),
		Simulation: true, CompatibilityRange: ">=1", Provenance: map[string]any{"source": "fixture"},
	})
	if e != nil {
		t.Fatalf("create harness release: %v", e)
	}
	for _, step := range []string{"verified", "approved", "installed", "active"} {
		if state == "quarantined" {
			break
		}
		if rel.State == state {
			break
		}
		rel, e = s.TransitionRegistryRelease(ctx, org, rel.ID, actor, role, step, "fixture", rel.Revision)
		if e != nil {
			t.Fatalf("transition to %s: %v", step, e)
		}
	}
	if rel.State != state {
		t.Fatalf("release state = %q, want %q", rel.State, state)
	}
	return rel
}

func runPrereqs(t *testing.T, s *Store, org, req, app string) (Task, Agent, string) {
	t.Helper()
	ctx := context.Background()
	admin := platform.NewID()
	if e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "INSERT INTO principals(id,org_id,name,role) VALUES($1,$2,'Fixture Admin','admin')", admin, org)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	tk := task(t, s, org, req)
	target := seedInventory(t, s, org)
	ag, e := s.CreateAgent(ctx, org, "Fixture Agent", "simulator", req, []string{"prepare_proposal"})
	if e != nil {
		t.Fatalf("create agent: %v", e)
	}
	return tk, ag, target
}

func TestAgentRunRefusesWithoutAnActiveHarnessRelease(t *testing.T) {
	s, org, req, app := fixture(t)
	ctx := context.Background()
	tk, ag, _ := runPrereqs(t, s, org, req, app)

	// No harness release at all.
	if _, e := s.CreateAgentRun(ctx, org, tk.ID, req, ag.ID, "inventory"); !errors.As(e, &ErrNoActiveHarness{}) {
		t.Fatalf("no release: got %v, want ErrNoActiveHarness", e)
	}
	// A quarantined release must NOT satisfy the gate.
	harnessRelease(t, s, org, req, "requester", "quarantined")
	if _, e := s.CreateAgentRun(ctx, org, tk.ID, req, ag.ID, "inventory"); !errors.As(e, &ErrNoActiveHarness{}) {
		t.Fatalf("quarantined release: got %v, want ErrNoActiveHarness", e)
	}

	// Only an operator may promote it, and promotion is what unlocks runs.
	if _, e := s.CreateAgentRun(ctx, org, tk.ID, req, ag.ID, "inventory"); !errors.As(e, &ErrNoActiveHarness{}) {
		t.Fatalf("gate did not hold before promotion: %v", e)
	}
	harnessReleaseInState(t, s, org, req)

	run, e := s.CreateAgentRun(ctx, org, tk.ID, req, ag.ID, "inventory")
	if e != nil {
		t.Fatalf("active release present, run refused: %v", e)
	}
	if run.Status != "queued" || run.HarnessReleaseID == "" || run.Harness != "simulator" {
		t.Fatalf("unexpected run: %+v", run)
	}
}

// harnessReleaseInState installs a harness release and drives it to active using an
// admin actor, returning it. Used where a test needs the gate satisfied.
func harnessReleaseInState(t *testing.T, s *Store, org, req string) RegistryRelease {
	t.Helper()
	ctx := context.Background()
	admin := platform.NewID()
	if e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "INSERT INTO principals(id,org_id,name,role) VALUES($1,$2,'Gate Admin','admin')", admin, org)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	return harnessRelease(t, s, org, admin, "admin", "active")
}

func TestAgentRunAuthorizationAndTerminalTask(t *testing.T) {
	s, org, req, app := fixture(t)
	ctx := context.Background()
	tk, ag, _ := runPrereqs(t, s, org, req, app)
	harnessReleaseInState(t, s, org, req)

	// A third party who is neither the task owner nor an admin cannot start a run.
	if _, e := s.CreateAgentRun(ctx, org, tk.ID, app, ag.ID, "inventory"); e == nil {
		t.Fatal("a non-owner, non-admin actor must not start a run")
	}
	// An admin may.
	admin := platform.NewID()
	if e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "INSERT INTO principals(id,org_id,name,role) VALUES($1,$2,'Ops Admin','admin')", admin, org)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateAgentRun(ctx, org, tk.ID, admin, ag.ID, "inventory"); e != nil {
		t.Fatalf("active admin must be allowed: %v", e)
	}
	// An agent from another org must never be usable.
	tk2 := task(t, s, org, req)
	if _, e := s.CreateAgentRun(ctx, org, tk2.ID, req, "does-not-exist-agent", "inventory"); e == nil {
		t.Fatal("unknown agent must be rejected")
	}
	// A terminal task cannot start new work.
	if e := s.CancelTask(ctx, org, tk2.ID, tk2.Version); e != nil {
		t.Fatalf("cancel: %v", e)
	}
	if _, e := s.CreateAgentRun(ctx, org, tk2.ID, req, ag.ID, "inventory"); e == nil {
		t.Fatal("a cancelled task must not accept new runs")
	}
}

func TestExecuteAgentRunPreparesExactlyOneProposal(t *testing.T) {
	s, org, req, app := fixture(t)
	ctx := context.Background()
	tk, ag, target := runPrereqs(t, s, org, req, app)
	harnessReleaseInState(t, s, org, req)

	run, e := s.CreateAgentRun(ctx, org, tk.ID, req, ag.ID, "inventory")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ExecuteAgentRun(ctx, org, run.ID); e != nil {
		t.Fatalf("execute: %v", e)
	}
	got, e := s.GetAgentRun(ctx, org, run.ID)
	if e != nil {
		t.Fatal(e)
	}
	if got.Status != "succeeded" || got.ProposalID == "" || got.FinishedAt == "" {
		t.Fatalf("run did not succeed cleanly: %+v", got)
	}
	if got.StartedAt == "" {
		t.Fatal("started_at must be recorded")
	}
	var ops []byte
	if e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "select operations from proposals where org_id=$1 and id=$2", org, got.ProposalID).Scan(&ops)
	}); e != nil {
		t.Fatal(e)
	}
	var parsed []map[string]any
	if e := json.Unmarshal(ops, &parsed); e != nil {
		t.Fatal(e)
	}
	if len(parsed) != 1 {
		t.Fatalf("run must prepare exactly one operation, got %d", len(parsed))
	}
	if parsed[0]["target_id"] != target {
		t.Fatalf("operation target = %v, want %s", parsed[0]["target_id"], target)
	}
	if k, _ := parsed[0]["business_key"].(string); k == "" {
		t.Fatal("prepared operation must carry a business key")
	}

	// Re-execution must not produce a second proposal.
	before := countProposals(t, s, org, tk.ID)
	if e := s.ExecuteAgentRun(ctx, org, run.ID); e != nil {
		t.Fatalf("second execute returned an error: %v", e)
	}
	if after := countProposals(t, s, org, tk.ID); after != before {
		t.Fatalf("re-execution created another proposal: %d -> %d", before, after)
	}
}

func TestExecuteAgentRunRecordsBusinessFailure(t *testing.T) {
	s, org, req, app := fixture(t)
	ctx := context.Background()
	// A documents run with no documents record in scope must fail visibly,
	// never sit in "running" forever.
	tk, ag, _ := runPrereqs(t, s, org, req, app)
	harnessReleaseInState(t, s, org, req)
	run, e := s.CreateAgentRun(ctx, org, tk.ID, req, ag.ID, "update the document")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ExecuteAgentRun(ctx, org, run.ID); e != nil {
		t.Fatalf("a business failure must not surface as an executor error: %v", e)
	}
	got, _ := s.GetAgentRun(ctx, org, run.ID)
	if got.Status != "failed" {
		t.Fatalf("status = %q, want failed", got.Status)
	}
	if got.FailureReason == "" {
		t.Fatal("failure_reason must be recorded so the operator can act")
	}
	if got.FinishedAt == "" {
		t.Fatal("finished_at must be set on failure")
	}
	if got.ProposalID != "" {
		t.Fatal("a failed run must not claim a proposal")
	}
}

func countProposals(t *testing.T, s *Store, org, task string) int {
	t.Helper()
	var n int
	if e := s.WithOrg(context.Background(), org, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "select count(*) from proposals where org_id=$1 and task_id=$2", org, task).Scan(&n)
	}); e != nil {
		t.Fatal(e)
	}
	return n
}

func TestAgentRunListAndGetAreScoped(t *testing.T) {
	s, org, req, app := fixture(t)
	ctx := context.Background()
	tk, ag, _ := runPrereqs(t, s, org, req, app)
	harnessReleaseInState(t, s, org, req)
	run, e := s.CreateAgentRun(ctx, org, tk.ID, req, ag.ID, "inventory")
	if e != nil {
		t.Fatal(e)
	}
	listed, e := s.ListAgentRuns(ctx, org)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, r := range listed {
		if r.ID == run.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("created run missing from the org listing")
	}
	other := platform.NewID()
	if e := s.WithOrg(ctx, other, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "INSERT INTO organizations(id,name) VALUES($1,'other')", other)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.GetAgentRun(ctx, other, run.ID); e == nil {
		t.Fatal("another org must not read this run")
	}
}
