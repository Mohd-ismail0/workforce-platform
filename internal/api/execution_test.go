package api

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"testing"
	"time"
	"workforce.local/platform/internal/platform"
)

func TestRiverAppliesThreeIntegrationsAndReadback(t *testing.T) {
	f := newFixture(t)
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
	cases := []struct {
		integration, action string
		before, payload     map[string]any
	}{
		{"inventory", "adjust", map[string]any{"quantity": 10}, map[string]any{"delta": 2}},
		{"documents", "update", map[string]any{"title": "old", "content": "before"}, map[string]any{"content": "after"}},
		{"mail", "send", map[string]any{}, map[string]any{"recipients": []string{"supplier@example.test"}, "subject": "reviewed", "body": "approved text", "bcc": []string{}, "attachments": []string{}}},
	}
	for _, tc := range cases {
		t.Run(tc.integration, func(t *testing.T) {
			target := platform.NewID()
			raw, _ := json.Marshal(tc.before)
			if err := f.st.WithOrg(ctx, "org-fixture-a", func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, "INSERT INTO simulator_records(id,org_id,integration,version,data) VALUES($1,'org-fixture-a',$2,1,$3)", target, tc.integration, raw)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			code, task := f.do("req", "POST", "/api/v1/tasks", map[string]any{"title": "execution " + tc.integration})
			if code != 201 {
				t.Fatalf("task %d %v", code, task)
			}
			code, p := f.do("req", "POST", "/api/v1/tasks/"+id(task)+"/proposals", map[string]any{"summary": "approved simulation", "operations": []map[string]any{{"id": platform.NewID(), "integration": tc.integration, "action": tc.action, "target_id": target, "expected_version": 1, "payload": tc.payload}}})
			if code != 201 {
				t.Fatalf("proposal %d %v", code, p)
			}
			decision := map[string]any{"revision": p["revision"], "digest": p["digest"]}
			for _, a := range []struct{ token, action string }{{"req", "endorse"}, {"app", "approve"}, {"app", "approve"}} {
				code, r := f.do(a.token, "POST", "/api/v1/proposals/"+id(p)+"/"+a.action, decision)
				if code != 200 {
					t.Fatalf("decision %d %v", code, r)
				}
			}
			deadline := time.Now().Add(15 * time.Second)
			for {
				_, state := f.do("req", "GET", "/api/v1/proposals/"+id(p), nil)
				if state["status"] == "done" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("River did not finish: %v", state)
				}
				time.Sleep(100 * time.Millisecond)
			}
			var version int
			var result []byte
			var receipts int
			err := f.st.WithOrg(ctx, "org-fixture-a", func(tx pgx.Tx) error {
				if err := tx.QueryRow(ctx, "SELECT version,data FROM simulator_records WHERE id=$1", target).Scan(&version, &result); err != nil {
					return err
				}
				return tx.QueryRow(ctx, "SELECT count(*) FROM receipts r JOIN effects e ON e.id=r.effect_id WHERE e.proposal_id=$1", id(p)).Scan(&receipts)
			})
			if err != nil {
				t.Fatal(err)
			}
			if version != 2 || receipts != 1 {
				t.Fatalf("duplicate/missing effect version=%d receipts=%d", version, receipts)
			}
			var got map[string]any
			json.Unmarshal(result, &got)
			if tc.integration == "inventory" && got["quantity"] != float64(12) {
				t.Fatalf("readback %s", result)
			}
			_, done := f.do("req", "GET", "/api/v1/tasks/"+id(task), nil)
			if done["status"] != "done" {
				t.Fatalf("task not complete: %v", done)
			}
		})
	}
}
