package api

import "testing"

// Milestone planning: a milestone is met on declared evidence and valid
// dependency state, never because its tasks happened to move.
func TestMilestonePlanningEndpoints(t *testing.T) {
	f, _, _ := freshOrgFixture(t)

	code, proj := f.do("req", "POST", "/api/v1/projects", map[string]any{"name": "Apollo"})
	if code != 201 {
		t.Fatalf("project: %d %v", code, proj)
	}
	base := "/api/v1/projects/" + id(proj) + "/milestones"

	// No acceptance evidence means "met" would mean nothing, so it is refused.
	if c, _ := f.do("req", "POST", base, map[string]any{"name": "Cutover"}); c != 422 {
		t.Fatalf("milestone without evidence: got %d want 422", c)
	}

	code, first := f.do("req", "POST", base, map[string]any{
		"name":                "Reconcile",
		"acceptance_evidence": "All 42 SKUs reconciled with zero variance",
		"committed_date":      "2026-03-31",
		"forecast_date":       "2026-04-15",
	})
	if code != 201 {
		t.Fatalf("milestone: %d %v", code, first)
	}
	if first["committed_date"] != "2026-03-31" || first["forecast_date"] != "2026-04-15" {
		t.Fatalf("dates not recorded: %v", first)
	}
	if first["status"] != "planned" {
		t.Fatalf("status %q want planned", first["status"])
	}
	// A malformed date is refused rather than silently dropped.
	if c, _ := f.do("req", "POST", base, map[string]any{
		"name": "Bad", "acceptance_evidence": "x", "committed_date": "31/03/2026",
	}); c != 422 {
		t.Fatalf("malformed date: got %d want 422", c)
	}

	_, second := f.do("req", "POST", base, map[string]any{
		"name": "Send replies", "acceptance_evidence": "Ten replies sent and read back",
	})
	if code, _ := f.do("req", "POST", "/api/v1/milestones/"+id(second)+"/dependencies",
		map[string]any{"parent_id": id(first)}); code != 201 {
		t.Fatalf("dependency: %d", code)
	}

	// Completing the dependent milestone is refused while its prerequisite is
	// unmet, even with evidence in hand.
	if c, _ := f.do("req", "POST", "/api/v1/milestones/"+id(second)+"/complete",
		map[string]any{"evidence": "ten replies sent", "version": second["version"]}); c == 200 {
		t.Fatal("a milestone was met while its prerequisite was unmet")
	}
	// And completing with no evidence is refused outright.
	if c, _ := f.do("req", "POST", "/api/v1/milestones/"+id(first)+"/complete",
		map[string]any{"evidence": "", "version": first["version"]}); c != 422 {
		t.Fatalf("completion without evidence: got %d want 422", c)
	}

	// Meeting the prerequisite then unblocks the dependent milestone.
	if c, _ := f.do("req", "POST", "/api/v1/milestones/"+id(first)+"/complete",
		map[string]any{"evidence": "42/42 reconciled", "version": first["version"]}); c != 200 {
		t.Fatalf("meet prerequisite: %d", c)
	}
	_, refreshed := f.do("req", "GET", base, nil)
	var current map[string]any
	for _, raw := range refreshed["items"].([]any) {
		if m := raw.(map[string]any); m["id"] == id(second) {
			current = m
		}
	}
	if current == nil {
		t.Fatal("dependent milestone missing from the list")
	}
	if c, _ := f.do("req", "POST", "/api/v1/milestones/"+id(second)+"/complete",
		map[string]any{"evidence": "ten replies sent and verified", "version": current["version"]}); c != 200 {
		t.Fatalf("meet dependent milestone: %d", c)
	}

	// Revising a forecast must not move the commitment: a promise is not a guess.
	_, f2 := f.do("req", "POST", base, map[string]any{
		"name": "Third", "acceptance_evidence": "evidence", "committed_date": "2026-06-30",
	})
	code, revised := f.do("req", "POST", "/api/v1/milestones/"+id(f2)+"/forecast",
		map[string]any{"forecast_date": "2026-08-01"})
	if code != 200 {
		t.Fatalf("forecast: %d", code)
	}
	if revised["forecast_date"] != "2026-08-01" {
		t.Fatalf("forecast not revised: %v", revised["forecast_date"])
	}
	if revised["committed_date"] != "2026-06-30" {
		t.Fatalf("revising the forecast moved the commitment to %q", revised["committed_date"])
	}

	// Cycles are refused.
	if c, _ := f.do("req", "POST", "/api/v1/milestones/"+id(first)+"/dependencies",
		map[string]any{"parent_id": id(second)}); c == 201 {
		t.Fatal("a milestone dependency cycle was accepted")
	}

	// Another organization sees none of this.
	if code, other := f.do("other", "GET", base, nil); code != 200 {
		t.Fatalf("cross-org milestones: %d", code)
	} else if len(other["items"].([]any)) != 0 {
		t.Fatalf("cross-org milestone leak: %v", other["items"])
	}
}
