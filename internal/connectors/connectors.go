// Package connectors contains typed, side-effect-free simulator integrations.
package connectors

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strconv"
)

var (
	ErrStale   = errors.New("stale record")
	ErrInvalid = errors.New("invalid operation")
	ErrUnknown = errors.New("unknown integration or operation")
)

type Operation struct {
	ID              string          `json:"id"`
	Integration     string          `json:"integration"`
	Action          string          `json:"action"`
	BusinessKey     string          `json:"business_key"`
	TargetID        string          `json:"target_id"`
	ExpectedVersion int             `json:"expected_version"`
	Payload         json.RawMessage `json:"payload"`
}
type Record struct {
	ID      string         `json:"id"`
	Version int            `json:"version"`
	Data    map[string]any `json:"data"`
}

// Field describes one accepted payload field, so the operation contract can be
// derived from the connector instead of maintained as prose that drifts.
type Field struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	Note     string `json:"note,omitempty"`
}

type Manifest struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Kind       string   `json:"kind"`
	Actions    []string `json:"actions"`
	Simulation bool     `json:"simulation"`
	// Fields is the payload schema for this integration's operations. Harness
	// instruction text is generated from it.
	Fields []Field `json:"fields,omitempty"`
}
type Connector interface {
	Manifest() Manifest
	Validate(Operation) error
	Prepare(Record, Operation) (Record, error)
}
type Registry struct{ cs map[string]Connector }

func NewRegistry() *Registry {
	return &Registry{map[string]Connector{"inventory": inventoryConnector{}, "mail": mailConnector{}, "documents": documentConnector{}}}
}
func (r *Registry) List() []Manifest {
	out := make([]Manifest, 0, len(r.cs))
	for _, c := range r.cs {
		out = append(out, c.Manifest())
	}
	return out
}
func (r *Registry) Get(id string) (Connector, bool) { c, ok := r.cs[id]; return c, ok }
func check(c Manifest, op Operation) error {
	if op.Integration != c.ID {
		return ErrUnknown
	}
	for _, a := range c.Actions {
		if op.Action == a {
			return nil
		}
	}
	return ErrUnknown
}
func parse(raw json.RawMessage, d any, allowed map[string]bool) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return ErrInvalid
		}
		key, ok := token.(string)
		if !ok || seen[key] || !allowed[key] {
			return ErrInvalid
		}
		seen[key] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return ErrInvalid
		}
	}
	if _, err := decoder.Token(); err != nil {
		return ErrInvalid
	}
	if json.Unmarshal(raw, d) != nil {
		return ErrInvalid
	}
	return nil
}
func prep(cur Record, op Operation, m Manifest, fn func(map[string]any) error) (Record, error) {
	if e := check(m, op); e != nil {
		return Record{}, e
	}
	if op.TargetID == "" || cur.ID != op.TargetID || op.ExpectedVersion != cur.Version {
		return Record{}, ErrStale
	}
	data := clone(cur.Data)
	if e := fn(data); e != nil {
		return Record{}, e
	}
	return Record{ID: cur.ID, Version: cur.Version + 1, Data: data}, nil
}
func clone(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		out[k] = cloneValue(v)
	}
	return out
}
func cloneValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return clone(x)
	case []string:
		return append([]string{}, x...)
	case []any:
		a := make([]any, len(x))
		for i := range x {
			a[i] = cloneValue(x[i])
		}
		return a
	default:
		return x
	}
}
func integer(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int64:
		return x, true
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) || x != math.Trunc(x) || x > 9007199254740991 || x < -9007199254740991 {
			return 0, false
		}
		return int64(x), true
	case json.Number:
		i, e := strconv.ParseInt(string(x), 10, 64)
		return i, e == nil
	default:
		return 0, false
	}
}

type inventoryConnector struct{}

