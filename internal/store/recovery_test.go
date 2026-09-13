package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// forceLease reproduces the state a real claim leaves behind, so recovery is tested
// against the situation it actually faces. A live claim sets started_at as well as the
// token and lease; simulating a crash without that would let a recovery path that
// wipes start history pass unnoticed.
func forceLease(t *testing.T, s *Store, org, runID, token string, expires *time.Time) {
	t.Helper()
	if err := s.WithOrg(context.Background(), org, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(),
			`UPDATE agent_runs
			    SET status='running',
			        claim_token=$3,
			        claim_expires_at=$4,
			        started_at=COALESCE(started_at, clock_timestamp())
			  WHERE org_id=$1 AND id=$2`,
			org, runID, token, expires)
		return e
	}); err != nil {
		t.Fatal(err)
	}
}

// TestOrganizationCursorCoversTheDirectory is the premise recovery depends on. The
// directory holds thousands of organisations and tenant tables are under forced RLS,
// so candidates must come from that directory and be walked with a cursor. A fixed
// prefix would have meant "the first few tenants are recovered, everyone else never".
func TestOrganizationCursorCoversTheDirectory(t *testing.T) {
	s, org, _, _ := fixture(t)
	ctx := context.Background()

	first, _, err := s.orgsAfter(ctx, "", 5)
	if err != nil {
		t.Fatalf("directory unreadable: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("the organisation directory must be readable by the runtime role")
	}
	// The walk must be strictly forward-moving, or it would revisit the same pages.
	second, _, err := s.orgsAfter(ctx, first[len(first)-1], 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range second {
		if id <= first[len(first)-1] {
			t.Fatalf("cursor walk went backwards: %q after %q", id, first[len(first)-1])
		}
	}
	// Every page must be ordered, so a cursor cannot skip entries.
	for i := 1; i < len(first); i++ {
		if first[i] <= first[i-1] {
			t.Fatalf("page is not ordered: %q after %q", first[i], first[i-1])
		}
	}
	// The fixture org must be discoverable by some page of the walk.
	found := false
	cursor := ""
	for i := 0; i < 200 && !found; i++ {
		page, wrapped, err := s.orgsAfter(ctx, cursor, 200)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		for _, id := range page {
			if id == org {
				found = true
			}
		}
		if wrapped {
			break
		}
		cursor = page[len(page)-1]
	}
	if !found {
		t.Fatal("the cursor walk never reached the fixture organisation; recovery would miss tenants")
	}
}

// TestExpiredRunIsActuallyRecovered is the point of the sweeper. Assertions are made
// against THIS fixture's run, never a global count: the development database persists
// and other tenants may legitimately have stale claims of their own.
func TestExpiredRunIsActuallyRecovered(t *testing.T) {
	s, org, req, _ := fixture(t)
	ctx := context.Background()
	tk, ag := gateRunSetup(t, s, org, req)

	run, e := s.CreateAgentRun(ctx, org, tk.ID, req, ag.ID, "inventory")
	if e != nil {
		t.Fatal(e)
	}
	// Simulate the crash: claimed, never finished, lease long past.
	past := time.Now().Add(-10 * time.Minute)
	forceLease(t, s, org, run.ID, "dead-worker", &past)

	n, err := s.ReclaimExpiredRunsForOrg(ctx, org, 20)
	if err != nil {
		t.Fatalf("recovery: %v", err)
	}
	if n < 1 {
		t.Fatal("an expired claim must actually be reclaimed, not merely be eligible")
	}

	got, e := s.GetAgentRun(ctx, org, run.ID)
	if e != nil {
		t.Fatal(e)
	}
	if got.Status != "queued" {
		t.Fatalf("status = %q, want queued after recovery", got.Status)
	}
	if got.StartedAt == "" {
		t.Fatal("recovery must not erase the fact that the run had started")
	}

	// Recovery is worthless unless the run can actually be picked back up.
	if e := s.ExecuteAgentRun(ctx, org, run.ID); e != nil {
		t.Fatalf("recovered run could not execute: %v", e)
	}
	final, e := s.GetAgentRun(ctx, org, run.ID)
	if e != nil {
		t.Fatal(e)
	}
	if final.Status != "succeeded" {
		t.Fatalf("status = %q (reason %q), want succeeded", final.Status, final.FailureReason)
	}
	if n := countProposals(t, s, org, tk.ID); n != 1 {
		t.Fatalf("recovery produced %d proposals, want exactly 1", n)
	}
}

// TestRecoveryRecordsAnAuditEvent: a run changing hands must be visible afterwards,
// or an investigator cannot tell a recovered run from a first attempt.
func TestRecoveryRecordsAnAuditEvent(t *testing.T) {
	s, org, req, _ := fixture(t)
	ctx := context.Background()
	tk, ag := gateRunSetup(t, s, org, req)

	run, e := s.CreateAgentRun(ctx, org, tk.ID, req, ag.ID, "inventory")
	if e != nil {
		t.Fatal(e)
	}
	past := time.Now().Add(-10 * time.Minute)
	forceLease(t, s, org, run.ID, "dead-worker", &past)
	if _, e := s.ReclaimExpiredRunsForOrg(ctx, org, 20); e != nil {
		t.Fatal(e)
	}

	var n int
	if e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM domain_events
			  WHERE org_id=$1 AND aggregate_id=$2 AND event_type='agent_run.reclaimed'`,
			org, run.ID).Scan(&n)
	}); e != nil {
		t.Fatal(e)
	}
	if n != 1 {
		t.Fatalf("expected exactly one reclaim event, got %d", n)
	}
}

// TestLiveLeaseIsNotReclaimed: recovery must never steal work from a healthy worker.
func TestLiveLeaseIsNotReclaimed(t *testing.T) {
	s, org, req, _ := fixture(t)
	ctx := context.Background()
	tk, ag := gateRunSetup(t, s, org, req)

	run, e := s.CreateAgentRun(ctx, org, tk.ID, req, ag.ID, "inventory")
	if e != nil {
		t.Fatal(e)
	}
	future := time.Now().Add(5 * time.Minute)
	forceLease(t, s, org, run.ID, "live-worker", &future)

	n, err := s.ReclaimExpiredRunsForOrg(ctx, org, 20)
	if err != nil {
		t.Fatalf("recovery: %v", err)
	}
	if n != 0 {
		t.Fatalf("reclaimed %d run(s) despite a live lease", n)
	}
	got, _ := s.GetAgentRun(ctx, org, run.ID)
	if got.Status != "running" {
		t.Fatalf("status = %q, want the healthy holder to keep it", got.Status)
	}
	if n := countProposals(t, s, org, tk.ID); n != 0 {
		t.Fatalf("recovery published %d proposal(s) for a live run", n)
	}
}

// TestRecoveredRunIsNotReclaimedAgain: once re-queued, a run must stop being a
// candidate, so a later sweep cannot pile up another delivery for it.
func TestRecoveredRunIsNotReclaimedAgain(t *testing.T) {
	s, org, req, _ := fixture(t)
	ctx := context.Background()
	tk, ag := gateRunSetup(t, s, org, req)

	run, e := s.CreateAgentRun(ctx, org, tk.ID, req, ag.ID, "inventory")
	if e != nil {
		t.Fatal(e)
	}
	past := time.Now().Add(-10 * time.Minute)
	forceLease(t, s, org, run.ID, "dead-worker", &past)

	first, err := s.ReclaimExpiredRunsForOrg(ctx, org, 20)
	if err != nil {
		t.Fatal(err)
	}
	if first < 1 {
		t.Fatal("the first recovery should have reclaimed the run")
	}
	second, err := s.ReclaimExpiredRunsForOrg(ctx, org, 20)
	if err != nil {
		t.Fatal(err)
	}
	if second != 0 {
		t.Fatalf("a queued run was reclaimed again (%d); recovery must be idempotent", second)
	}

	// Exactly one event, and exactly one delivery's worth of work can result.
	var events int
	if e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM domain_events WHERE org_id=$1 AND aggregate_id=$2 AND event_type='agent_run.reclaimed'`,
			org, run.ID).Scan(&events)
	}); e != nil {
		t.Fatal(e)
	}
	if events != 1 {
		t.Fatalf("expected one reclaim event after two sweeps, got %d", events)
	}
	if n := countProposals(t, s, org, tk.ID); n != 0 {
		t.Fatalf("recovery alone must not publish work, got %d proposal(s)", n)
	}
}

