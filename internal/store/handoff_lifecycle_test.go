package store

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// extraPrincipal inserts one more active principal into the fixture org and
// returns its id, so hop chains can be exercised beyond the two built-ins.
func extraPrincipal(t *testing.T, s *Store, org, role string) string {
	t.Helper()
	id := newIDForTest()
	if e := s.WithOrg(context.Background(), org, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(),
			"INSERT INTO principals(id,org_id,name,role) VALUES($1,$2,'extra',$3)", id, org, role)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	return id
}

func newIDForTest() string { return "p-" + time.Now().Format("150405.000000000") }

// A handoff has a lifecycle, not just an accept. The recipient must be able to
// ask a question and to decline; the creator must be able to withdraw it; and
// neither action may be taken by the wrong party.
func TestHandoffDeclineAndClarify(t *testing.T) {
	s, org, req, app := fixture(t)
	ctx := context.Background()
	task, e := s.CreateTask(ctx, org, "lifecycle task", "", req, "", "")
	if e != nil {
		t.Fatal(e)
	}
	h, e := s.CreateHandoff(ctx, org, task.ID, req, app, "assignee", "please take this")
	if e != nil {
		t.Fatal(e)
	}
	if h.State != "offered" {
		t.Fatalf("new handoff state %q want offered", h.State)
	}

	// The CREATOR cannot decline their own offer; only the recipient may.
	if e := s.DeclineHandoff(ctx, org, h.ID, req, "not mine to decline"); e == nil {
		t.Fatal("creator was allowed to decline their own handoff")
	}
	// A stranger cannot decline it either.
	stranger := extraPrincipal(t, s, org, "requester")
	if e := s.DeclineHandoff(ctx, org, h.ID, stranger, "not addressed to me"); e == nil {
		t.Fatal("a non-recipient was allowed to decline")
	}

	// The recipient may ask for clarification; the handoff stays open.
	if e := s.ClarifyHandoff(ctx, org, h.ID, app, "which supplier is this for?"); e != nil {
		t.Fatalf("clarify: %v", e)
	}
	got, e := s.PeekHandoff(ctx, org, h.ID, app)
	if e != nil {
		t.Fatal(e)
	}
	if got.State != "clarification_requested" {
		t.Fatalf("after clarify state %q want clarification_requested", got.State)
	}
	if got.Reason != "which supplier is this for?" {
		t.Fatalf("clarification reason not recorded: %q", got.Reason)
	}

	// And the recipient may still decline from the clarification state.
	if e := s.DeclineHandoff(ctx, org, h.ID, app, "not enough context"); e != nil {
		t.Fatalf("decline after clarify: %v", e)
	}
	got, e = s.PeekHandoff(ctx, org, h.ID, app)
	if e != nil {
		t.Fatal(e)
	}
	if got.State != "declined" {
		t.Fatalf("state %q want declined", got.State)
	}
	if !strings.Contains(got.Reason, "not enough context") {
		t.Fatalf("decline reason not recorded: %q", got.Reason)
	}
	// A declined handoff is terminal: it cannot then be accepted.
	if e := s.AcceptHandoff(ctx, org, h.ID, app); e == nil {
		t.Fatal("a declined handoff was accepted")
	}
}

// The creator withdraws their own offer; nobody else may.
func TestHandoffCancelByCreatorOnly(t *testing.T) {
	s, org, req, app := fixture(t)
	ctx := context.Background()
	task, e := s.CreateTask(ctx, org, "cancel task", "", req, "", "")
	if e != nil {
		t.Fatal(e)
	}
	h, e := s.CreateHandoff(ctx, org, task.ID, req, app, "assignee", "take this")
	if e != nil {
		t.Fatal(e)
	}
	if e := s.CancelHandoff(ctx, org, h.ID, app, "recipient tries to cancel"); e == nil {
		t.Fatal("recipient was allowed to cancel the creator's offer")
	}
	if e := s.CancelHandoff(ctx, org, h.ID, req, "no longer needed"); e != nil {
		t.Fatalf("creator cancel: %v", e)
	}
	got, e := s.PeekHandoff(ctx, org, h.ID, req)
	if e != nil {
		t.Fatal(e)
	}
	if got.State != "cancelled" {
		t.Fatalf("state %q want cancelled", got.State)
	}
}

