package connectors

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestRejectDuplicatePayloadFields(t *testing.T) {
	c, _ := NewRegistry().Get("inventory")
	_, err := c.Prepare(Record{ID: "x", Version: 1, Data: map[string]any{"quantity": 10}}, Operation{Integration: "inventory", Action: "adjust", TargetID: "x", ExpectedVersion: 1, Payload: json.RawMessage(`{"delta":-9,"delta":1}`)})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate field accepted: %v", err)
	}
}
func TestRejectUnsafeJSONInteger(t *testing.T) {
	c, _ := NewRegistry().Get("inventory")
	_, err := c.Prepare(Record{ID: "x", Version: 1, Data: map[string]any{"quantity": 10}}, Operation{Integration: "inventory", Action: "adjust", TargetID: "x", ExpectedVersion: 1, Payload: json.RawMessage(`{"delta":9007199254740993}`)})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsafe integer accepted: %v", err)
	}
}
func TestCloneStringSlices(t *testing.T) {
	c, _ := NewRegistry().Get("documents")
	cur := Record{ID: "x", Version: 1, Data: map[string]any{"title": "old", "tags": []string{"private"}}}
	got, err := c.Prepare(cur, Operation{Integration: "documents", Action: "update", TargetID: "x", ExpectedVersion: 1, Payload: json.RawMessage(`{"title":"new"}`)})
	if err != nil {
		t.Fatal(err)
	}
	got.Data["tags"].([]string)[0] = "changed"
	if cur.Data["tags"].([]string)[0] != "private" {
		t.Fatal("input slice mutated")
	}
}
