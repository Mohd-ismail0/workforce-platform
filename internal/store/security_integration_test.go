package store

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"os"
	"testing"
	"workforce.local/platform/internal/connectors"
	"workforce.local/platform/internal/platform"
)

func fixture(t *testing.T) (*Store, string, string, string) {
	t.Helper()
	u := os.Getenv("DATABASE_URL")
	if u == "" {
		t.Skip("DATABASE_URL required")
	}
	ctx := context.Background()
	s, e := Open(ctx, u)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	org := platform.NewID()
	req := platform.NewID()
	app := platform.NewID()
	e = s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "INSERT INTO organizations(id,name) VALUES($1,'security fixture')", org)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, "INSERT INTO principals(id,org_id,name,role) VALUES($1,$3,'requester','requester'),($2,$3,'approver','approver')", req, app, org)
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	return s, org, req, app
}
func proposal(t *testing.T, s *Store, org, req string) (Task, Proposal) {
	t.Helper()
	ctx := context.Background()
	task, e := s.CreateTask(ctx, org, "security task", "", req, "", "")
	if e != nil {
		t.Fatal(e)
	}
	target := platform.NewID()
	e = s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "INSERT INTO simulator_records(id,org_id,integration,version,data) VALUES($1,$2,'inventory',1,'{\"quantity\":10}')", target, org)
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	p, e := s.CreateProposal(ctx, org, task.ID, req, "test", []connectors.Operation{{ID: platform.NewID(), Integration: "inventory", Action: "adjust", TargetID: target, ExpectedVersion: 1, Payload: json.RawMessage(`{"delta":1}`)}})
	if e != nil {
		t.Fatal(e)
	}
	return task, p
}
func TestApprovalRequiresEndorsement(t *testing.T) {
	s, o, r, a := fixture(t)
	_, p := proposal(t, s, o, r)
	if e := s.Decide(context.Background(), o, p.ID, a, "approver", "approve", p.Revision, p.Digest, ""); e == nil {
		t.Fatal("approval without endorsement accepted")
	}
}
func TestRevokedApproverCannotExecute(t *testing.T) {
	s, o, r, a := fixture(t)
	_, p := proposal(t, s, o, r)
	ctx := context.Background()
	for _, d := range []struct{ id, role, kind string }{{r, "requester", "endorse"}, {a, "approver", "approve"}} {
		if e := s.Decide(ctx, o, p.ID, d.id, d.role, d.kind, p.Revision, p.Digest, ""); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.WithOrg(ctx, o, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "UPDATE principals SET active=false WHERE id=$1", a)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	_ = s.executeProposal(ctx, connectors.NewRegistry(), o, p.ID)
	got, e := s.GetProposal(ctx, o, p.ID)
	if e != nil {
		t.Fatal(e)
	}
	if got.Status == "done" {
		t.Fatal("revoked authority executed")
	}
}
func TestDependencyPreventsExecution(t *testing.T) {
	s, o, r, a := fixture(t)
	task, p := proposal(t, s, o, r)
	ctx := context.Background()
	parent, e := s.CreateTask(ctx, o, "dependency", "", r, "", "")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.AddDependency(ctx, o, task.ID, parent.ID); e != nil {
		t.Fatal(e)
	}
	if e = s.Decide(ctx, o, p.ID, r, "requester", "endorse", p.Revision, p.Digest, ""); e != nil {
		t.Fatal(e)
	}
	if e = s.Decide(ctx, o, p.ID, a, "approver", "approve", p.Revision, p.Digest, ""); e != nil {
		return
	}
	_ = s.executeProposal(ctx, connectors.NewRegistry(), o, p.ID)
	got, _ := s.GetProposal(ctx, o, p.ID)
	if got.Status == "done" {
		t.Fatal("unfinished dependency ignored")
	}
}
