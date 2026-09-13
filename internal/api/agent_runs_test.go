package api

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"workforce.local/platform/internal/platform"
)

// freshOrgFixture builds a server scoped to brand new organisations. The shared
// development database keeps state between runs, so any assertion about "there is no
// active harness release yet" must start from an org that has never had one.
func freshOrgFixture(t *testing.T) (*apiFixture, string, string) {
	t.Helper()
	f := newFixture(t)
	org, other := platform.NewID(), platform.NewID()
	req, app, admin := platform.NewID(), platform.NewID(), platform.NewID()
	oreq := platform.NewID()
	ctx := context.Background()
	if e := f.st.WithOrg(ctx, org, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "INSERT INTO organizations(id,name) VALUES($1,'run org'),($2,'other org')", org, other)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, "INSERT INTO principals(id,org_id,name,role) VALUES($1,$4,'Requester','requester'),($2,$4,'Approver','approver'),($3,$4,'Admin','admin')", req, app, admin, org)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if e := f.st.WithOrg(ctx, other, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "INSERT INTO principals(id,org_id,name,role) VALUES($1,$2,'Other','requester')", oreq, other)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	f.h = New(platform.Config{AuthMode: "local", Tokens: map[string]platform.Identity{
		"req":   {ID: req, OrgID: org, Role: "requester", Name: "Requester"},
		"app":   {ID: app, OrgID: org, Role: "approver", Name: "Approver"},
		"admin": {ID: admin, OrgID: org, Role: "admin", Name: "Admin"},
		"other": {ID: oreq, OrgID: other, Role: "requester", Name: "Other"},
	}}, f.st).Handler()
	return f, org, admin
}

// activeHarnessRelease drives a harness release all the way to active, which is the
// only state that permits an agent run to start.
func activeHarnessRelease(t *testing.T, f *apiFixture, tok string) map[string]any {
	t.Helper()
	u := platform.NewID()
	code, rel := f.do(tok, "POST", "/api/v1/registry/releases", map[string]any{
		"family": "harness-fam-" + u[:8], "kind": "harness", "version": "1.0.0-" + u[8:16],
		"digest": "sha256:" + u, "requested_capabilities": []string{"prepare_proposal"},
		"simulation": true, "compatibility_range": ">=1",
	})
	if code != 201 {
		t.Fatalf("create harness release: %d %v", code, rel)
	}
	for _, step := range []string{"verified", "approved", "installed", "active"} {
		code, rel = f.do(tok, "POST", "/api/v1/registry/releases/"+id(rel)+"/transition",
			map[string]any{"expected_version": rel["revision"], "target_state": step, "reason": "test"})
		if code != 200 {
			t.Fatalf("transition to %s: %d %v", step, code, rel)
		}
	}
	if rel["state"] != "active" {
		t.Fatalf("harness release not active: %v", rel["state"])
	}
	return rel
}

func TestAgentRunGateReturnsConflictUntilHarnessIsActive(t *testing.T) {
	f, _, _ := freshOrgFixture(t)
	code, task := f.do("req", "POST", "/api/v1/tasks", map[string]any{"title": "run gate"})
	if code != 201 {
		t.Fatalf("task %d", code)
	}
	code, agent := f.do("req", "POST", "/api/v1/agents", map[string]any{"name": "Run Gate Agent", "harness": "simulator"})
	if code != 201 {
		t.Fatalf("agent %d %v", code, agent)
	}
	body := map[string]any{"agent_id": id(agent), "intent": "inventory"}

	code, resp := f.do("req", "POST", "/api/v1/tasks/"+id(task)+"/runs", body)
	if code != 409 {
		t.Fatalf("no active harness must be a conflict, got %d %v", code, resp)
	}
	e, _ := resp["error"].(map[string]any)
	if e == nil || e["message"] == "" || e["code"] != "no_active_harness" {
		t.Fatalf("409 must explain that an active harness release is required: %v", resp)
	}

	// A quarantined release still must not unlock runs.
	u := platform.NewID()
	code, qrel := f.do("admin", "POST", "/api/v1/registry/releases", map[string]any{
		"family": "harness-q-" + u[:8], "kind": "harness", "version": "1.0.0-" + u[8:16],
		"digest": "sha256:" + u, "requested_capabilities": []string{"prepare_proposal"}, "simulation": true,
	})
	if code != 201 {
		t.Fatalf("quarantined release %d %v", code, qrel)
	}
	if code, _ = f.do("req", "POST", "/api/v1/tasks/"+id(task)+"/runs", body); code != 409 {
		t.Fatalf("a quarantined harness must not unlock runs, got %d", code)
	}

	activeHarnessRelease(t, f, "admin")
	if code, resp = f.do("req", "POST", "/api/v1/tasks/"+id(task)+"/runs", body); code != 201 {
		t.Fatalf("active harness must allow a run: %d %v", code, resp)
	}
	if resp["status"] != "queued" || resp["harness"] != "simulator" {
		t.Fatalf("unexpected run: %v", resp)
	}
}

func TestAgentRunExecutesThroughRiverAndIsOrgScoped(t *testing.T) {
	f, org, _ := freshOrgFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	workerDone := make(chan error, 1)
	go func() { workerDone <- f.st.RunWorker(ctx, nil) }()
	defer func() {
		cancel()
		select {
		case err := <-workerDone:
			if err != nil {
				t.Log(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("worker stop timed out")
		}
	}()

	target := platform.NewID()
	if err := f.st.WithOrg(ctx, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO simulator_records(id,org_id,integration,version,data) VALUES($1,$2,'inventory',1,$3)", target, org, []byte(`{"quantity":5}`))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	activeHarnessRelease(t, f, "admin")

	code, task := f.do("req", "POST", "/api/v1/tasks", map[string]any{"title": "run end to end"})
	if code != 201 {
		t.Fatalf("task %d", code)
	}
	code, agent := f.do("req", "POST", "/api/v1/agents", map[string]any{"name": "End To End Agent", "harness": "simulator"})
	if code != 201 {
		t.Fatalf("agent %d %v", code, agent)
	}
	code, run := f.do("req", "POST", "/api/v1/tasks/"+id(task)+"/runs", map[string]any{"agent_id": id(agent), "intent": "inventory"})
	if code != 201 {
		t.Fatalf("start run %d %v", code, run)
	}

	deadline := time.Now().Add(30 * time.Second)
	var final map[string]any
	for {
		_, final = f.do("req", "GET", "/api/v1/runs/"+id(run), nil)
		if final["status"] == "succeeded" || final["status"] == "failed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run never completed: %v", final)
		}
		time.Sleep(150 * time.Millisecond)
	}
	if final["status"] != "succeeded" {
		t.Fatalf("run failed: %v", final)
	}
	pid, _ := final["proposal_id"].(string)
	if pid == "" {
		t.Fatalf("a succeeded run must link its proposal: %v", final)
	}
	// The prepared proposal must still await human endorsement: a run prepares work,
	// it never authorises it.
	_, p := f.do("req", "GET", "/api/v1/proposals/"+pid, nil)
	if p["status"] != "pending_endorsement" {
		t.Fatalf("run output must be unapproved, got status %v", p["status"])
	}

	_, list := f.do("req", "GET", "/api/v1/runs", nil)
	if items, _ := list["items"].([]any); len(items) == 0 {
		t.Fatal("run missing from the org listing")
	}
	if code, _ = f.do("other", "GET", "/api/v1/runs/"+id(run), nil); code != 404 {
		t.Fatalf("another org must not read this run, got %d", code)
	}
}
