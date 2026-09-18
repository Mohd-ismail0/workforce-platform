package api

import (
	"testing"
	"time"
)

// The organization model is only useful if it is reachable and if it answers
// questions about a point in time. This drives the endpoints the UI uses.
//
// It runs against a FRESH organization (freshOrgFixture creates unique
// principals), not the shared fixture org: the shared database persists, so a
// test that asserted on org-fixture-a would see its own previous run's rows and
// fail on the second run for the wrong reason.
func TestOrganizationEndpoints(t *testing.T) {
	f, _, _ := freshOrgFixture(t)

	// Resolve this org's principal ids through the public API rather than
	// reaching into the fixture.
	_, meReq := f.do("req", "GET", "/api/v1/me", nil)
	_, meApp := f.do("app", "GET", "/api/v1/me", nil)
	reqID, _ := meReq["id"].(string)
	appID, _ := meApp["id"].(string)
	if reqID == "" || appID == "" {
		t.Fatalf("could not resolve principals: req=%q app=%q", reqID, appID)
	}

	// Positions.
	code, team := f.do("req", "POST", "/api/v1/positions", map[string]any{"name": "Finance"})
	if code != 201 {
		t.Fatalf("create position: %d %v", code, team)
	}
	if code, _ := f.do("req", "POST", "/api/v1/positions", map[string]any{"name": ""}); code != 422 {
		t.Fatalf("empty position name: got %d want 422", code)
	}
	if code, body := f.do("req", "GET", "/api/v1/positions", nil); code != 200 {
		t.Fatalf("list positions: %d", code)
	} else if len(body["items"].([]any)) != 1 {
		t.Fatalf("expected exactly the one position in a fresh org, got %v", body["items"])
	}

	// A reporting relationship, then reading it back as of now.
	past := time.Now().Add(-2 * time.Hour).Format(time.RFC3339)
	code, rel := f.do("req", "POST", "/api/v1/relationships", map[string]any{
		"subject_id": reqID,
		"kind":       "reports_to",
		"object_id":  appID,
		"from":       past,
	})
	if code != 201 {
		t.Fatalf("create relationship: %d %v", code, rel)
	}
	if rel["valid_to"] != "" {
		t.Fatalf("open-ended relationship reported an end: %v", rel["valid_to"])
	}
	code, body := f.do("req", "GET", "/api/v1/relationships", nil)
	if code != 200 {
		t.Fatalf("list relationships: %d", code)
	}
	if len(body["items"].([]any)) != 1 {
		t.Fatalf("expected the one current relationship, got %v", body["items"])
	}

	// A second concurrent manager is a contradiction and is refused.
	if code, _ := f.do("req", "POST", "/api/v1/relationships", map[string]any{
		"subject_id": reqID,
		"kind":       "reports_to",
		"object_id":  appID,
	}); code == 201 {
		t.Fatal("a second concurrent manager was accepted")
	}

	// Matrix membership may overlap: a person is in several teams at once.
	mk := func(name string) string {
		_, p := f.do("req", "POST", "/api/v1/positions", map[string]any{"name": name})
		return id(p)
	}
	for _, pos := range []string{mk("Ops"), mk("Risk")} {
		if code, _ := f.do("req", "POST", "/api/v1/relationships", map[string]any{
			"subject_id": reqID,
			"kind":       "member_of",
			"object_id":  pos,
		}); code != 201 {
			t.Fatalf("concurrent team membership refused: %d", code)
		}
	}

	// History is answerable: asking about a time before it began returns nothing.
	before := time.Now().Add(-5 * time.Hour).Format(time.RFC3339)
	code, body = f.do("req", "GET", "/api/v1/relationships?as_of="+before, nil)
	if code != 200 {
		t.Fatalf("as_of query: %d", code)
	}
	if n := len(body["items"].([]any)); n != 0 {
		t.Fatalf("history before the relationship began was not empty: %d items", n)
	}
	// An unparseable instant is refused rather than silently treated as now.
	if code, _ := f.do("req", "GET", "/api/v1/relationships?as_of=not-a-time", nil); code != 422 {
		t.Fatalf("bad as_of: got %d want 422", code)
	}

	// Ending it closes the interval; it is no longer current, but the period it
	// was true remains readable.
	if code, _ := f.do("req", "POST", "/api/v1/relationships/"+id(rel)+"/end", map[string]any{}); code != 200 {
		t.Fatalf("end relationship: %d", code)
	}
	code, body = f.do("req", "GET", "/api/v1/relationships", nil)
	if code != 200 {
		t.Fatal(code)
	}
	for _, raw := range body["items"].([]any) {
		if raw.(map[string]any)["id"] == id(rel) {
			t.Fatal("ended relationship still reported as current")
		}
	}
	mid := time.Now().Add(-time.Hour).Format(time.RFC3339)
	_, pastBody := f.do("req", "GET", "/api/v1/relationships?as_of="+mid, nil)
	found := false
	for _, raw := range pastBody["items"].([]any) {
		if raw.(map[string]any)["id"] == id(rel) {
			found = true
		}
	}
	if !found {
		t.Fatal("history was erased by ending the relationship")
	}

	// Another organization sees none of this. `other` is a principal in a
	// different fresh org.
	if code, other := f.do("other", "GET", "/api/v1/relationships", nil); code != 200 {
		t.Fatalf("cross-org list: %d", code)
	} else if len(other["items"].([]any)) != 0 {
		t.Fatalf("cross-org relationship leak: %v", other["items"])
	}
}
