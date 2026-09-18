package api

import "testing"

func boardItem(t *testing.T, items []any, taskID string) (map[string]any, bool) {
	t.Helper()
	for _, raw := range items {
		m, ok := raw.(map[string]any)
		if ok && m["task_id"] == taskID {
			return m, true
		}
	}
	return nil, false
}

// The board is a server-side projection of the CALLER's work, labelled with why
// each item is theirs. Identity comes from the authenticated principal, so one
// person cannot ask for another's board.
func TestWorkBoardEndpoints(t *testing.T) {
	f, _, _ := freshOrgFixture(t)
	_, meReq := f.do("req", "GET", "/api/v1/me", nil)
	reqID, _ := meReq["id"].(string)

	// Work I am accountable for.
	_, mine := f.do("req", "POST", "/api/v1/tasks", map[string]any{"title": "I own this"})
	// Work I execute for someone else: app owns it, I am the assignee.
	_, theirs := f.do("app", "POST", "/api/v1/tasks", map[string]any{"title": "I execute this", "assignee_id": reqID})
	// An offer to me that I have not answered.
	_, offered := f.do("app", "POST", "/api/v1/tasks", map[string]any{"title": "offered to me"})
	if code, h := f.do("app", "POST", "/api/v1/tasks/"+id(offered)+"/handoffs",
		map[string]any{"recipient_id": reqID, "role": "owner", "summary": "take ownership"}); code != 201 {
		t.Fatalf("offer: %d %v", code, h)
	}

	code, body := f.do("req", "GET", "/api/v1/work/board", nil)
	if code != 200 {
		t.Fatalf("board: %d", code)
	}
	items := body["items"].([]any)

	if it, ok := boardItem(t, items, id(mine)); !ok || it["relevance"] != "accountable" {
		t.Fatalf("owned task: %v ok=%v want accountable", it, ok)
	}
	if it, ok := boardItem(t, items, id(theirs)); !ok || it["relevance"] != "executing" {
		t.Fatalf("assigned task: %v ok=%v want executing", it, ok)
	}
	if it, ok := boardItem(t, items, id(offered)); !ok || it["relevance"] != "handoff_offered" {
		t.Fatalf("offered task: %v ok=%v want handoff_offered", it, ok)
	}

	// The other principal's board is a different board: it does not contain my
	// work, and it labels its own work by its own relationship.
	code, otherBody := f.do("app", "GET", "/api/v1/work/board", nil)
	if code != 200 {
		t.Fatalf("other board: %d", code)
	}
	otherItems := otherBody["items"].([]any)
	if _, ok := boardItem(t, otherItems, id(mine)); ok {
		t.Fatal("my work appeared on someone else's board")
	}
	if it, ok := boardItem(t, otherItems, id(theirs)); !ok || it["relevance"] != "accountable" {
		t.Fatalf("owner sees own task as: %v ok=%v want accountable", it, ok)
	}

	// A decision waiting on me is deliberately NOT on the board. My commitments
	// and my decisions are different obligations; folding one into the other
	// turns answering a question into doing the work.
	for _, raw := range items {
		if raw.(map[string]any)["relevance"] == "awaiting_decision" {
			t.Fatal("a decision leaked into the commitments board")
		}
	}
	// And the decision inbox remains its own surface.
	if code, _ := f.do("req", "GET", "/api/v1/decisions", nil); code != 200 {
		t.Fatalf("decisions endpoint: %d", code)
	}
}
