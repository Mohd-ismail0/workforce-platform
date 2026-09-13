package connectors

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

func integerValue(v any) int64 {
	i, ok := integer(v)
	if !ok {
		return -1
	}
	return i
}

func TestRegistryAndInventoryPrepare(t *testing.T) {
	r := NewRegistry()
	for _, id := range []string{"inventory", "mail", "documents"} {
		if _, ok := r.Get(id); !ok {
			t.Fatalf("missing %s", id)
		}
	}
	c, _ := r.Get("inventory")
	cur := Record{ID: "sku-1", Version: 2, Data: map[string]any{"quantity": 10, "name": "widget"}}
	payload := json.RawMessage(`{"delta":-3}`)
	got, err := c.Prepare(cur, Operation{ID: "op", Integration: "inventory", Action: "adjust", TargetID: "sku-1", ExpectedVersion: 2, Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 3 || integerValue(got.Data["quantity"]) != 7 {
		t.Fatalf("got %#v", got)
	}
	if integerValue(cur.Data["quantity"]) != 10 {
		t.Fatal("input mutated")
	}
	got.Data["name"] = "changed"
	if cur.Data["name"] != "widget" {
		t.Fatal("deep copy leaked")
	}
}
func TestValidationFailures(t *testing.T) {
	c, _ := NewRegistry().Get("inventory")
	cur := Record{ID: "x", Version: 1, Data: map[string]any{"quantity": 1}}
	cases := []struct {
		name string
		op   Operation
		want error
	}{
		{"stale", Operation{Integration: "inventory", Action: "adjust", TargetID: "x", ExpectedVersion: 0, Payload: json.RawMessage(`{"delta":1}`)}, ErrStale},
		{"malformed", Operation{Integration: "inventory", Action: "adjust", TargetID: "x", ExpectedVersion: 1, Payload: json.RawMessage(`{"delta":1,"hidden":2}`)}, ErrInvalid},
		{"negative", Operation{Integration: "inventory", Action: "adjust", TargetID: "x", ExpectedVersion: 1, Payload: json.RawMessage(`{"delta":-2}`)}, ErrInvalid},
		{"action", Operation{Integration: "inventory", Action: "send", TargetID: "x", ExpectedVersion: 1, Payload: json.RawMessage(`{}`)}, ErrUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, e := c.Prepare(cur, tc.op)
			if !errors.Is(e, tc.want) {
				t.Fatalf("err %v want %v", e, tc.want)
			}
		})
	}
}
func TestTypedAdaptersAndJSONNumbers(t *testing.T) {
	r := NewRegistry()
	mail, _ := r.Get("mail")
	doc, _ := r.Get("documents")
	cur := Record{ID: "m", Version: 1, Data: map[string]any{}}
	m, err := mail.Prepare(cur, Operation{Integration: "mail", Action: "send", TargetID: "m", ExpectedVersion: 1, Payload: json.RawMessage(`{"recipients":["a@example.com"],"subject":"Hi","body":"Body","bcc":[],"attachments":[]}`)})
	if err != nil || m.Version != 2 {
		t.Fatal(err)
	}
	d, err := doc.Prepare(Record{ID: "d", Version: 4, Data: map[string]any{"title": "old", "content": "text"}}, Operation{Integration: "documents", Action: "update", TargetID: "d", ExpectedVersion: 4, Payload: json.RawMessage(`{"title":"new","content":"updated"}`)})
	if err != nil || d.Data["title"] != "new" {
		t.Fatal(err)
	}
	var decoded map[string]any
	_ = json.Unmarshal([]byte(`{"quantity":3}`), &decoded)
	inv, _ := r.Get("inventory")
	if _, err = inv.Prepare(Record{ID: "i", Version: 1, Data: decoded}, Operation{Integration: "inventory", Action: "adjust", TargetID: "i", ExpectedVersion: 1, Payload: json.RawMessage(`{"delta":1}`)}); err != nil {
		t.Fatal(err)
	}
	_ = math.MaxInt
}
