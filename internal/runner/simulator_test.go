package runner

import (
	"context"
	"encoding/json"
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
	if !Supported("simulator") {
		t.Fatal("Supported() must include the simulator")
	}
	if Supported("claude-code") {
		t.Fatal("Supported() must not claim a harness this binary cannot run")
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
		if res.Status != StatusSucceeded || res.Draft == nil {
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
		if res.Status != StatusSucceeded || res.Draft == nil {
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
	if res.Status != StatusFailed || res.FailureReason == "" {
		t.Fatalf("want a failure with a reason, got %+v", res)
	}
	if res.Draft != nil {
		t.Fatal("a failed run must not return a draft")
	}
}

// --- The pause path: a runner that cannot proceed must ask, not guess. ---

func TestClarificationIntentWaitsInsteadOfGuessing(t *testing.T) {
	res, err := Default().Run(context.Background(), Request{RunID: "run-w", Intent: "clarify the supplier quantity", Records: scoped()})
	if err != nil {
		t.Fatalf("asking for input is not an error: %v", err)
	}
	if res.Status != StatusWaiting {
		t.Fatalf("status = %q, want %q", res.Status, StatusWaiting)
	}
	if res.Draft != nil {
		t.Fatal("a waiting run must not produce a draft")
	}
	if res.Gate == nil {
		t.Fatal("a waiting run must carry a gate request")
	}
	if res.Gate.Prompt == "" {
		t.Fatal("the gate must state what it needs")
	}
	if res.Gate.Kind != "missing_information" {
		t.Fatalf("gate kind = %q", res.Gate.Kind)
	}
	// The schema is what the human is actually asked for, so it must be non-trivial.
	var schema struct {
		Properties map[string]struct{ Type string } `json:"properties"`
		Required   []string                         `json:"required"`
	}
	if e := json.Unmarshal(res.Gate.InputSchema, &schema); e != nil {
		t.Fatalf("input schema must be valid JSON: %v", e)
	}
	if len(schema.Properties) == 0 || len(schema.Required) == 0 {
		t.Fatalf("input schema must declare properties and required fields: %s", res.Gate.InputSchema)
	}
}

func TestAnsweringTheGateChangesTheOutcome(t *testing.T) {
	base := Request{RunID: "run-a", Intent: "clarify the supplier quantity", Records: scoped()}

	// Still waiting with no answer.
	res, _ := Default().Run(context.Background(), base)
	if res.Status != StatusWaiting {
		t.Fatalf("expected to wait, got %q", res.Status)
	}

	// With both fields answered the run proceeds and the answer is visible in the
	// prepared mail operation — the human's input is consequential, not decorative.
	res, err := Default().Run(context.Background(), Request{
		RunID: "run-a", Intent: base.Intent, Records: scoped(),
		Inputs: []Input{
			{Name: "supplier_email", Value: `"ops@supplier.test"`},
			{Name: "quantity", Value: `42`},
		},
	})
	if err != nil {
		t.Fatalf("run with input: %v", err)
	}
	if res.Status != StatusSucceeded || res.Draft == nil {
		t.Fatalf("status = %q draft = %v", res.Status, res.Draft)
	}
	if len(res.Draft.Operations) != 1 {
		t.Fatalf("expected one operation, got %d", len(res.Draft.Operations))
	}
	op := res.Draft.Operations[0]
	if op.Integration != "mail" || op.Action != "send" {
		t.Fatalf("unexpected operation %+v", op)
	}
	var payload struct {
		Recipients []string `json:"recipients"`
		Body       string   `json:"body"`
	}
	if e := json.Unmarshal(op.Payload, &payload); e != nil {
		t.Fatalf("payload: %v", e)
	}
	if len(payload.Recipients) != 1 || payload.Recipients[0] != "ops@supplier.test" {
		t.Fatalf("recipient must come from the human answer, got %v", payload.Recipients)
	}
	if !strings.Contains(payload.Body, "42") {
		t.Fatalf("the stated quantity must reach the prepared work, got %q", payload.Body)
	}
	if op.BusinessKey == "" {
		t.Fatal("a resumed draft must still carry a business key")
	}
}

func TestPartialAnswerStillWaits(t *testing.T) {
	res, err := Default().Run(context.Background(), Request{
		RunID: "run-p", Intent: "clarify the supplier quantity", Records: scoped(),
		Inputs: []Input{{Name: "supplier_email", Value: `"ops@supplier.test"`}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusWaiting {
		t.Fatalf("a half-answered gate must keep waiting, got %q", res.Status)
	}
}

func TestInvalidAnswerIsRejectedNotInvented(t *testing.T) {
	for _, tc := range []struct{ name, email, qty string }{
		{"blank email", `""`, `5`},
		{"fractional quantity", `"a@b.test"`, `2.5`},
		{"wrong type", `7`, `5`},
	} {
		res, err := Default().Run(context.Background(), Request{
			RunID: "run-x", Intent: "clarify the supplier quantity", Records: scoped(),
			Inputs: []Input{
				{Name: "supplier_email", Value: tc.email},
				{Name: "quantity", Value: tc.qty},
			},
		})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if res.Status != StatusFailed {
			t.Fatalf("%s: a bad answer must fail the run, got %q", tc.name, res.Status)
		}
		if res.Draft != nil {
			t.Fatalf("%s: must not prepare anything from an invalid answer", tc.name)
		}
	}
}

func TestRunnerStopsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Default().Run(ctx, Request{RunID: "run-4", Intent: "inventory", Records: scoped()}); err == nil {
		t.Fatal("a cancelled context must abort the run")
	}
}

func TestInputsAreAddressableByName(t *testing.T) {
	req := Request{Inputs: []Input{{Name: "a", Value: `"x"`}, {Name: "empty", Value: ""}}}
	if v, ok := req.Answer("a"); !ok || string(v) != `"x"` {
		t.Fatalf("Answer(a) = %s, %v", v, ok)
	}
	if _, ok := req.Answer("empty"); ok {
		t.Fatal("an empty answer must not be treated as supplied")
	}
	if _, ok := req.Answer("missing"); ok {
		t.Fatal("an unasked field must not resolve")
	}
}
