package api

import "testing"

// Capability configuration: activation grants what the release requested, an
// operator can widen it only as an admin, and both the effective configuration
// and a prospective "what would be denied" are inspectable.
func TestCapabilityConfigurationEndpoints(t *testing.T) {
	f, _, _ := freshOrgFixture(t)

	// A harness release, submitted with a request and never with a grant.
	code, rel := f.do("admin", "POST", "/api/v1/registry/releases", map[string]any{
		"family": "harness-cap-api", "kind": "harness", "version": "1.0.0-api",
		"digest": "sha256:cap-api", "requested_capabilities": []string{"prepare_proposal"},
		"simulation": true, "manifest": map[string]any{"runner_id": "simulator"},
	})
	if code != 201 {
		t.Fatalf("release: %d %v", code, rel)
	}
	if granted := rel["granted_capabilities"].([]any); len(granted) != 0 {
		t.Fatalf("a submission carried grants: %v", granted)
	}
	for _, step := range []string{"verified", "approved", "installed", "active"} {
		code, rel = f.do("admin", "POST", "/api/v1/registry/releases/"+id(rel)+"/transition",
			map[string]any{"expected_version": rel["revision"], "target_state": step, "reason": "test"})
		if code != 200 {
			t.Fatalf("transition to %s: %d %v", step, code, rel)
		}
	}
	// Activation is the operator's approval, so the grant becomes real there.
	if granted := rel["granted_capabilities"].([]any); len(granted) != 1 {
		t.Fatalf("activation granted nothing: %v", granted)
	}

	// An agent definition may declare anything; the ceiling applies at admission.
	_, ag := f.do("req", "POST", "/api/v1/agents", map[string]any{
		"name": "Cap Agent", "harness": "simulator", "capabilities": []string{"prepare_proposal"},
	})
	code, cfg := f.do("req", "GET", "/api/v1/agents/"+id(ag)+"/configuration", nil)
	if code != 200 {
		t.Fatalf("configuration: %d %v", code, cfg)
	}
	if got := cfg["permitted"].([]any); len(got) != 1 || got[0] != "prepare_proposal" {
		t.Fatalf("permitted = %v", got)
	}
	if got := cfg["declared"].([]any); len(got) != 1 {
		t.Fatalf("declared = %v", got)
	}
	if got := cfg["denied"].([]any); len(got) != 0 {
		t.Fatalf("an agent declares outside its ceiling: %v", got)
	}

	// "What would be denied" is answerable before anything is launched.
	code, check := f.do("req", "POST", "/api/v1/capabilities/check", map[string]any{
		"harness": "simulator", "capabilities": []string{"prepare_proposal", "approve:work"},
	})
	if code != 200 {
		t.Fatalf("check: %d %v", code, check)
	}
	if got := check["permitted"].([]any); len(got) != 1 || got[0] != "prepare_proposal" {
		t.Fatalf("check permitted = %v", got)
	}
	if got := check["denied"].([]any); len(got) != 1 || got[0] != "approve:work" {
		t.Fatalf("check denied = %v", got)
	}

	// A non-admin cannot widen the ceiling; an admin can, and it accumulates.
	if c, _ := f.do("req", "POST", "/api/v1/registry/releases/"+id(rel)+"/grant",
		map[string]any{"expected_version": rel["revision"], "capabilities": []string{"write:inventory"}}); c == 200 {
		t.Fatal("a non-admin granted a capability")
	}
	code, widened := f.do("admin", "POST", "/api/v1/registry/releases/"+id(rel)+"/grant",
		map[string]any{"expected_version": rel["revision"], "capabilities": []string{"write:inventory"}})
	if code != 200 {
		t.Fatalf("admin grant: %d %v", code, widened)
	}
	if got := widened["granted_capabilities"].([]any); len(got) != 2 {
		t.Fatalf("grant did not accumulate onto the activated set: %v", got)
	}
	// And the widened ceiling is now visible in the prospective check.
	_, after := f.do("req", "POST", "/api/v1/capabilities/check", map[string]any{
		"harness": "simulator", "capabilities": []string{"write:inventory"},
	})
	if got := after["denied"].([]any); len(got) != 0 {
		t.Fatalf("a granted capability is still reported as denied: %v", got)
	}
}
