package runner

import (
	"context"
	"strings"
	"testing"
)

func rec(id, integration string, version int) RecordRef {
	return RecordRef{ID: id, Integration: integration, Version: version, Data: map[string]any{"quantity": 10.0}}
}

func scoped() []RecordRef {
	return []RecordRef{rec("inv-1", "inventory", 3), rec("doc-1", "documents", 7), rec("mail-1", "mail", 2)}
}

func TestManifestDeclaresSimulationHonestly(t *testing.T) {
	m := Default().Manifest()
	if !m.Simulation {
		t.Fatal("simulator must declare itself as a simulation")
	}
	if m.Kind != "harness" {
		t.Fatalf("kind = %q, want harness", m.Kind)
	}
	if m.MaxOperations < 1 {
		t.Fatalf("MaxOperations = %d, want >= 1", m.MaxOperations)
	}
	if len(Available()) == 0 {
		t.Fatal("Available() must advertise the simulator")
	}
}

func TestUnknownRunnerIsRejected(t *testing.T) {
	if _, err := New("claude-code"); err == nil {
		t.Fatal("New() must reject a harness it cannot actually run")
	}
	if _, err := New("simulator"); err != nil {
		t.Fatalf("New(simulator) failed: %v", err)
	}
}

func TestInventoryIntentTargetsScopedRecordAndCarriesBusinessKey(t *testing.T) {
	for _, intent := range []string{"inventory", "INVENTORY check", ""} {
		res, err := Default().Run(context.Background(), Request{RunID: "run-1", Intent: intent, Records: scoped()})
		if err != nil {
			t.Fatalf("intent %q: %v", intent, err)
		}
		if res.Status != "succeeded" || res.Draft == nil {
			t.Fatalf("intent %q: status=%q draft=%v", intent, res.Status, res.Draft)
		}
		if len(res.Draft.Operations) > Default().Manifest().MaxOperations {
			t.Fatalf("intent %q: exceeded MaxOperations", intent)
		}
		op := res.Draft.Operations[0]
		if op.Integration != "inventory" || op.Action != "adjust" || op.TargetID != "inv-1" {
			t.Fatalf("intent %q: unexpected operation %+v", intent, op)
		}
		if op.ExpectedVersion != 3 {
			t.Fatalf("intent %q: expected_version = %d, want the scoped record version 3", intent, op.ExpectedVersion)
		}
		if op.BusinessKey != "run:run-1:inv-1" {
			t.Fatalf("intent %q: business_key = %q", intent, op.BusinessKey)
		}
	}
}

func TestDocumentAndMailIntentsAlsoCarryBusinessKey(t *testing.T) {
	cases := []struct{ intent, integration, target, action string }{
		{"update the document", "documents", "doc-1", "update"},
		{"send mail to supplier", "mail", "mail-1", "send"},
	}
	for _, c := range cases {
		res, err := Default().Run(context.Background(), Request{RunID: "run-9", Intent: c.intent, Records: scoped()})
		if err != nil {
			t.Fatalf("%s: %v", c.intent, err)
		}
		if res.Status != "succeeded" || res.Draft == nil {
			t.Fatalf("%s: status=%q", c.intent, res.Status)
		}
		op := res.Draft.Operations[0]
		if op.Integration != c.integration || op.TargetID != c.target || op.Action != c.action {
			t.Fatalf("%s: got %+v", c.intent, op)
		}
		if strings.TrimSpace(op.BusinessKey) == "" {
			t.Fatalf("%s: draft must carry a business key or proposal creation will be rejected", c.intent)
		}
	}
}

func TestNoSuitableRecordFailsWithoutError(t *testing.T) {
	res, err := Default().Run(context.Background(), Request{RunID: "run-3", Intent: "inventory", Records: []RecordRef{rec("doc-1", "documents", 1)}})
	if err != nil {
		t.Fatalf("a missing record is a business failure, not an error: %v", err)
	}
	if res.Status != "failed" || res.FailureReason == "" {
		t.Fatalf("want a failure with a reason, got %+v", res)
	}
	if res.Draft != nil {
		t.Fatal("a failed run must not return a draft")
	}
}

func TestRunnerStopsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Default().Run(ctx, Request{RunID: "run-4", Intent: "inventory", Records: scoped()}); err == nil {
		t.Fatal("a cancelled context must abort the run")
	}
}
