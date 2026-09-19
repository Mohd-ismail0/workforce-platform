package connectors

import (
	"strings"
	"testing"
)

// Every operation a connector offers must say how far it has been evidenced.
// An action with no certification would otherwise be implicitly trusted, and
// "we never established this" must not read the same as "this is fine".
func TestEveryOfferedActionIsCertified(t *testing.T) {
	for _, m := range NewRegistry().List() {
		declared := map[string]bool{}
		for _, om := range m.Maturity {
			declared[om.Action] = true
			if strings.TrimSpace(om.Evidence) == "" {
				t.Errorf("%s.%s claims maturity %q with no evidence; a level with no stated evidence is a claim, not a certification",
					m.ID, om.Action, om.Maturity)
			}
			switch om.Maturity {
			case MaturityReadOnly, MaturityPreparePreview, MaturityGovernedApply, MaturityVerifiedApply:
			default:
				t.Errorf("%s.%s has unknown maturity %q", m.ID, om.Action, om.Maturity)
			}
		}
		for _, action := range m.Actions {
			got := m.MaturityOf(action)
			if got.Maturity == MaturityUnsupported {
				t.Errorf("%s offers %q but certifies nothing for it; the effective level would be unsupported",
					m.ID, action)
			}
		}
	}
}

// A simulator must never claim to have applied or verified anything against a
// real system. Those levels mean the effect reached an external provider and was
// independently read back; a simulator has done neither, and letting it say so
// would be the most damaging kind of overclaim in this platform.
func TestSimulatorsDoNotClaimRealEffectMaturity(t *testing.T) {
	for _, m := range NewRegistry().List() {
		if !m.Simulation {
			continue
		}
		for _, om := range m.Maturity {
			if om.Maturity == MaturityGovernedApply || om.Maturity == MaturityVerifiedApply {
				t.Errorf("%s is a SIMULATOR but certifies %s as %q; a simulated effect must not be described as applied or verified externally",
					m.ID, om.Action, om.Maturity)
			}
		}
	}
}

// The safe default: an action nobody certified is unsupported, not trusted.
func TestUncertifiedActionIsUnsupported(t *testing.T) {
	m := NewRegistry().List()[0]
	got := m.MaturityOf("something-nobody-certified")
	if got.Maturity != MaturityUnsupported {
		t.Fatalf("an uncertified action reported %q want unsupported", got.Maturity)
	}
	if !strings.Contains(string(got.Maturity), "unsupported") {
		t.Fatalf("unexpected maturity value %q", got.Maturity)
	}
}

// A gap between what is offered and what is certified must be VISIBLE. Silently
// omitting the uncertified action would leave a reader believing the list is
// complete.
func TestUncertifiedOfferedActionsAreStillListed(t *testing.T) {
	m := Manifest{
		ID: "gap", Actions: []string{"read", "write"},
		Maturity: []OperationMaturity{{Action: "read", Maturity: MaturityReadOnly, Evidence: "provider docs + smoke test"}},
	}
	all := m.AllMaturities()
	if len(all) != 2 {
		t.Fatalf("AllMaturities returned %d entries, want both actions: %+v", len(all), all)
	}
	byAction := map[string]Maturity{}
	for _, om := range all {
		byAction[om.Action] = om.Maturity
	}
	if byAction["read"] != MaturityReadOnly {
		t.Errorf("read = %q", byAction["read"])
	}
	if byAction["write"] != MaturityUnsupported {
		t.Errorf("an uncertified offered action reported %q, and must read as unsupported", byAction["write"])
	}
}

// The levels are ordered claims, so a higher level must never be inferred from a
// lower one being present.
func TestMaturityLevelsAreDistinct(t *testing.T) {
	seen := map[Maturity]bool{}
	for _, lv := range []Maturity{MaturityUnsupported, MaturityReadOnly, MaturityPreparePreview, MaturityGovernedApply, MaturityVerifiedApply} {
		if seen[lv] {
			t.Fatalf("duplicate maturity level %q", lv)
		}
		seen[lv] = true
	}
	if MaturityVerifiedApply == MaturityGovernedApply {
		t.Fatal("verified_apply and governed_apply must be distinguishable: one claims readback, the other does not")
	}
}
