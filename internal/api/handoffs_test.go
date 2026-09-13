package api

import "testing"

func TestHandoffInboxScoped(t *testing.T) {
	f := newFixture(t)
	_, task := f.do("req", "POST", "/api/v1/tasks", map[string]any{"title": "handoff inbox"})
	code, h := f.do("req", "POST", "/api/v1/tasks/"+id(task)+"/handoffs", map[string]any{"recipient_id": "org-fixture-a-approver", "role": "owner", "summary": "review ownership"})
	if code != 201 {
		t.Fatalf("offer %d %v", code, h)
	}
	for _, tok := range []string{"req", "app", "other"} {
		code, body := f.do(tok, "GET", "/api/v1/handoffs", nil)
		if code != 200 {
			t.Fatalf("inbox %s: %d", tok, code)
		}
		found := false
		for _, raw := range body["items"].([]any) {
			item := raw.(map[string]any)
			if item["id"] == id(h) {
				found = true
				if item["created_by"] != "org-fixture-a-requester" {
					t.Fatal("missing creator")
				}
			}
		}
		if found != (tok != "other") {
			t.Fatalf("wrong scope %s found %v", tok, found)
		}
	}
}
