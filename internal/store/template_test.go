package store

import (
	"context"
	"testing"
)

func templateFixture(t *testing.T) (*Store, string, string) {
	t.Helper()
	s, org, actor, _ := fixture(t)
	return s, org, actor
}

// A draft template is not a thing anyone should be handed. Publishing is a
// deliberate act, so an unfinished template cannot be adopted by accident.
func TestTemplateMustBePublishedToUse(t *testing.T) {
	s, org, actor := templateFixture(t)
	ctx := context.Background()

	tpl, e := s.CreateAgentTemplate(ctx, org, actor, "Invoice triage", "1.0.0", "simulator",
		"Reads supplier mail and prepares an inventory correction.", []string{"prepare_proposal"}, "")
	if e != nil {
		t.Fatalf("create template: %v", e)
	}
	if tpl.Status != "draft" {
		t.Fatalf("a new template is %q want draft", tpl.Status)
	}
	if _, e := s.CreateAgentFromTemplate(ctx, org, actor, "from draft", tpl.ID, nil, ""); e == nil {
		t.Fatal("an agent was created from an unpublished template")
	}

	if _, e := s.PublishAgentTemplate(ctx, org, actor, tpl.ID, tpl.Revision); e != nil {
		t.Fatalf("publish: %v", e)
	}
	ag, e := s.CreateAgentFromTemplate(ctx, org, actor, "from published", tpl.ID, nil, "")
	if e != nil {
		t.Fatalf("create from published template: %v", e)
	}
	// The agent inherits the template's harness and its scope.
	if ag.Harness != "simulator" {
		t.Fatalf("inherited harness %q", ag.Harness)
	}
	if len(ag.Capabilities) != 1 || ag.Capabilities[0] != "prepare_proposal" {
		t.Fatalf("inherited capabilities %v", ag.Capabilities)
	}
}

// Customization is SCOPED: an employee may narrow what a template offers, never
// widen it. Otherwise "start from a template" would be a way to declare anything.
func TestCustomizationCannotExceedTemplateScope(t *testing.T) {
	s, org, actor := templateFixture(t)
	ctx := context.Background()
	tpl, e := s.CreateAgentTemplate(ctx, org, actor, "Triager", "1.0.0", "simulator",
		"desc", []string{"prepare_proposal", "read:inventory"}, "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.PublishAgentTemplate(ctx, org, actor, tpl.ID, tpl.Revision); e != nil {
		t.Fatal(e)
	}

	// Narrowing is allowed.
	if _, e := s.CreateAgentFromTemplate(ctx, org, actor, "narrower", tpl.ID,
		[]string{"read:inventory"}, ""); e != nil {
		t.Fatalf("narrowing was refused: %v", e)
	}
	// Widening is not.
	if _, e := s.CreateAgentFromTemplate(ctx, org, actor, "wider", tpl.ID,
		[]string{"write:inventory"}, ""); e == nil {
		t.Fatal("an agent declared a capability outside its template's scope")
	}
	// And a mix is refused as a whole rather than silently trimmed.
	if _, e := s.CreateAgentFromTemplate(ctx, org, actor, "mixed", tpl.ID,
		[]string{"read:inventory", "write:inventory"}, ""); e == nil {
		t.Fatal("a partly-out-of-scope customization was accepted")
	}
}

// An agent stays pinned to the template VERSION it came from. Publishing a new
// version must not silently change the configuration of work already in flight.
func TestAgentPinsTheTemplateVersion(t *testing.T) {
	s, org, actor := templateFixture(t)
	ctx := context.Background()
	v1, e := s.CreateAgentTemplate(ctx, org, actor, "Pinned", "1.0.0", "simulator",
		"first", []string{"prepare_proposal"}, "do the first thing")
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.PublishAgentTemplate(ctx, org, actor, v1.ID, v1.Revision); e != nil {
		t.Fatal(e)
	}
	ag, e := s.CreateAgentFromTemplate(ctx, org, actor, "pinned agent", v1.ID, nil, "")
	if e != nil {
		t.Fatal(e)
	}
	if ag.TemplateID != v1.ID || ag.TemplateVersion != "1.0.0" {
		t.Fatalf("agent not pinned to its template version: id=%q version=%q", ag.TemplateID, ag.TemplateVersion)
	}

	// A new version of the same template, with a different scope.
	v2, e := s.CreateAgentTemplate(ctx, org, actor, "Pinned", "2.0.0", "simulator",
		"second", []string{"prepare_proposal", "read:inventory"}, "do the second thing")
	if e != nil {
		t.Fatalf("a new version should be creatable: %v", e)
	}
	if _, e := s.PublishAgentTemplate(ctx, org, actor, v2.ID, v2.Revision); e != nil {
		t.Fatal(e)
	}

	// The existing agent is unchanged: still v1, still its v1 capabilities.
	again, e := s.GetAgent(ctx, org, ag.ID)
	if e != nil {
		t.Fatal(e)
	}
	if again.TemplateVersion != "1.0.0" {
		t.Fatalf("agent version drifted to %q", again.TemplateVersion)
	}
	if len(again.Capabilities) != 1 {
		t.Fatalf("agent capabilities drifted to %v", again.Capabilities)
	}
}

// Templates are validated declarative data. A template naming a harness nothing
// can run is a trap for whoever picks it, so publishing it is refused.
func TestTemplateValidation(t *testing.T) {
	s, org, actor := templateFixture(t)
	ctx := context.Background()

	if _, e := s.CreateAgentTemplate(ctx, org, actor, "", "1.0.0", "simulator", "", nil, ""); e == nil {
		t.Fatal("a template with no name was accepted")
	}
	if _, e := s.CreateAgentTemplate(ctx, org, actor, "Nameless version", "", "simulator", "", nil, ""); e == nil {
		t.Fatal("a template with no version was accepted")
	}
	bad, e := s.CreateAgentTemplate(ctx, org, actor, "Bad harness", "1.0.0", "not-a-real-harness", "", nil, "")
	if e != nil {
		t.Fatalf("creating a draft should not pre-judge the harness: %v", e)
	}
	if _, e := s.PublishAgentTemplate(ctx, org, actor, bad.ID, bad.Revision); e == nil {
		t.Fatal("a template naming an unsupported harness was published")
	}
}
