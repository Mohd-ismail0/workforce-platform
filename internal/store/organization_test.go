package store

import (
	"context"
	"testing"
	"time"

	"workforce.local/platform/internal/platform"
)

// A reporting line is a fact with a validity period, not a mutable column. The
// same relationship must therefore read differently depending on when you ask.
func TestReportingRelationshipIsEffectiveDated(t *testing.T) {
	s, org, req, app := fixture(t)
	ctx := context.Background()
	boss := extraPrincipal(t, s, org, "approver")

	start := time.Now().Add(-2 * time.Hour)
	end := time.Now().Add(-1 * time.Hour)
	if _, e := s.CreateRelationship(ctx, org, req, app, "reports_to", boss, start, &end); e != nil {
		t.Fatalf("create: %v", e)
	}

	// Before it began: nothing.
	if rows, e := s.RelationshipsAsOf(ctx, org, start.Add(-time.Minute)); e != nil {
		t.Fatal(e)
	} else if len(rows) != 0 {
		t.Fatalf("relationship visible before it started: %+v", rows)
	}
	// Inside the interval: present.
	if rows, e := s.RelationshipsAsOf(ctx, org, start.Add(30*time.Minute)); e != nil {
		t.Fatal(e)
	} else if len(rows) != 1 {
		t.Fatalf("relationship missing when it was in effect: %+v", rows)
	}
	// At the exact end: absent. Half-open [start, end) means the end instant
	// belongs to whatever came next, not to this period.
	if rows, e := s.RelationshipsAsOf(ctx, org, end); e != nil {
		t.Fatal(e)
	} else if len(rows) != 0 {
		t.Fatalf("half-open interval treated the end instant as inside: %+v", rows)
	}
	// At the exact start: present.
	if rows, e := s.RelationshipsAsOf(ctx, org, start); e != nil {
		t.Fatal(e)
	} else if len(rows) != 1 {
		t.Fatalf("half-open interval excluded its own start: %+v", rows)
	}
}

// Two managers at once is a contradiction, not a feature: "who approves this"
// would depend on row order.
func TestReportingRejectsOverlappingConcurrentManager(t *testing.T) {
	s, org, req, app := fixture(t)
	ctx := context.Background()
	bossA := extraPrincipal(t, s, org, "approver")
	bossB := extraPrincipal(t, s, org, "approver")

	start := time.Now().Add(-time.Hour)
	if _, e := s.CreateRelationship(ctx, org, req, app, "reports_to", bossA, start, nil); e != nil {
		t.Fatalf("first manager: %v", e)
	}
	// Overlapping and open-ended: must be refused.
	if _, e := s.CreateRelationship(ctx, org, req, app, "reports_to", bossB, time.Now(), nil); e == nil {
		t.Fatal("a second concurrent manager was accepted")
	}
	// A non-overlapping period in the past is fine: this is a manager change, not
	// a contradiction.
	oldStart := start.Add(-3 * time.Hour)
	oldEnd := start.Add(-2 * time.Hour)
	if _, e := s.CreateRelationship(ctx, org, req, app, "reports_to", bossB, oldStart, &oldEnd); e != nil {
		t.Fatalf("historical non-overlapping manager: %v", e)
	}
}

// The matrix is the point: a person belongs to several teams at once, and covers
// for several colleagues at once. Those must NOT be constrained like a manager.
func TestMatrixMembershipAndCoverMayOverlap(t *testing.T) {
	s, org, req, app := fixture(t)
	ctx := context.Background()
	teamA, e := s.CreatePosition(ctx, org, req, "Finance", "")
	if e != nil {
		t.Fatal(e)
	}
	teamB, e := s.CreatePosition(ctx, org, req, "Ops", "")
	if e != nil {
		t.Fatal(e)
	}
	start := time.Now().Add(-time.Hour)
	if _, e := s.CreateRelationship(ctx, org, req, app, "member_of", teamA.ID, start, nil); e != nil {
		t.Fatalf("team A: %v", e)
	}
	if _, e := s.CreateRelationship(ctx, org, req, app, "member_of", teamB.ID, start, nil); e != nil {
		t.Fatalf("concurrent second team membership refused: %v", e)
	}
	// Cover for two colleagues at once is also normal.
	other1 := extraPrincipal(t, s, org, "requester")
	other2 := extraPrincipal(t, s, org, "requester")
	if _, e := s.CreateRelationship(ctx, org, req, app, "covers_for", other1, start, nil); e != nil {
		t.Fatalf("cover 1: %v", e)
	}
	if _, e := s.CreateRelationship(ctx, org, req, app, "covers_for", other2, start, nil); e != nil {
		t.Fatalf("concurrent second cover refused: %v", e)
	}
}

