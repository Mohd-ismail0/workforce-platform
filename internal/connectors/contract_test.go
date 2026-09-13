package connectors

import (
	"encoding/json"
	"errors"
	"testing"
)

// TestMailOptionalCollectionsOmittedMeansEmpty pins the semantics of an omitted
// optional collection, which had to be defined explicitly.
//
// Before this rule, `bcc: nil` failed validation. A harness that simply does not
// mention bcc is saying "there is none" — not "this operation is malformed" — and a
// real model omitted it, which surfaced the ambiguity. Unknown fields remain rejected,
// so relaxing this cannot be used to smuggle data past validation.
func TestMailOptionalCollectionsOmittedMeansEmpty(t *testing.T) {
	c, ok := NewRegistry().Get("mail")
	if !ok {
		t.Fatal("mail connector missing")
	}
	rec := Record{ID: "m1", Version: 1, Data: map[string]any{}}

	// The required fields only; bcc and attachments are absent entirely.
	op := Operation{
		Integration: "mail", Action: "send", TargetID: "m1", ExpectedVersion: 1,
		Payload: json.RawMessage(`{"recipients":["ops@supplier.test"],"subject":"s","body":"b"}`),
	}
	got, err := c.Prepare(rec, op)
	if err != nil {
		t.Fatalf("an omitted optional collection means empty and must be accepted: %v", err)
	}
	for _, field := range []string{"bcc", "attachments"} {
		v, present := got.Data[field]
		if !present {
			t.Fatalf("%s must be materialised as an empty list, not left absent", field)
		}
		if list, ok := v.([]string); !ok || len(list) != 0 {
			t.Fatalf("%s = %#v, want an empty []string", field, v)
		}
	}
	// Explicit empty lists behave identically to omission.
	explicit := Operation{
		Integration: "mail", Action: "send", TargetID: "m1", ExpectedVersion: 1,
		Payload: json.RawMessage(`{"recipients":["ops@supplier.test"],"subject":"s","body":"b","bcc":[],"attachments":[]}`),
	}
	if _, err := c.Prepare(rec, explicit); err != nil {
		t.Fatalf("explicit empty collections must be accepted: %v", err)
	}
}

// TestMailStillRejectsMissingRequiredAndUnknownFields guards the other side of the
// rule: relaxing omission must not have widened what is acceptable.
func TestMailStillRejectsMissingRequiredAndUnknownFields(t *testing.T) {
	c, _ := NewRegistry().Get("mail")
	rec := Record{ID: "m1", Version: 1, Data: map[string]any{}}

	cases := map[string]string{
		"missing recipients":                   `{"subject":"s","body":"b"}`,
		"empty recipients":                     `{"recipients":[],"subject":"s","body":"b"}`,
		"missing subject":                      `{"recipients":["a@b.test"],"body":"b"}`,
		"missing body":                         `{"recipients":["a@b.test"],"subject":"s"}`,
		"unknown field":                        `{"recipients":["a@b.test"],"subject":"s","body":"b","sneaky":"x"}`,
		"duplicate key":                        `{"recipients":["a@b.test"],"recipients":["c@d.test"],"subject":"s","body":"b"}`,
		"wrong type for a required collection": `{"recipients":"a@b.test","subject":"s","body":"b"}`,
	}
	for name, payload := range cases {
		op := Operation{
			Integration: "mail", Action: "send", TargetID: "m1", ExpectedVersion: 1,
			Payload: json.RawMessage(payload),
		}
		if _, err := c.Prepare(rec, op); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: got %v, want ErrInvalid", name, err)
		}
	}
}

// TestInventoryStillRejectsMissingDelta keeps the same guarantee for the other
// integration: only mail's optional collections were relaxed.
func TestInventoryStillRejectsMissingDelta(t *testing.T) {
	c, _ := NewRegistry().Get("inventory")
	rec := Record{ID: "i1", Version: 1, Data: map[string]any{"quantity": 10.0}}
	op := Operation{
		Integration: "inventory", Action: "adjust", TargetID: "i1", ExpectedVersion: 1,
		Payload: json.RawMessage(`{}`),
	}
	if _, err := c.Prepare(rec, op); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a missing required delta must be rejected, got %v", err)
	}
}

// TestEveryConnectorPublishesItsSchema is the anti-drift guarantee. The harness
// contract is GENERATED from these manifests, so a connector that declines to
// publish its payload schema would silently produce an unactionable contract for
// every model — exactly the defect the first real model-backed run exposed.
func TestEveryConnectorPublishesItsSchema(t *testing.T) {
	for _, m := range NewRegistry().List() {
		if len(m.Actions) == 0 {
			t.Fatalf("connector %q publishes no actions", m.ID)
		}
		if len(m.Fields) == 0 {
			t.Fatalf("connector %q publishes no payload schema; the generated harness "+
				"contract would name no fields for it", m.ID)
		}
		required := 0
		for _, f := range m.Fields {
			if f.Name == "" || f.Type == "" {
				t.Fatalf("connector %q has an incomplete field: %+v", m.ID, f)
			}
			if f.Required {
				required++
			}
		}
		// documents legitimately accepts either title or content, so it may declare
		// no single required field; the others must have at least one.
		if m.ID != "documents" && required == 0 {
			t.Fatalf("connector %q declares no required field; validation would accept an empty payload", m.ID)
		}
	}
}
