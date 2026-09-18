package api

import "testing"

// The handoff inbox is scoped to the parties involved: the creator and the
// recipient see it, nobody else does.
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

// The lifecycle reply endpoints exist and enforce who may take each action.
// Previously only "accept" existed, so a recipient who could not do the work had
// no way to say so and an unanswered offer sat forever.
func TestHandoffLifecycleEndpoints(t *testing.T) {
	f := newFixture(t)
	_, task := f.do("req", "POST", "/api/v1/tasks", map[string]any{"title": "lifecycle"})
	code, h := f.do("req", "POST", "/api/v1/tasks/"+id(task)+"/handoffs",
		map[string]any{"recipient_id": "org-fixture-a-approver", "role": "assignee", "summary": "please take this"})
	if code != 201 {
		t.Fatalf("offer %d %v", code, h)
	}
	// A decline with no reason is refused rather than silently recorded.
	if c, _ := f.do("app", "POST", "/api/v1/handoffs/"+id(h)+"/decline", map[string]any{}); c != 422 {
		t.Fatalf("empty decline reason: got %d want 422", c)
	}
	// The creator cannot decline their own offer.
	if c, _ := f.do("req", "POST", "/api/v1/handoffs/"+id(h)+"/decline", map[string]any{"reason": "x"}); c == 200 {
		t.Fatal("creator was allowed to decline their own handoff")
	}
	// The recipient asks a question; the offer stays open.
	if c, _ := f.do("app", "POST", "/api/v1/handoffs/"+id(h)+"/clarify", map[string]any{"reason": "which supplier?"}); c != 200 {
		t.Fatalf("clarify: got %d want 200", c)
	}
	code, body := f.do("app", "GET", "/api/v1/handoffs/"+id(h), nil)
	if code != 200 {
		t.Fatalf("peek %d", code)
	}
	if body["state"] != "clarification_requested" {
		t.Fatalf("state %q want clarification_requested", body["state"])
	}
	if body["reason"] != "which supplier?" {
		t.Fatalf("clarification question not exposed: %v", body["reason"])
	}
	if body["expires_at"] == "" {
		t.Fatal("offer should carry an expiry so it cannot sit forever")
	}
	// The recipient declines; the handoff is terminal.
	if c, _ := f.do("app", "POST", "/api/v1/handoffs/"+id(h)+"/decline", map[string]any{"reason": "not enough context"}); c != 200 {
		t.Fatalf("decline: got %d want 200", c)
	}
	if c, _ := f.do("app", "POST", "/api/v1/handoffs/"+id(h)+"/accept", nil); c == 200 {
		t.Fatal("a declined handoff was accepted")
	}

	// A second offer, this time withdrawn by its creator.
	_, task2 := f.do("req", "POST", "/api/v1/tasks", map[string]any{"title": "withdraw"})
	_, h2 := f.do("req", "POST", "/api/v1/tasks/"+id(task2)+"/handoffs",
		map[string]any{"recipient_id": "org-fixture-a-approver", "role": "assignee", "summary": "take this"})
	if c, _ := f.do("app", "POST", "/api/v1/handoffs/"+id(h2)+"/cancel", map[string]any{"reason": "recipient tries"}); c == 200 {
		t.Fatal("recipient was allowed to cancel the creator's offer")
	}
	if c, _ := f.do("req", "POST", "/api/v1/handoffs/"+id(h2)+"/cancel", map[string]any{"reason": "no longer needed"}); c != 200 {
		t.Fatalf("creator cancel: got %d want 200", c)
	}
	_, after := f.do("req", "GET", "/api/v1/handoffs/"+id(h2), nil)
	if after["state"] != "cancelled" {
		t.Fatalf("state %q want cancelled", after["state"])
	}
}
