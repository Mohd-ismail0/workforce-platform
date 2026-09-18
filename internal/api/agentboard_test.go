package api

import "testing"

// The agent board's contract: it describes one agent, is scoped to that agent,
// and reports whether the agent is actually working. The state-machine
// separation (queued/running/parked) is asserted deterministically in the store
// test, where a run's status can be set directly rather than raced against a
// real worker.
func TestAgentBoardEndpoint(t *testing.T) {
	f, _, _ := freshOrgFixture(t)

	code, ag := f.do("req", "POST", "/api/v1/agents", map[string]any{
		"name": "Board Agent", "harness": "simulator",
	})
	if code != 201 {
		t.Fatalf("create agent: %d %v", code, ag)
	}

	// An agent with no attempts is not active, and says so.
	code, board := f.do("req", "GET", "/api/v1/agents/"+id(ag)+"/board", nil)
	if code != 200 {
		t.Fatalf("board: %d %v", code, board)
	}
	def, ok := board["agent"].(map[string]any)
	if !ok {
		t.Fatalf("board did not include the agent definition: %v", board)
	}
	if def["name"] != "Board Agent" || def["harness"] != "simulator" {
		t.Fatalf("agent definition wrong: %v", def)
	}
	if board["active"] != false {
		t.Fatalf("an agent with no attempts reported active: %v", board["active"])
	}
	for _, k := range []string{"running", "queued", "parked", "tasks", "open_gates"} {
		if _, ok := board[k].([]any); !ok {
			t.Fatalf("board is missing the %q collection: %v", k, board)
		}
	}

	// A second agent's board is empty even though the first exists.
	_, ag2 := f.do("req", "POST", "/api/v1/agents", map[string]any{
		"name": "Other Agent", "harness": "simulator",
	})
	_, board2 := f.do("req", "GET", "/api/v1/agents/"+id(ag2)+"/board", nil)
	if n := len(board2["running"].([]any)) + len(board2["queued"].([]any)) + len(board2["parked"].([]any)); n != 0 {
		t.Fatalf("another agent's attempts leaked onto this board: %v", board2)
	}

	// An unknown agent is refused with 404 rather than answered with an empty
	// board, because an empty board is a claim about an agent that does not
	// exist - and 409 would tell a client its request conflicted rather than
	// that the agent is missing.
	if code, _ := f.do("req", "GET", "/api/v1/agents/does-not-exist/board", nil); code != 404 {
		t.Fatalf("unknown agent: got %d want 404", code)
	}

	// Another organization's agent is not visible from here.
	_, foreign := f.do("other", "POST", "/api/v1/agents", map[string]any{
		"name": "Foreign Agent", "harness": "simulator",
	})
	if code, _ := f.do("req", "GET", "/api/v1/agents/"+id(foreign)+"/board", nil); code != 404 {
		t.Fatalf("cross-organization agent board: got %d want 404", code)
	}
}