func (inventoryConnector) Manifest() Manifest {
	return Manifest{
		ID: "inventory", Name: "Inventory simulator", Kind: "simulator",
		Actions: []string{"adjust"}, Simulation: true,
		Fields: []Field{{Name: "delta", Type: "integer", Required: true,
			Note: "signed change applied to the record's quantity"}},
	}
}
func (inventoryConnector) Validate(op Operation) error {
	return check(inventoryConnector{}.Manifest(), op)
}
func (inventoryConnector) Prepare(cur Record, op Operation) (Record, error) {
	var p struct {
		Delta any `json:"delta"`
	}
	return prep(cur, op, inventoryConnector{}.Manifest(), func(d map[string]any) error {
		if e := parse(op.Payload, &p, map[string]bool{"delta": true}); e != nil {
			return e
		}
		delta, ok := integer(p.Delta)
		if !ok {
			return ErrInvalid
		}
		q, ok := integer(d["quantity"])
		if !ok || q < 0 || q > 9007199254740991 || delta > 9007199254740991-q || delta < -q {
			return ErrInvalid
		}
		d["quantity"] = q + delta
		return nil
	})
}

type mailConnector struct{}

func (mailConnector) Manifest() Manifest {
	return Manifest{
		ID: "mail", Name: "Mail simulator", Kind: "simulator",
		Actions: []string{"send"}, Simulation: true,
		Fields: []Field{
			{Name: "recipients", Type: "array of string", Required: true, Note: "at least one address"},
			{Name: "subject", Type: "string", Required: true},
			{Name: "body", Type: "string", Required: true},
			{Name: "bcc", Type: "array of string", Required: false, Note: "use [] when empty"},
			{Name: "attachments", Type: "array of string", Required: false, Note: "use [] when empty"},
		},
	}
}
func (mailConnector) Validate(op Operation) error { return check(mailConnector{}.Manifest(), op) }
func (mailConnector) Prepare(cur Record, op Operation) (Record, error) {
	var p struct {
		Recipients  []string `json:"recipients"`
		Subject     string   `json:"subject"`
		Body        string   `json:"body"`
		BCC         []string `json:"bcc"`
		Attachments []string `json:"attachments"`
	}
	return prep(cur, op, mailConnector{}.Manifest(), func(d map[string]any) error {
		if e := parse(op.Payload, &p, map[string]bool{"recipients": true, "subject": true, "body": true, "bcc": true, "attachments": true}); e != nil {
			return e
		}
		if len(p.Recipients) == 0 || p.Subject == "" || p.Body == "" {
			return ErrInvalid
		}
		// An ABSENT optional collection means "empty", not "invalid": a model that
		// omits bcc is saying there is none, and rejecting the whole operation for
		// that was brittle. Unknown fields are still rejected, so nothing is smuggled.
		d["recipients"] = append([]string{}, p.Recipients...)
		d["subject"] = p.Subject
		d["body"] = p.Body
		d["bcc"] = append([]string{}, p.BCC...)
		d["attachments"] = append([]string{}, p.Attachments...)
		return nil
	})
}

type documentConnector struct{}

func (documentConnector) Manifest() Manifest {
	return Manifest{
		ID: "documents", Name: "Documents simulator", Kind: "simulator",
		Actions: []string{"update"}, Simulation: true,
		Fields: []Field{
			{Name: "title", Type: "string", Required: false, Note: "supply title and/or content"},
			{Name: "content", Type: "string", Required: false, Note: "supply title and/or content"},
		},
	}
}
func (documentConnector) Validate(op Operation) error {
	return check(documentConnector{}.Manifest(), op)
}
func (documentConnector) Prepare(cur Record, op Operation) (Record, error) {
	var p struct {
		Title   *string `json:"title"`
		Content *string `json:"content"`
	}
	return prep(cur, op, documentConnector{}.Manifest(), func(d map[string]any) error {
		if e := parse(op.Payload, &p, map[string]bool{"title": true, "content": true}); e != nil {
			return e
		}
		if p.Title == nil && p.Content == nil {
			return ErrInvalid
		}
		if p.Title != nil {
			d["title"] = *p.Title
		}
		if p.Content != nil {
			d["content"] = *p.Content
		}
		return nil
	})
}
