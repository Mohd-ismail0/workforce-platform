package store

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"testing"
	"workforce.local/platform/internal/connectors"
	"workforce.local/platform/internal/platform"
)

func TestBusinessKeyBlocksDuplicateAcrossTasks(t *testing.T) {
	s, o, r, _ := fixture(t)
	ctx := context.Background()
	a := task(t, s, o, r)
	b := task(t, s, o, r)
	target := seedInventory(t, s, o)
	key := "invoice:INV-1:settle"
	if _, e := s.CreateProposal(ctx, o, a.ID, r, "first", []connectors.Operation{opForTest(t, target, key, 1)}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateProposal(ctx, o, b.ID, r, "duplicate", []connectors.Operation{opForTest(t, target, key, 1)}); e == nil {
		t.Fatal("duplicate business key accepted across tasks")
	}
}
func TestBusinessKeyAllowsRebindAfterRejection(t *testing.T) {
	s, o, r, a1 := fixture(t)
	ctx := context.Background()
	tk := task(t, s, o, r)
	target := seedInventory(t, s, o)
	key := "invoice:INV-2:settle"
	p, e := s.CreateProposal(ctx, o, tk.ID, r, "first", []connectors.Operation{opForTest(t, target, key, 1)})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Decide(ctx, o, p.ID, a1, "approver", "reject", p.Revision, p.Digest, "not needed"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateProposal(ctx, o, tk.ID, r, "rebind", []connectors.Operation{opForTest(t, target, key, 1)}); e != nil {
		t.Fatalf("released key not rebindable: %v", e)
	}
}
func TestBusinessKeyChangedPayloadRejected(t *testing.T) {
	s, o, r, _ := fixture(t)
	ctx := context.Background()
	tk := task(t, s, o, r)
	target := seedInventory(t, s, o)
	key := "invoice:INV-3:settle"
	if _, e := s.CreateProposal(ctx, o, tk.ID, r, "first", []connectors.Operation{opForTest(t, target, key, 1)}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateProposal(ctx, o, tk.ID, r, "changed", []connectors.Operation{opForTest(t, target, key, 5)}); e == nil {
		t.Fatal("changed payload under same business key accepted")
	}
}
func TestBusinessKeyBlocksAfterApproval(t *testing.T) {
	s, o, r, a1 := fixture(t)
	ctx := context.Background()
	tk := task(t, s, o, r)
	target := seedInventory(t, s, o)
	key := "invoice:INV-4:settle"
	p, e := s.CreateProposal(ctx, o, tk.ID, r, "first", []connectors.Operation{opForTest(t, target, key, 1)})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Decide(ctx, o, p.ID, r, "requester", "endorse", p.Revision, p.Digest, ""); e != nil {
		t.Fatal(e)
	}
	if e = s.Decide(ctx, o, p.ID, a1, "approver", "approve", p.Revision, p.Digest, ""); e != nil {
		t.Fatal(e)
	}
	tk2 := task(t, s, o, r)
	if _, e = s.CreateProposal(ctx, o, tk2.ID, r, "after approval", []connectors.Operation{opForTest(t, target, key, 1)}); e == nil {
		t.Fatal("dispatched business key reused")
	}
}
func TestEffectLinksBusinessOperation(t *testing.T) {
	s, o, r, a1 := fixture(t)
	ctx := context.Background()
	tk := task(t, s, o, r)
	target := seedInventory(t, s, o)
	key := "invoice:INV-5:settle"
	p, e := s.CreateProposal(ctx, o, tk.ID, r, "first", []connectors.Operation{opForTest(t, target, key, 1)})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Decide(ctx, o, p.ID, r, "requester", "endorse", p.Revision, p.Digest, ""); e != nil {
		t.Fatal(e)
	}
	if e = s.Decide(ctx, o, p.ID, a1, "approver", "approve", p.Revision, p.Digest, ""); e != nil {
		t.Fatal(e)
	}
	var linked int
	var st string
	e = s.WithOrg(ctx, o, func(tx pgx.Tx) error {
		if e := tx.QueryRow(ctx, "select count(*),coalesce(max(bo.status),'') from effects e join business_operations bo on bo.org_id=e.org_id and bo.id=e.business_operation_id where e.proposal_id=$1", p.ID).Scan(&linked, &st); e != nil {
			return e
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if linked != 1 || st != "dispatching" {
		t.Fatalf("linked=%d status=%s", linked, st)
	}
}
func TestKeyMovesWithinTaskButNotAcrossTasks(t *testing.T) {
	s, o, r, a := fixture(t)
	ctx := context.Background()
	key := "regress:lineage:1"
	target := seedInventory(t, s, o)
	first := task(t, s, o, r)

	rev1, e := s.CreateProposal(ctx, o, first.ID, r, "first", []connectors.Operation{opForTest(t, target, key, 1)})
	if e != nil {
		t.Fatal(e)
	}
	// Same task, same intent, deliberately re-prepared: a new revision takes the key.
	rev2, e := s.CreateProposal(ctx, o, first.ID, r, "corrected", []connectors.Operation{opForTest(t, target, key, 1)})
	if e != nil {
		t.Fatalf("revision within the same task must be allowed: %v", e)
	}
	if rev2.Revision != 2 {
		t.Fatalf("expected revision 2, got %d", rev2.Revision)
	}
	old, e := s.GetProposal(ctx, o, rev1.ID)
	if e != nil {
		t.Fatal(e)
	}
	if old.Status != "superseded" {
		t.Fatalf("previous revision should be superseded, got %s", old.Status)
	}
	// Exactly one reservation exists for the key, held by the live revision.
	var holder string
	var count int
	if e := s.WithOrg(ctx, o, func(tx pgx.Tx) error {
		if e := tx.QueryRow(ctx, "SELECT count(*) FROM business_operations WHERE org_id=$1 AND business_key=$2", o, key).Scan(&count); e != nil {
			return e
		}
		return tx.QueryRow(ctx, "SELECT coalesce(proposal_id,'') FROM business_operations WHERE org_id=$1 AND business_key=$2", o, key).Scan(&holder)
	}); e != nil {
		t.Fatal(e)
	}
	if count != 1 || holder != rev2.ID {
		t.Fatalf("reservation not moved cleanly: count=%d holder=%s want=%s", count, holder, rev2.ID)
	}
	// A different task claiming the same official operation is refused.
	second := task(t, s, o, r)
	if _, e := s.CreateProposal(ctx, o, second.ID, r, "other task", []connectors.Operation{opForTest(t, target, key, 1)}); e == nil {
		t.Fatal("cross-task reuse of a business key was accepted")
	}
	// The live revision remains fully actionable afterwards.
	if e := s.Decide(ctx, o, rev2.ID, r, "requester", "endorse", rev2.Revision, rev2.Digest, ""); e != nil {
		t.Fatalf("live revision unusable after a refused cross-task claim: %v", e)
	}
	if e := s.Decide(ctx, o, rev2.ID, a, "approver", "approve", rev2.Revision, rev2.Digest, ""); e != nil {
		t.Fatalf("approval after refused claim failed: %v", e)
	}
	// Once dispatched, the key is spent even within the task.
	if _, e := s.CreateProposal(ctx, o, first.ID, r, "after approval", []connectors.Operation{opForTest(t, target, key, 1)}); e == nil {
		t.Fatal("spent business key was reused after approval")
	}
}

func TestInvalidBusinessKeyRejected(t *testing.T) {
	s, o, r, _ := fixture(t)
	ctx := context.Background()
	tk := task(t, s, o, r)
	target := seedInventory(t, s, o)
	if _, e := s.CreateProposal(ctx, o, tk.ID, r, "bad", []connectors.Operation{opForTest(t, target, "bad key with spaces and $", 1)}); e == nil {
		t.Fatal("unsafe business key accepted")
	}
}
func task(t *testing.T, s *Store, o, r string) Task {
	t.Helper()
	tk, e := s.CreateTask(context.Background(), o, "business op task", "", r, "", "")
	if e != nil {
		t.Fatal(e)
	}
	return tk
}
func seedInventory(t *testing.T, s *Store, o string) string {
	t.Helper()
	id := platform.NewID()
	e := s.WithOrg(context.Background(), o, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(), "INSERT INTO simulator_records(id,org_id,integration,version,data) VALUES($1,$2,'inventory',1,'{\"quantity\":10}')", id, o)
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func opForTest(t *testing.T, target, key string, delta int) connectors.Operation {
	t.Helper()
	d, _ := json.Marshal(map[string]int{"delta": delta})
	return connectors.Operation{ID: platform.NewID(), Integration: "inventory", Action: "adjust", BusinessKey: key, TargetID: target, ExpectedVersion: 1, Payload: d}
}
