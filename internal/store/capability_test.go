package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// activeHarnessWithGrants installs an active harness release for the simulator
// runner. Activation is the operator's approval of the release, so it is also
// what makes the release's requested capabilities GRANTED — the ceiling an agent
// on this harness may actually run with.
func activeHarnessWithGrants(t *testing.T, s *Store, org, actor, suffix string, requested []string) RegistryRelease {
	t.Helper()
	ctx := context.Background()
	rel, e := s.CreateRegistryRelease(ctx, org, actor, RegistryCreate{
		Family:  "harness-cap-" + suffix,
		Kind:    "harness",
		Version: "1.0.0-" + suffix,
		Digest:  "sha256:cap-" + suffix,
		// The submitter may REQUEST anything; creation itself grants nothing.
		RequestedCapabilities: requested,
		Manifest:              json.RawMessage(`{"runner_id":"simulator"}`),
		Simulation:            true,
	})
	if e != nil {
		t.Fatalf("create harness release: %v", e)
	}
	if len(rel.GrantedCapabilities) != 0 {
		t.Fatalf("a submission carried grants: %v", rel.GrantedCapabilities)
	}
	for _, step := range []string{"verified", "approved", "installed", "active"} {
		rel, e = s.TransitionRegistryRelease(ctx, org, rel.ID, actor, "admin", step, "test", rel.Revision)
		if e != nil {
			t.Fatalf("transition to %s: %v", step, e)
		}
	}
	if len(rel.GrantedCapabilities) == 0 && len(requested) > 0 {
		t.Fatalf("activation granted nothing for a release that requested %v", requested)
	}
	return rel
}

// Admission is the gate. An agent definition may describe capabilities freely —
// it is metadata — but a RUN is refused when the agent declares something the
// operator never granted its harness, and the refusal names what is missing.
func TestAdmissionRefusesUngrantedCapabilities(t *testing.T) {
	s, org, actor, _ := fixture(t)
	ctx := context.Background()
	activeHarnessWithGrants(t, s, org, actor, "a", []string{"prepare_proposal"})
	task, e := s.CreateTask(ctx, org, "admission task", "", actor, "", "")
	if e != nil {
		t.Fatal(e)
	}

	ok, e := s.CreateAgent(ctx, org, "within ceiling", "simulator", actor, []string{"prepare_proposal"})
	if e != nil {
		t.Fatalf("describing an agent should not require a grant: %v", e)
	}
	if _, e := s.CreateAgentRun(ctx, org, task.ID, actor, ok.ID, "inventory"); e != nil {
		t.Fatalf("a granted capability was refused at admission: %v", e)
	}

	// Declaring more than was granted is allowed as a DESCRIPTION but refused at
	// admission, with a diagnostic naming exactly what is missing.
	over, e := s.CreateAgent(ctx, org, "beyond ceiling", "simulator", actor,
		[]string{"prepare_proposal", "write:inventory"})
	if e != nil {
		t.Fatalf("describing an agent should not require a grant: %v", e)
	}
	_, e = s.CreateAgentRun(ctx, org, task.ID, actor, over.ID, "inventory")
	if e == nil {
		t.Fatal("an agent ran with a capability the operator never granted")
	}
	if !strings.Contains(e.Error(), "write:inventory") {
		t.Fatalf("the refusal does not name what is missing: %v", e)
	}
	// It must not name the capability that WAS granted.
	if strings.Contains(e.Error(), "prepare_proposal") {
		t.Fatalf("the refusal names a granted capability as missing: %v", e)
	}
}

