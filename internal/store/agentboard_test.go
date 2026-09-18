package store

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func setRunStatus(t *testing.T, s *Store, org, runID, status string) {
	t.Helper()
	if e := s.WithOrg(context.Background(), org, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(),
			"update agent_runs set status=$3 where org_id=$1 and id=$2", org, runID, status)
		return e
	}); e != nil {
		t.Fatalf("set run status %s: %v", status, e)
	}
}

// The board exists to tell the truth about what an agent is doing. The most
// important distinction is that a PARKED run is waiting for a person: it is not
// reasoning, it is not burning a worker, and a board that painted it the same
// colour as active work would misreport the agent as busy.
func TestAgentBoardSeparatesParkedFromRunning(t *testing.T) {
	s, org, req, _ := fixture(t)
	ctx := context.Background()
	tk, ag := gateRunSetup(t, s, org, req)

	run, e := s.CreateAgentRun(ctx, org, tk.ID, req, ag.ID, "inventory")
	if e != nil {
		t.Fatal(e)
	}
	board, e := s.AgentBoard(ctx, org, ag.ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(board.Queued) != 1 || board.Queued[0].ID != run.ID {
		t.Fatalf("a new run should be queued, got %+v", board.Queued)
	}
	if board.Active {
		t.Fatal("an agent with only a queued run was reported as active")
	}

	// Actually running: this is the only state that counts as active.
	setRunStatus(t, s, org, run.ID, "running")
	board, e = s.AgentBoard(ctx, org, ag.ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(board.Running) != 1 {
		t.Fatalf("running run missing: %+v", board.Running)
	}
	if !board.Active {
		t.Fatal("an agent with a running attempt was not reported as active")
	}

	// Parked for a human. This must NOT look active.
	setRunStatus(t, s, org, run.ID, "waiting")
	board, e = s.AgentBoard(ctx, org, ag.ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(board.Parked) != 1 {
		t.Fatalf("parked run missing: %+v", board.Parked)
	}
	if len(board.Running) != 0 {
		t.Fatal("a parked run was also reported as running")
	}
	if board.Active {
		t.Fatal("an agent waiting for a human was reported as active — a parked run is not a reasoning process")
	}
}

// One agent's board is that agent's. Sharing it would make "what is this agent
// doing" unanswerable the moment an organisation has two of them.
func TestAgentBoardIsScopedToItsAgent(t *testing.T) {
	s, org, req, _ := fixture(t)
	ctx := context.Background()
	tk, agA := gateRunSetup(t, s, org, req)
	agB, e := s.CreateAgent(ctx, org, "second agent", "simulator", req, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateAgentRun(ctx, org, tk.ID, req, agA.ID, "inventory"); e != nil {
		t.Fatal(e)
	}
	boardB, e := s.AgentBoard(ctx, org, agB.ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(boardB.Queued)+len(boardB.Running)+len(boardB.Parked) != 0 {
		t.Fatalf("another agent's runs leaked onto this board: %+v", boardB)
	}
	boardA, e := s.AgentBoard(ctx, org, agA.ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(boardA.Queued) != 1 {
		t.Fatalf("the owning agent's run is missing: %+v", boardA.Queued)
	}
}

// An agent identity is not a human approver. An agent has no row in `principals`,
// so it can never satisfy the separation-of-duties check that authorises an
// official change — the invariant the whole platform rests on.
func TestAgentIdentityCannotApprove(t *testing.T) {
	s, org, req, _ := fixture(t)
	ctx := context.Background()
	_, prop := proposal(t, s, org, req)
	if e := s.Decide(ctx, org, prop.ID, req, "requester", "endorse", prop.Revision, prop.Digest, ""); e != nil {
		t.Fatalf("endorse: %v", e)
	}
	// No capabilities declared: an agent cannot even be given a capability an
	// operator has not granted (see the capability tests). The point here is that
	// an agent id is not a principal, so it can never satisfy human approval.
	ag, e := s.CreateAgent(ctx, org, "approver-bot", "simulator", req, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e := s.Decide(ctx, org, prop.ID, ag.ID, "approver", "approve", prop.Revision, prop.Digest, ""); e == nil {
		t.Fatal("an agent identity was accepted as a human approver")
	}
	// Declaring the capability is not authority: a capability string cannot
	// create a principal.
	ppl, e := s.ListPeople(ctx, org)
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range ppl {
		if p.ID == ag.ID {
			t.Fatal("creating an agent also created a principal")
		}
	}
}
