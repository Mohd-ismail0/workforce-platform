package api

import "testing"

// Templates: a published, versioned, declarative starting point, with
// customization scoped to what the template offers.
func TestAgentTemplateEndpoints(t *testing.T) {
	f, _, _ := freshOrgFixture(t)

	code, tpl := f.do("req", "POST", "/api/v1/templates", map[string]any{
		"name": "Invoice triage", "version": "1.0.0", "harness": "simulator",
		"description": "Reads supplier mail and prepares a correction.",
		"capabilities": []string{"prepare_proposal", "read:inventory"},
		"instructions":  "Prefer the packing slip when quantities disagree.",
	})
	if code != 201 {
		t.Fatalf("create template: %d %v", code, tpl)
	}
	if tpl["status"] != "draft" {
		t.Fatalf("a new template is %q want draft", tpl["status"])
	}
	// Required fields are a validation failure.
	if c, _ := f.do("req", "POST", "/api/v1/templates", map[string]any{"name": "no version"}); c != 422 {
		t.Fatalf("missing fields: got %d want 422", c)
	}

	// A draft cannot be adopted.
	if c, _ := f.do("req", "POST", "/api/v1/agents/from-template", map[string]any{
		"name": "Early", "template_id": id(tpl),
	}); c == 201 {
		t.Fatal("an agent was created from an unpublished template")
	}

	code, tpl = f.do("req", "POST", "/api/v1/templates/"+id(tpl)+"/publish",
		map[string]any{"expected_version": tpl["revision"]})
	if code != 200 {
		t.Fatalf("publish: %d %v", code, tpl)
	}
	if tpl["status"] != "published" {
		t.Fatalf("status %q want published", tpl["status"])
	}

	// Adopting it inherits the harness, the scope and the instructions.
	code, ag := f.do("req", "POST", "/api/v1/agents/from-template", map[string]any{
		"name": "Triager one", "template_id": id(tpl),
	})
	if code != 201 {
		t.Fatalf("from template: %d %v", code, ag)
	}
	if ag["harness"] != "simulator" {
		t.Fatalf("inherited harness %q", ag["harness"])
	}
	if ag["template_version"] != "1.0.0" {
		t.Fatalf("agent not pinned to the template version: %v", ag["template_version"])
	}
	if ag["instructions"] == "" {
		t.Fatal("the template's instructions were not inherited")
	}
	if caps := ag["capabilities"].([]any); len(caps) != 2 {
		t.Fatalf("inherited capabilities %v", caps)
	}

	// Customization may NARROW the scope...
	code, narrow := f.do("req", "POST", "/api/v1/agents/from-template", map[string]any{
		"name": "Triager narrow", "template_id": id(tpl),
		"capabilities": []string{"read:inventory"},
	})
	if code != 201 {
		t.Fatalf("narrowing: %d %v", code, narrow)
	}
	if caps := narrow["capabilities"].([]any); len(caps) != 1 {
		t.Fatalf("narrowed capabilities %v", caps)
	}
	// ...but never widen it.
	if c, _ := f.do("req", "POST", "/api/v1/agents/from-template", map[string]any{
		"name": "Triager wide", "template_id": id(tpl),
		"capabilities": []string{"write:inventory"},
	}); c == 201 {
		t.Fatal("an agent was created with capabilities outside its template's scope")
	}

	// Unknown template is a 404, not an empty success.
	if c, _ := f.do("req", "POST", "/api/v1/agents/from-template", map[string]any{
		"name": "Ghost", "template_id": "does-not-exist",
	}); c != 404 {
		t.Fatalf("unknown template: got %d want 404", c)
	}

	// Listing is scoped to the organization.
	if code, list := f.do("req", "GET", "/api/v1/templates", nil); code != 200 {
		t.Fatalf("list: %d", code)
	} else if len(list["items"].([]any)) != 1 {
		t.Fatalf("expected the one template, got %v", list["items"])
	}
	if code, other := f.do("other", "GET", "/api/v1/templates", nil); code != 200 {
		t.Fatalf("cross-org list: %d", code)
	} else if len(other["items"].([]any)) != 0 {
		t.Fatalf("cross-org template leak: %v", other["items"])
	}
}