// TestBatchLimitIsHonouredNotOverrun: the candidate set must be bounded BEFORE the
// update. Applying the limit afterwards would leave extra rows marked queued with no
// job to pick them up — stranded, not recovered.
func TestBatchLimitIsHonouredNotOverrun(t *testing.T) {
	s, org, req, _ := fixture(t)
	ctx := context.Background()
	tk, ag := gateRunSetup(t, s, org, req)

	past := time.Now().Add(-10 * time.Minute)
	var ids []string
	for i := 0; i < 3; i++ {
		r, e := s.CreateAgentRun(ctx, org, tk.ID, req, ag.ID, "inventory")
		if e != nil {
			t.Fatal(e)
		}
		forceLease(t, s, org, r.ID, "dead-worker", &past)
		ids = append(ids, r.ID)
	}

	n, err := s.ReclaimExpiredRunsForOrg(ctx, org, 1)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("reclaimed %d runs, want exactly the requested 1", n)
	}

	// Exactly one run may be queued; the others must be untouched, not silently moved.
	queued := 0
	for _, id := range ids {
		got, e := s.GetAgentRun(ctx, org, id)
		if e != nil {
			t.Fatal(e)
		}
		if got.Status == "queued" {
			queued++
		}
	}
	if queued != 1 {
		t.Fatalf("%d runs ended queued; the batch limit must not strand the remainder", queued)
	}
}

