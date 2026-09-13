package runner

import (
	"context"
	"fmt"
	"workforce.local/platform/internal/connectors"
)

type Manifest struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`
	Version       string   `json:"version"`
	Simulation    bool     `json:"simulation"`
	Capabilities  []string `json:"capabilities"`
	MaxOperations int      `json:"max_operations"`
}
type Request struct {
	OrgID     string      `json:"org_id"`
	TaskID    string      `json:"task_id"`
	RunID     string      `json:"run_id"`
	AgentID   string      `json:"agent_id"`
	AgentName string      `json:"agent_name"`
	Harness   string      `json:"harness"`
	Intent    string      `json:"intent"`
	Records   []RecordRef `json:"records"`
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
type Result struct {
	Status        string         `json:"status"`
	Summary       string         `json:"summary"`
	FailureReason string         `json:"failure_reason"`
	Notes         []string       `json:"notes"`
	Draft         *ProposalDraft `json:"draft"`
}
type Runner interface {
	Manifest() Manifest
	Run(context.Context, Request) (Result, error)
}

func New(id string) (Runner, error) {
	if id == "simulator" {
		return simulatorRunner{}, nil
	}
	return nil, fmt.Errorf("unknown runner %q", id)
}
func Available() []Manifest { return []Manifest{simulatorRunner{}.Manifest()} }
func Default() Runner       { return simulatorRunner{} }
