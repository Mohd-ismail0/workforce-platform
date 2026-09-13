package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"workforce.local/platform/internal/platform"
	"workforce.local/platform/internal/store"
)

type apiFixture struct {
	t      *testing.T
	st     *store.Store
	h      http.Handler
	tokens map[string]string
}

func newFixture(t *testing.T) *apiFixture {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL required for PostgreSQL integration tests")
	}
	st, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := store.Seed(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{"req": "org-fixture-a-requester", "app": "org-fixture-a-approver", "other": "org-fixture-b-requester"}
	cfg := platform.Config{AuthMode: "local", Tokens: map[string]platform.Identity{
		"req":   {ID: ids["req"], OrgID: "org-fixture-a", Role: "requester", Name: "Requester"},
		"app":   {ID: ids["app"], OrgID: "org-fixture-a", Role: "approver", Name: "Approver"},
		"other": {ID: ids["other"], OrgID: "org-fixture-b", Role: "requester", Name: "Other"},
	}}
	return &apiFixture{t: t, st: st, h: New(cfg, st).Handler(), tokens: ids}
}
func (f *apiFixture) do(tok, method, path string, body any) (int, map[string]any) {
	f.t.Helper()
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(method, path, nil)
	} else {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest(method, path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	var v map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	return w.Code, v
}
func id(v map[string]any) string { return v["id"].(string) }
func TestHTTPProjectTaskSnakeCaseAndOrgIsolation(t *testing.T) {
	f := newFixture(t)
	status, p := f.do("req", "POST", "/api/v1/projects", map[string]any{"name": "P", "description": "D"})
	if status != 201 {
		t.Fatalf("project: %d %#v", status, p)
	}
	if _, ok := p["org_id"]; !ok {
		t.Fatalf("missing org_id: %#v", p)
	}
	status, task := f.do("req", "POST", "/api/v1/tasks", map[string]any{"title": "T", "description": "D", "project_id": id(p)})
	if status != 201 {
		t.Fatalf("task: %d %#v", status, task)
	}
	status, got := f.do("other", "GET", "/api/v1/tasks/"+id(task), nil)
	if status != 404 || got["error"].(map[string]any)["code"] != "not_found" {
		t.Fatalf("isolation: %d %#v", status, got)
	}
	status, _ = f.do("req", "POST", "/api/v1/tasks", map[string]any{"title": "bad", "org_id": "org-fixture-b"})
	if status != 201 {
		t.Fatalf("org header/body must not alter auth: %d", status)
	}
}
func TestHTTPProposalAuthorizationDigestAndIdempotency(t *testing.T) {
	f := newFixture(t)
	_, task := f.do("req", "POST", "/api/v1/tasks", map[string]any{"title": "T"})
	tid := id(task)
	_, records := f.do("req", "GET", "/api/v1/integrations/inventory/records", nil)
	var targetVersion any
	for _, raw := range records["items"].([]any) {
		rec := raw.(map[string]any)
		if rec["id"] == "org-fixture-a-inventory" {
			targetVersion = rec["version"]
		}
	}
	status, p := f.do("req", "POST", "/api/v1/tasks/"+tid+"/proposals", map[string]any{"summary": "S", "operations": []map[string]any{{"integration": "inventory", "action": "adjust", "target_id": "org-fixture-a-inventory", "expected_version": targetVersion, "payload": map[string]any{"delta": 1}}}})
	if status != 201 {
		t.Fatalf("proposal: %d %#v", status, p)
	}
	pid, dig := id(p), p["digest"].(string)
	rev := int(p["revision"].(float64))
	status, _ = f.do("app", "POST", "/api/v1/proposals/"+pid+"/endorse", map[string]any{"revision": rev, "digest": dig})
	if status != 409 {
		t.Fatalf("wrong endorser: %d", status)
	}
	status, _ = f.do("req", "POST", "/api/v1/proposals/"+pid+"/endorse", map[string]any{"revision": rev, "digest": "stale"})
	if status != 409 {
		t.Fatalf("stale digest: %d", status)
	}
	status, _ = f.do("req", "POST", "/api/v1/proposals/"+pid+"/endorse", map[string]any{"revision": rev, "digest": dig})
	if status != 200 {
		t.Fatalf("endorse: %d", status)
	}
	status, _ = f.do("req", "POST", "/api/v1/proposals/"+pid+"/approve", map[string]any{"revision": rev, "digest": dig})
	if status != 409 {
		t.Fatalf("same human approve: %d", status)
	}
	status, _ = f.do("app", "POST", "/api/v1/proposals/"+pid+"/approve", map[string]any{"revision": rev, "digest": dig})
	if status != 200 {
		t.Fatalf("approve: %d", status)
	}
	status, _ = f.do("app", "POST", "/api/v1/proposals/"+pid+"/approve", map[string]any{"revision": rev, "digest": dig})
	if status != 200 {
		t.Fatalf("idempotent approve: %d", status)
	}
}
func TestHTTPDependencyCycleAndHandoffRecipient(t *testing.T) {
	f := newFixture(t)
	_, a := f.do("req", "POST", "/api/v1/tasks", map[string]any{"title": "A"})
	_, b := f.do("req", "POST", "/api/v1/tasks", map[string]any{"title": "B"})
	if s, _ := f.do("req", "POST", "/api/v1/tasks/"+id(a)+"/dependencies", map[string]any{"parent_id": id(b)}); s != 201 {
		t.Fatalf("dep: %d", s)
	}
	if s, _ := f.do("req", "POST", "/api/v1/tasks/"+id(b)+"/dependencies", map[string]any{"parent_id": id(a)}); s != 409 {
		t.Fatalf("cycle: %d", s)
	}
	s, h := f.do("req", "POST", "/api/v1/tasks/"+id(a)+"/handoffs", map[string]any{"recipient_id": "org-fixture-a-approver", "role": "owner", "summary": "x"})
	if s != 201 {
		t.Fatalf("handoff: %d %#v", s, h)
	}
	if s, _ = f.do("req", "POST", "/api/v1/handoffs/"+id(h)+"/accept", nil); s != 404 {
		t.Fatalf("wrong recipient: %d", s)
	}
	if s, _ = f.do("app", "POST", "/api/v1/handoffs/"+id(h)+"/accept", nil); s != 200 {
		t.Fatalf("accept: %d", s)
	}
}
