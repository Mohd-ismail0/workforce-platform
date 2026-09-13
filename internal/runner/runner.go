package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"workforce.local/platform/internal/connectors"
)

// Manifest describes what a runner can actually do. Simulation must be true for any
// runner that does not execute a real external harness.
type Manifest struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`
	Version       string   `json:"version"`
	Simulation    bool     `json:"simulation"`
	Capabilities  []string `json:"capabilities"`
	MaxOperations int      `json:"max_operations"`
	// Command is the configured binary NAME only, for display/diagnostics.
	// Full arguments are never exposed so a task cannot learn or alter them.
	Command string `json:"command,omitempty"`
}

// Input is a single human answer already collected for this run, keyed by the field
// name the runner asked for. Value holds the raw JSON of that answer.
type Input struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Request is the only thing a runner ever receives. Records are read-only, scoped to
// a single organisation, and are the runner's entire view of the outside world.
type Request struct {
	OrgID     string      `json:"org_id"`
	TaskID    string      `json:"task_id"`
	RunID     string      `json:"run_id"`
	AgentID   string      `json:"agent_id"`
	AgentName string      `json:"agent_name"`
	Harness   string      `json:"harness"`
	Intent    string      `json:"intent"`
	Records   []RecordRef `json:"records"`
	Inputs    []Input     `json:"inputs"`
}

// Answer returns the JSON value supplied for a field name, if the human provided it.
func (r Request) Answer(name string) (json.RawMessage, bool) {
	for _, in := range r.Inputs {
		if in.Name == name && in.Value != "" {
			return json.RawMessage(in.Value), true
		}
	}
	return nil, false
}

type RecordRef struct {
	ID          string         `json:"id"`
	Integration string         `json:"integration"`
	Version     int            `json:"version"`
	Data        map[string]any `json:"data"`
}

type ProposalDraft struct {
	Summary    string                 `json:"summary"`
	Operations []connectors.Operation `json:"operations"`
}

// GateRequest is a durable request for human input. A runner that cannot proceed
// returns this instead of a draft; the store persists it and parks the run.
type GateRequest struct {
	Kind        string          `json:"kind"`
	Prompt      string          `json:"prompt"`
	InputSchema json.RawMessage `json:"input_schema"`
	// RespondentID may be left empty, in which case the accountable task owner is
	// asked. A runner never chooses to bypass the owner without naming someone.
	RespondentID string `json:"respondent_id,omitempty"`
}

type Result struct {
	Status        string         `json:"status"`
	Summary       string         `json:"summary"`
	FailureReason string         `json:"failure_reason"`
	Notes         []string       `json:"notes"`
	Draft         *ProposalDraft `json:"draft"`
	Gate          *GateRequest   `json:"gate"`
}

// Result statuses. A runner reports exactly one of these.
const (
	// StatusSucceeded means a draft proposal is attached.
	StatusSucceeded = "succeeded"
	// StatusWaiting means the runner needs human input and has attached a Gate.
	StatusWaiting = "waiting"
	// StatusFailed means the runner could not do its job. FailureReason is a public code.
	StatusFailed = "failed"
)

type Runner interface {
	Manifest() Manifest
	Run(context.Context, Request) (Result, error)
}

func New(id string) (Runner, error) {
	if id == "simulator" {
		return simulatorRunner{}, nil
	}
	for _, c := range cliConfigs() {
		if c.id == id {
			return c, nil
		}
	}
	return nil, fmt.Errorf("unknown runner %q", id)
}

// RunBudget reports the longest wall-clock time the named runner may take for a
// single attempt. The store derives the claim lease from this, so a slow but
// healthy harness can never outlive its own lease and be reclaimed mid-run.
func RunBudget(id string) time.Duration {
	for _, c := range cliConfigs() {
		if c.id == id {
			return c.timeout
		}
	}
	return 30 * time.Second
}

// Available lists the runners this binary can actually execute. Registration of a
// harness release does not add an entry here.
func Available() []Manifest {
	out := []Manifest{simulatorRunner{}.Manifest()}
	for _, c := range cliConfigs() {
		out = append(out, c.Manifest())
	}
	return out
}

func Default() Runner { return simulatorRunner{} }

// Supported reports whether this binary can execute the named runner.
func Supported(id string) bool {
	for _, m := range Available() {
		if m.ID == id {
			return true
		}
	}
	return false
}