// A management cycle is not a hierarchy. It must be refused when created, not
// discovered later by a query that loops.
func TestReportingRejectsCycle(t *testing.T) {
	s, org, req, _ := fixture(t)
	ctx := context.Background()
	a := extraPrincipal(t, s, org, "approver")
	b := extraPrincipal(t, s, org, "approver")
	start := time.Now().Add(-time.Hour)

	if _, e := s.CreateRelationship(ctx, org, req, a, "reports_to", b, start, nil); e != nil {
		t.Fatalf("a reports to b: %v", e)
	}
	// b reports to a would close a loop.
	if _, e := s.CreateRelationship(ctx, org, req, b, "reports_to", a, start, nil); e == nil {
		t.Fatal("a management cycle was accepted")
	}
	// A longer chain that loops back must also be refused.
	c := extraPrincipal(t, s, org, "approver")
	if _, e := s.CreateRelationship(ctx, org, req, b, "reports_to", c, start, nil); e != nil {
		t.Fatalf("b reports to c: %v", e)
	}
	if _, e := s.CreateRelationship(ctx, org, req, c, "reports_to", a, start, nil); e == nil {
		t.Fatal("a three-node management cycle was accepted")
	}
}

// Ending a relationship closes its interval rather than deleting the row, so the
// history remains answerable.
func TestEndRelationshipClosesInterval(t *testing.T) {
	s, org, req, app := fixture(t)
	ctx := context.Background()
	boss := extraPrincipal(t, s, org, "approver")
	start := time.Now().Add(-2 * time.Hour)
	rel, e := s.CreateRelationship(ctx, org, req, app, "reports_to", boss, start, nil)
	if e != nil {
		t.Fatal(e)
	}
	cut := time.Now().Add(-time.Hour)
	if e := s.EndRelationship(ctx, org, req, rel.ID, cut); e != nil {
		t.Fatalf("end: %v", e)
	}
	if rows, e := s.RelationshipsAsOf(ctx, org, time.Now()); e != nil {
		t.Fatal(e)
	} else if len(rows) != 0 {
		t.Fatalf("ended relationship still current: %+v", rows)
	}
	// The history survives: it is still readable for the period it was true.
	if rows, e := s.RelationshipsAsOf(ctx, org, start.Add(time.Minute)); e != nil {
		t.Fatal(e)
	} else if len(rows) != 1 {
		t.Fatalf("history was erased by ending the relationship: %+v", rows)
	}
}

// Reporting to someone is not authority over them. The spec is explicit that
// "reports to" must be displayed separately from "can approve", and that a
// reporting relationship must not grant access.
func TestReportingDoesNotGrantApprovalAuthority(t *testing.T) {
	s, org, req, app := fixture(t)
	ctx := context.Background()
	// `app` is an approver; `req` is a requester. Make the requester the MANAGER
	// of the approver. If structure granted authority, the manager would now be
	// able to approve.
	boss := req
	if _, e := s.CreateRelationship(ctx, org, req, app, "reports_to", boss, time.Now().Add(-time.Hour), nil); e != nil {
		t.Fatal(e)
	}
	// The requester's role is unchanged: still a requester, still unable to
	// approve their own proposal (separation of duties still applies). Read the
	// role through the same path the server uses, which re-reads current
	// authority from the kernel rather than trusting a claim.
	got, e := s.ResolveIdentity(ctx, platform.Identity{ID: boss, OrgID: org, Role: "admin"})
	if e != nil {
		t.Fatal(e)
	}
	if got.Role != "requester" {
		t.Fatalf("reporting changed the manager's role to %q (claiming admin also failed to be corrected)", got.Role)
	}
	// And the approver's role is unchanged too: being managed does not remove
	// their authority.
	if r, e := s.ResolveIdentity(ctx, platform.Identity{ID: app, OrgID: org, Role: "requester"}); e != nil {
		t.Fatal(e)
	} else if r.Role != "approver" {
		t.Fatalf("reporting changed the subordinate's role to %q", r.Role)
	}
}
