package api

import (
	"testing"

	"workforce.local/platform/internal/platform"
)

func TestPendingDecisionInboxAndAgentRegistry(t *testing.T) {
	f := newFixture(t)
	code, a := f.do("req", "POST", "/api/v1/agents", map[string]any{"name": "Test registered agent", "harness": "opencode", "capabilities": []string{"prepare"}})
	if code != 201 {
		t.Fatalf("agent create %d %v", code, a)
	}
	code, listing := f.do("req", "GET", "/api/v1/agents", nil)
	if code != 200 {
		t.Fatal(code)
	}
	found := false
	for _, raw := range listing["items"].([]any) {
		if raw.(map[string]any)["id"] == id(a) {
			found = true
		}
	}
	if !found {
		t.Fatal("registered agent missing")
	}
	_, task := f.do("req", "POST", "/api/v1/tasks", map[string]any{"title": "decision inbox"})
	_, records := f.do("req", "GET", "/api/v1/integrations/documents/records", nil)
	rec := records["items"].([]any)[0].(map[string]any)
	code, p := f.do("req", "POST", "/api/v1/tasks/"+id(task)+"/proposals", map[string]any{"summary": "document review", "operations": []map[string]any{{"integration": "documents", "action": "update", "business_key": platform.NewID(), "target_id": rec["id"], "expected_version": rec["version"], "payload": map[string]any{"title": "Reviewed"}}}})
	if code != 201 {
		t.Fatalf("proposal %d %v", code, p)
	}
	_, inbox := f.do("req", "GET", "/api/v1/decisions", nil)
	found = false
	for _, raw := range inbox["items"].([]any) {
		d := raw.(map[string]any)
		if d["proposal_id"] == id(p) && d["kind"] == "endorse" {
			found = true
		}
	}
	if !found {
		t.Fatal("pending endorsement absent from requester inbox")
	}
}