// Increasing what an agent may run with requires a new OPERATOR grant. A
// non-admin cannot widen it, and widening it actually unblocks admission.
func TestGrantIsAdminOnlyAndRaisesTheCeiling(t *testing.T) {
	s, org, actor, _ := fixture(t)
	ctx := context.Background()
	rel := activeHarnessWithGrants(t, s, org, actor, "b", []string{"prepare_proposal"})
	task, e := s.CreateTask(ctx, org, "grant task", "", actor, "", "")
	if e != nil {
		t.Fatal(e)
	}
	ag, e := s.CreateAgent(ctx, org, "writer", "simulator", actor, []string{"write:inventory"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateAgentRun(ctx, org, task.ID, actor, ag.ID, "inventory"); e == nil {
		t.Fatal("a run was admitted before any grant")
	}

	// A non-admin cannot widen the ceiling, even for their own agent.
	if _, e := s.GrantRegistryCapabilities(ctx, org, actor, "requester", rel.ID, []string{"write:inventory"}, rel.Revision); e == nil {
		t.Fatal("a non-admin granted a capability")
	}
	// An admin can. That is the explicit approval a privilege change requires.
	rel2, e := s.GrantRegistryCapabilities(ctx, org, actor, "admin", rel.ID, []string{"write:inventory"}, rel.Revision)
	if e != nil {
		t.Fatalf("admin grant: %v", e)
	}
	if len(rel2.GrantedCapabilities) != 2 {
		t.Fatalf("grant did not accumulate onto the activated set: %v", rel2.GrantedCapabilities)
	}
	if _, e := s.CreateAgentRun(ctx, org, task.ID, actor, ag.ID, "inventory"); e != nil {
		t.Fatalf("admission still refused after the operator granted the capability: %v", e)
	}
}

// Effective configuration must be inspectable: what the agent declares, the
// ceiling its harness permits, anything outside it, and the headroom left.
func TestAgentConfigurationIsInspectable(t *testing.T) {
	s, org, actor, _ := fixture(t)
	ctx := context.Background()
	activeHarnessWithGrants(t, s, org, actor, "c", []string{"read:inventory", "prepare_proposal"})

	ag, e := s.CreateAgent(ctx, org, "reader", "simulator", actor, []string{"read:inventory"})
	if e != nil {
		t.Fatal(e)
	}
	cfg, e := s.AgentConfiguration(ctx, org, ag.ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(cfg.Declared) != 1 || cfg.Declared[0] != "read:inventory" {
		t.Fatalf("declared = %v", cfg.Declared)
	}
	if len(cfg.Permitted) != 2 {
		t.Fatalf("permitted = %v want the two granted capabilities", cfg.Permitted)
	}
	if len(cfg.Denied) != 0 {
		t.Fatalf("an agent declares something outside its ceiling: %v", cfg.Denied)
	}
	if len(cfg.Available) != 1 || cfg.Available[0] != "prepare_proposal" {
		t.Fatalf("available = %v want the ungranted-to-this-agent capability", cfg.Available)
	}

	// A prospective check answers "what would be denied" BEFORE anything is
	// launched, which is what an operator or employee needs to see.
	check, e := s.CheckCapabilities(ctx, org, "simulator", []string{"read:inventory", "approve:work"})
	if e != nil {
		t.Fatal(e)
	}
	if len(check.Denied) != 1 || check.Denied[0] != "approve:work" {
		t.Fatalf("denied = %v want [approve:work]", check.Denied)
	}
	if len(check.Permitted) != 1 || check.Permitted[0] != "read:inventory" {
		t.Fatalf("permitted = %v want [read:inventory]", check.Permitted)
	}
}

// Even a GRANTED approve capability does not make an agent a human approver.
// A capability expresses what may be prepared; authority to authorise an official
// change comes from a principal row, which an agent does not have.
func TestGrantedCapabilityIsStillNotApprovalAuthority(t *testing.T) {
	s, org, actor, _ := fixture(t)
	ctx := context.Background()
	activeHarnessWithGrants(t, s, org, actor, "d", []string{"approve:work"})

	ag, e := s.CreateAgent(ctx, org, "approver-bot", "simulator", actor, []string{"approve:work"})
	if e != nil {
		t.Fatal(e)
	}
	_, prop := proposal(t, s, org, actor)
	if e := s.Decide(ctx, org, prop.ID, actor, "requester", "endorse", prop.Revision, prop.Digest, ""); e != nil {
		t.Fatalf("endorse: %v", e)
	}
	if e := s.Decide(ctx, org, prop.ID, ag.ID, "approver", "approve", prop.Revision, prop.Digest, ""); e == nil {
		t.Fatal("an agent with a GRANTED approve capability was accepted as a human approver")
	}
}