// TestTerminalTaskIsNotResurrected: recovery must not bring back work for closed
// business, or a cancelled task's agent would start publishing again.
func TestTerminalTaskIsNotResurrected(t *testing.T) {
	s, org, req, _ := fixture(t)
	ctx := context.Background()
	tk, ag := gateRunSetup(t, s, org, req)

	run, e := s.CreateAgentRun(ctx, org, tk.ID, req, ag.ID, "inventory")
	if e != nil {
		t.Fatal(e)
	}
	past := time.Now().Add(-10 * time.Minute)
	forceLease(t, s, org, run.ID, "dead-worker", &past)
	if e := s.CancelTask(ctx, org, tk.ID, tk.Version); e != nil {
		t.Fatalf("cancel: %v", e)
	}

	if _, err := s.ReclaimExpiredRunsForOrg(ctx, org, 20); err != nil {
		t.Fatalf("recovery: %v", err)
	}
	got, _ := s.GetAgentRun(ctx, org, run.ID)
	if got.Status == "queued" {
		t.Fatal("recovery resurrected a run for a cancelled task")
	}
	if n := countProposals(t, s, org, tk.ID); n != 0 {
		t.Fatalf("nothing may be published for a cancelled task, got %d", n)
	}
}

// TestDisplacedHolderCannotPublishTwice states the fencing property. After recovery,
// the run must be claimable exactly once more: the displaced holder's token matches
// nothing, so only one new attempt can publish.
func TestDisplacedHolderCannotPublishTwice(t *testing.T) {
	s, org, req, _ := fixture(t)
	ctx := context.Background()
	tk, ag := gateRunSetup(t, s, org, req)

	run, e := s.CreateAgentRun(ctx, org, tk.ID, req, ag.ID, "inventory")
	if e != nil {
		t.Fatal(e)
	}
	past := time.Now().Add(-10 * time.Minute)
	forceLease(t, s, org, run.ID, "stale-holder", &past)
	if _, e := s.ReclaimExpiredRunsForOrg(ctx, org, 20); e != nil {
		t.Fatal(e)
	}

	// A new attempt claims and publishes...
	if e := s.ExecuteAgentRun(ctx, org, run.ID); e != nil {
		t.Fatal(e)
	}
	if n := countProposals(t, s, org, tk.ID); n != 1 {
		t.Fatalf("expected exactly one proposal after recovery, got %d", n)
	}
	// ...and every later delivery is a quiet no-op, so the displaced holder cannot
	// double-apply the work.
	for i := 0; i < 3; i++ {
		if e := s.ExecuteAgentRun(ctx, org, run.ID); e != nil {
			t.Fatalf("delivery %d was not a quiet no-op: %v", i, e)
		}
	}
	if n := countProposals(t, s, org, tk.ID); n != 1 {
		t.Fatalf("duplicate publication after recovery: %d proposals", n)
	}
}
