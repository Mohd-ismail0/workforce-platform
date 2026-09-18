package store

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func boardFor(t *testing.T, items []WorkBoardItem, taskID string) (WorkBoardItem, bool) {
	t.Helper()
	for _, it := range items {
		if it.TaskID == taskID {
			return it, true
		}
	}
	return WorkBoardItem{}, false
}

// A person's board must distinguish WHY a piece of work is theirs. "Work I am
// accountable for", "work I am executing for someone else" and "an offer I have
// not answered" are different obligations, and merging them into one list hides
// which of them the person actually carries.
func TestWorkBoardClassifiesByRelationship(t *testing.T) {
	s, org, req, app := fixture(t)
	ctx := context.Background()

	mine, e := s.CreateTask(ctx, org, "I own this", "", req, "", "")
	if e != nil {
		t.Fatal(e)
	}
	// owner=app, assignee=req: I am executing someone else's work.
	theirs, e := s.CreateTask(ctx, org, "I execute this", "", app, req, "")
	if e != nil {
		t.Fatal(e)
	}
	offered, e := s.CreateTask(ctx, org, "offered to me", "", app, "", "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateHandoff(ctx, org, offered.ID, app, req, "owner", "take ownership"); e != nil {
		t.Fatal(e)
	}
	closed, e := s.CreateTask(ctx, org, "finished thing", "", req, "", "")
	if e != nil {
		t.Fatal(e)
	}
	// A terminal task is not outstanding work.
	if e := s.CancelTask(ctx, org, closed.ID, closed.Version); e != nil {
		t.Fatalf("cancel: %v", e)
	}

	items, e := s.WorkBoard(ctx, org, req)
	if e != nil {
		t.Fatal(e)
	}

	if it, ok := boardFor(t, items, mine.ID); !ok || it.Relevance != "accountable" {
		t.Fatalf("task I own: got %+v ok=%v want relevance=accountable", it, ok)
	}
	if it, ok := boardFor(t, items, theirs.ID); !ok || it.Relevance != "executing" {
		t.Fatalf("task I execute: got %+v ok=%v want relevance=executing", it, ok)
	}
	if it, ok := boardFor(t, items, offered.ID); !ok || it.Relevance != "handoff_offered" {
		t.Fatalf("offer to me: got %+v ok=%v want relevance=handoff_offered", it, ok)
	}
	if _, ok := boardFor(t, items, closed.ID); ok {
		t.Fatal("a cancelled task appeared on the board")
	}

	// Someone else's board is a different board.
	other, e := s.WorkBoard(ctx, org, app)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := boardFor(t, other, mine.ID); ok {
		t.Fatal("a task I neither own nor execute appeared on my colleague's board")
	}
	if it, ok := boardFor(t, other, theirs.ID); !ok || it.Relevance != "accountable" {
		t.Fatalf("colleague's own task: got %+v ok=%v want accountable", it, ok)
	}
}

// A board that says "blocked" without saying by what is only half a board. The
// person needs to know WHOSE work is holding theirs up.
func TestWorkBoardNamesTheBlocker(t *testing.T) {
	s, org, req, app := fixture(t)
	ctx := context.Background()
	blocker, e := s.CreateTask(ctx, org, "prerequisite", "", app, "", "")
	if e != nil {
		t.Fatal(e)
	}
	blocked, e := s.CreateTask(ctx, org, "waiting on the prerequisite", "", req, "", "")
	if e != nil {
		t.Fatal(e)
	}
	if e := s.AddDependency(ctx, org, blocked.ID, blocker.ID); e != nil {
		t.Fatalf("dependency: %v", e)
	}

	items, e := s.WorkBoard(ctx, org, req)
	if e != nil {
		t.Fatal(e)
	}
	it, ok := boardFor(t, items, blocked.ID)
	if !ok {
		t.Fatal("blocked task missing from the board")
	}
	if it.BlockerTaskID != blocker.ID {
		t.Fatalf("blocker task = %q want %q", it.BlockerTaskID, blocker.ID)
	}
	if it.BlockerTitle != "prerequisite" {
		t.Fatalf("blocker title = %q", it.BlockerTitle)
	}
	if it.BlockerOwnerID != app {
		t.Fatalf("blocker owner = %q want %q (the person actually holding it up)", it.BlockerOwnerID, app)
	}

	// Once the prerequisite is done, the blocker clears. Completion normally
	// happens through an approved proposal (covered by the API tests); this
	// white-box update keeps the board query under test in isolation.
	if e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "update tasks set status='done', version=version+1 where org_id=$1 and id=$2", org, blocker.ID)
		return e
	}); e != nil {
		t.Fatalf("complete prerequisite: %v", e)
	}
	items, e = s.WorkBoard(ctx, org, req)
	if e != nil {
		t.Fatal(e)
	}
	if it, ok := boardFor(t, items, blocked.ID); ok && it.BlockerTaskID != "" {
		t.Fatalf("blocker still reported after the prerequisite completed: %+v", it)
	}
}

// Taking on an offer must move the work, not duplicate it: the same task cannot
// appear as both an unanswered offer and something I am accountable for.
func TestWorkBoardMovesTaskWhenOfferAccepted(t *testing.T) {
	s, org, req, app := fixture(t)
	ctx := context.Background()
	task, e := s.CreateTask(ctx, org, "transfer me", "", app, "", "")
	if e != nil {
		t.Fatal(e)
	}
	h, e := s.CreateHandoff(ctx, org, task.ID, app, req, "owner", "please take this")
	if e != nil {
		t.Fatal(e)
	}
	items, e := s.WorkBoard(ctx, org, req)
	if e != nil {
		t.Fatal(e)
	}
	if it, ok := boardFor(t, items, task.ID); !ok || it.Relevance != "handoff_offered" {
		t.Fatalf("before accepting: got %+v ok=%v", it, ok)
	}
	if e := s.AcceptHandoff(ctx, org, h.ID, req); e != nil {
		t.Fatalf("accept: %v", e)
	}
	items, e = s.WorkBoard(ctx, org, req)
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	var got WorkBoardItem
	for _, it := range items {
		if it.TaskID == task.ID {
			count++
			got = it
		}
	}
	if count != 1 {
		t.Fatalf("accepted work appears %d times on the board; want exactly once", count)
	}
	if got.Relevance != "accountable" {
		t.Fatalf("after accepting, relevance = %q want accountable", got.Relevance)
	}
}