// A handoff that is not answered must expire on its own, and an expired handoff
// must not be acceptable. This is the durable-wait promise: work does not sit
// forever waiting for a person who never replies.
func TestHandoffExpires(t *testing.T) {
	s, org, req, app := fixture(t)
	ctx := context.Background()
	task, e := s.CreateTask(ctx, org, "expiry task", "", req, "", "")
	if e != nil {
		t.Fatal(e)
	}
	// Create with an expiry already in the past.
	h, e := s.createHandoff(ctx, org, task.ID, req, app, "assignee", "too late", time.Now().Add(-time.Minute))
	if e != nil {
		t.Fatal(e)
	}
	// Reading it must surface the expiry rather than showing a stale offer.
	got, e := s.PeekHandoff(ctx, org, h.ID, app)
	if e != nil {
		t.Fatal(e)
	}
	if got.State != "expired" {
		t.Fatalf("state %q want expired", got.State)
	}
	// And an expired handoff cannot be accepted.
	if e := s.AcceptHandoff(ctx, org, h.ID, app); e == nil {
		t.Fatal("an expired handoff was accepted")
	}
}

// Endless ping-pong of accountability is not collaboration; it is a way to
// launder responsibility. Hop chains are bounded.
func TestHandoffHopLimit(t *testing.T) {
	s, org, req, app := fixture(t)
	ctx := context.Background()
	task, e := s.CreateTask(ctx, org, "hop task", "", req, "", "")
	if e != nil {
		t.Fatal(e)
	}
	// Walk ownership around a ring of principals, accepting each hop.
	ring := []string{app, extraPrincipal(t, s, org, "approver"), extraPrincipal(t, s, org, "approver")}
	current := req
	for i := 0; i < maxHandoffHops; i++ {
		next := ring[i%len(ring)]
		h, e := s.CreateHandoff(ctx, org, task.ID, current, next, "owner", "hop")
		if e != nil {
			t.Fatalf("hop %d create: %v", i, e)
		}
		if e := s.AcceptHandoff(ctx, org, h.ID, next); e != nil {
			t.Fatalf("hop %d accept: %v", i, e)
		}
		current = next
	}
	// One more hop exceeds the limit and must be refused, not silently allowed.
	if _, e := s.CreateHandoff(ctx, org, task.ID, current, req, "owner", "one hop too many"); e == nil {
		t.Fatal("hop limit was not enforced")
	}
}

// Two replies to the same offer race. Exactly one may win: a handoff cannot be
// simultaneously accepted and declined, and the loser must be told it lost.
func TestHandoffConcurrentAcceptAndDeclineOnlyOneWins(t *testing.T) {
	s, org, req, app := fixture(t)
	ctx := context.Background()
	task, e := s.CreateTask(ctx, org, "race task", "", req, "", "")
	if e != nil {
		t.Fatal(e)
	}
	h, e := s.CreateHandoff(ctx, org, task.ID, req, app, "assignee", "race")
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make([]error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); results[0] = s.AcceptHandoff(ctx, org, h.ID, app) }()
	go func() { defer wg.Done(); results[1] = s.DeclineHandoff(ctx, org, h.ID, app, "no") }()
	wg.Wait()
	wins := 0
	for _, r := range results {
		if r == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("exactly one reply must win, got %d (accept=%v decline=%v)", wins, results[0], results[1])
	}
	got, e := s.PeekHandoff(ctx, org, h.ID, app)
	if e != nil {
		t.Fatal(e)
	}
	if got.State != "accepted" && got.State != "declined" {
		t.Fatalf("final state %q must be one of accepted/declined", got.State)
	}
}
