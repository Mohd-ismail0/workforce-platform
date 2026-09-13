package store

import (
	"context"
	"github.com/jackc/pgx/v5"
	"testing"
	"time"
	"workforce.local/platform/internal/connectors"
)

func TestDependencyAutomaticallyResumesChild(t *testing.T) {
	s, o, r, a := fixture(t)
	parent, pp := proposal(t, s, o, r)
	child, cp := proposal(t, s, o, r)
	ctx := context.Background()
	if e := s.AddDependency(ctx, o, child.ID, parent.ID); e != nil {
		t.Fatal(e)
	}
	approve := func(p Proposal) {
		t.Helper()
		if e := s.Decide(ctx, o, p.ID, r, "requester", "endorse", p.Revision, p.Digest, ""); e != nil {
			t.Fatal(e)
		}
		if e := s.Decide(ctx, o, p.ID, a, "approver", "approve", p.Revision, p.Digest, ""); e != nil {
			t.Fatal(e)
		}
	}
	approve(cp)
	if e := s.executeProposal(ctx, connectors.NewRegistry(), o, cp.ID); e != nil {
		t.Fatal(e)
	}
	waiting, _ := s.GetProposal(ctx, o, cp.ID)
	if waiting.Status != "approved" {
		t.Fatalf("waiting state %s", waiting.Status)
	}
	approve(pp)
	if e := s.executeProposal(ctx, connectors.NewRegistry(), o, pp.ID); e != nil {
		t.Fatal(e)
	}
	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- s.RunWorker(workerCtx, nil) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("stop timeout")
		}
	}()
	limit := time.Now().Add(10 * time.Second)
	for {
		got, e := s.GetProposal(ctx, o, cp.ID)
		if e != nil {
			t.Fatal(e)
		}
		if got.Status == "done" {
			break
		}
		if time.Now().After(limit) {
			t.Fatalf("child not resumed: %s", got.Status)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
func TestStaleEffectStopsRetry(t *testing.T) {
	s, o, r, a := fixture(t)
	task, p := proposal(t, s, o, r)
	ctx := context.Background()
	for _, v := range []struct{ id, role, kind string }{{r, "requester", "endorse"}, {a, "approver", "approve"}} {
		if e := s.Decide(ctx, o, p.ID, v.id, v.role, v.kind, p.Revision, p.Digest, ""); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.WithOrg(ctx, o, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "UPDATE simulator_records SET version=version+1 WHERE id=$1", p.Operations[0].TargetID)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if e := s.executeProposal(ctx, connectors.NewRegistry(), o, p.ID); e != nil {
		t.Fatalf("permanent failure retried: %v", e)
	}
	got, _ := s.GetProposal(ctx, o, p.ID)
	if got.Status != "needs_attention" {
		t.Fatalf("state %s", got.Status)
	}
	current, _ := s.GetTask(ctx, o, task.ID)
	if current.Status != "blocked" {
		t.Fatalf("task %s", current.Status)
	}
}
