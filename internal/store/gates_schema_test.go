package store

import (
	"encoding/json"
	"errors"
	"testing"
)

// TestGateSchemaAcceptsScalarArrays is the defect the first strict real-model pause
// acceptance exposed: the model asked for `recipients` as an ARRAY of strings — exactly
// what the mail connector requires — and the scalar-only validator rejected every
// answer, so the run parked on a question no human could ever answer (HTTP 422).
func TestGateSchemaAcceptsScalarArrays(t *testing.T) {
	schema := json.RawMessage(`{
		"properties":{
			"recipients":{"type":"array","items":{"type":"string"}},
			"quantity":{"type":"array","items":{"type":"integer"}},
			"subject":{"type":"string"}
		},
		"required":["recipients","subject"]}`)

	ok := json.RawMessage(`{"recipients":["ops@supplier.test","second@supplier.test"],"quantity":[1,2],"subject":"s"}`)
	if e := validateGateResponse(schema, ok); e != nil {
		t.Fatalf("a declared array of strings must be answerable: %v", e)
	}
	// A single-element array is the common real case and must work too.
	if e := validateGateResponse(schema, json.RawMessage(`{"recipients":["ops@supplier.test"],"subject":"s"}`)); e != nil {
		t.Fatalf("a one-element array must be answerable: %v", e)
	}
	// An empty array is a legitimate answer meaning "none".
	if e := validateGateResponse(schema, json.RawMessage(`{"recipients":[],"subject":"s"}`)); e != nil {
		t.Fatalf("an empty declared array must be accepted as empty: %v", e)
	}

	bad := map[string]string{
		"scalar where an array is declared":     `{"recipients":"ops@supplier.test","subject":"s"}`,
		"wrong item type (string in int array)": `{"recipients":["a@b.test"],"quantity":["nope"],"subject":"s"}`,
		"item is an object":                     `{"recipients":[{"email":"a@b.test"}],"subject":"s"}`,
		"nested array":                          `{"recipients":[["a@b.test"]],"subject":"s"}`,
		"integer item with a fraction":          `{"recipients":["a@b.test"],"quantity":[2.5],"subject":"s"}`,
		"integer item beyond exact range":       `{"recipients":["a@b.test"],"quantity":[9007199254740993],"subject":"s"}`,
		"missing required array":                `{"subject":"s"}`,
	}
	for name, payload := range bad {
		if e := validateGateResponse(schema, json.RawMessage(payload)); !errors.Is(e, ErrGateInvalidResponse) {
			t.Fatalf("%s: got %v, want ErrGateInvalidResponse", name, e)
		}
	}
}

// TestGateArraySizeIsBounded: every input entry point is bounded, and a gate answer is
// an entry point.
func TestGateArraySizeIsBounded(t *testing.T) {
	schema := json.RawMessage(`{"properties":{"recipients":{"type":"array","items":{"type":"string"}}}}`)
	big := make([]string, maxGateArrayItems+1)
	for i := range big {
		big[i] = "a@b.test"
	}
	payload, _ := json.Marshal(map[string]any{"recipients": big})
	if e := validateGateResponse(schema, payload); !errors.Is(e, ErrGateInvalidResponse) {
		t.Fatalf("an oversized array must be refused, got %v", e)
	}
	// Exactly at the limit is accepted, so the bound is not off by one.
	at := make([]string, maxGateArrayItems)
	for i := range at {
		at[i] = "a@b.test"
	}
	payload, _ = json.Marshal(map[string]any{"recipients": at})
	if e := validateGateResponse(schema, payload); e != nil {
		t.Fatalf("an array at the limit must be accepted: %v", e)
	}
}

// TestValidateGateSchemaRefusesUnanswerableQuestions is the guard that turns a dead end
// into an upfront refusal. Without it a harness can park a run on a question that every
// answer fails validation against, and nothing ever signals that to a person.
func TestValidateGateSchemaRefusesUnanswerableQuestions(t *testing.T) {
	refuse := map[string]string{
		"array with no item type":      `{"properties":{"a":{"type":"array"}}}`,
		"array of arrays":              `{"properties":{"a":{"type":"array","items":{"type":"array"}}}}`,
		"array of objects":             `{"properties":{"a":{"type":"array","items":{"type":"object"}}}}`,
		"unknown scalar type":          `{"properties":{"a":{"type":"object"}}}`,
		"declared but untyped field":   `{"properties":{"a":{}}}`,
		"required field not declared":  `{"properties":{"a":{"type":"string"}},"required":["b"]}`,
		"properties present but empty": `{"properties":{}}`,
	}
	for name, schema := range refuse {
		if e := ValidateGateSchema(json.RawMessage(schema)); !errors.Is(e, ErrGateInvalidSchema) {
			t.Fatalf("%s: got %v, want ErrGateInvalidSchema", name, e)
		}
	}

	accept := map[string]string{
		"empty schema means unconstrained": `{}`,
		"no schema at all":                 ``,
		"strings":                          `{"properties":{"a":{"type":"string"}}}`,
		"scalar array":                     `{"properties":{"a":{"type":"array","items":{"type":"string"}}}}`,
		"integer array":                    `{"properties":{"a":{"type":"array","items":{"type":"integer"}}}}`,
		"requested alias":                  `{"requested":{"a":{"type":"string"}}}`,
		"typed scalars":                    `{"properties":{"a":{"type":"integer"},"b":{"type":"number"},"c":{"type":"boolean"}}}`,
	}
	for name, schema := range accept {
		if e := ValidateGateSchema(json.RawMessage(schema)); e != nil {
			t.Fatalf("%s: expected acceptance, got %v", name, e)
		}
	}
}

// TestGateSchemaRequiredMustBeDeclared closes a subtle gap: a required name with no
// declared type would be demanded of the human and then rejected on submission.
func TestGateSchemaRequiredMustBeDeclared(t *testing.T) {
	schema := json.RawMessage(`{"properties":{"a":{"type":"string"}},"required":["a","b"]}`)
	if e := ValidateGateSchema(schema); !errors.Is(e, ErrGateInvalidSchema) {
		t.Fatalf("a required field with no declaration must be refused, got %v", e)
	}
	// And the response validator enforces the same rule.
	if e := validateGateResponse(schema, json.RawMessage(`{"a":"x","b":"y"}`)); !errors.Is(e, ErrGateInvalidResponse) {
		t.Fatalf("an undeclared field must not be answerable, got %v", e)
	}
}

// TestScalarOnlySchemaStillWorks guards against the array support widening scalars.
func TestScalarOnlySchemaStillWorks(t *testing.T) {
	schema := json.RawMessage(`{"properties":{"email":{"type":"string"},"qty":{"type":"integer"}},"required":["email","qty"]}`)
	if e := validateGateResponse(schema, json.RawMessage(`{"email":"a@b.test","qty":7}`)); e != nil {
		t.Fatalf("the original scalar shape must keep working: %v", e)
	}
	for name, bad := range map[string]string{
		"array where a string is declared": `{"email":["a@b.test"],"qty":7}`,
		"fractional integer":               `{"email":"a@b.test","qty":2.5}`,
		"undeclared field":                 `{"email":"a@b.test","qty":7,"extra":true}`,
	} {
		if e := validateGateResponse(schema, json.RawMessage(bad)); !errors.Is(e, ErrGateInvalidResponse) {
			t.Fatalf("%s: got %v, want ErrGateInvalidResponse", name, e)
		}
	}
}
