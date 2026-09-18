package store

import (
	"context"
	"testing"
	"time"
)

// milestoneFixture returns a store, a fresh organization, a project in it, and
// the id of an active principal in that organization to act as.
func milestoneFixture(t *testing.T) (*Store, string, string, string) {
	t.Helper()
	s, org, actor, _ := fixture(t)
	p, e := s.CreateProject(context.Background(), org, "Apollo", "")
	if e != nil {
		t.Fatal(e)
	}
	return s, org, p.ID, actor
}

func day(y int, m time.Month, d int) *time.Time {
	v := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &v
}

// A milestone with no stated acceptance evidence can only ever be "completed" by
// assertion, so the declaration is required at creation.
func TestMilestoneRequiresAcceptanceEvidence(t *testing.T) {
	s, org, proj, actor := milestoneFixture(t)
	ctx := context.Background()
	if _, e := s.CreateMilestone(ctx, org, actor, proj, "Cutover", "", nil, nil); e == nil {
		t.Fatal("a milestone with no acceptance evidence was accepted; 'met' would mean nothing")
	}
	if _, e := s.CreateMilestone(ctx, org, actor, proj, "Cutover", "   ", nil, nil); e == nil {
		t.Fatal("whitespace was accepted as acceptance evidence")
	}
	m, e := s.CreateMilestone(ctx, org, actor, proj, "Cutover",
		"All 42 SKUs reconciled with no variance above 1 unit", nil, nil)
	if e != nil {
		t.Fatalf("create: %v", e)
	}
	if m.Status != "planned" {
		t.Fatalf("new milestone status %q want planned", m.Status)
	}
}

// Forecast and commitment are different facts. Revising a forecast must not
// silently move a promise, and vice versa.
func TestForecastAndCommittedStaySeparate(t *testing.T) {
	s, org, proj, actor := milestoneFixture(t)
	ctx := context.Background()
	m, e := s.CreateMilestone(ctx, org, actor, proj, "Pilot",
		"Three colleagues complete a full cycle unaided",
		day(2026, time.March, 31), day(2026, time.April, 15))
	if e != nil {
		t.Fatal(e)
	}
	if m.CommittedDate != "2026-03-31" {
		t.Fatalf("committed = %q want 2026-03-31", m.CommittedDate)
	}
	if m.ForecastDate != "2026-04-15" {
		t.Fatalf("forecast = %q want 2026-04-15", m.ForecastDate)
	}
	// Revising the forecast leaves the commitment alone: that is the whole point
	// of holding them apart, and a plan whose promise moves whenever the guess
	// moves has no promise in it.
	m2, e := s.SetMilestoneForecast(ctx, org, actor, m.ID, day(2026, time.May, 1))
	if e != nil {
		t.Fatalf("set forecast: %v", e)
	}
	if m2.ForecastDate != "2026-05-01" {
		t.Fatalf("forecast = %q want 2026-05-01", m2.ForecastDate)
	}
	if m2.CommittedDate != "2026-03-31" {
		t.Fatalf("revising the forecast moved the commitment to %q", m2.CommittedDate)
	}
}

// A milestone is met on evidence, and only once its own prerequisites are met.
// "Everything under it happened to move" is not completion.
func TestMilestoneCompletionRequiresEvidenceAndPrerequisites(t *testing.T) {
	s, org, proj, actor := milestoneFixture(t)
	ctx := context.Background()
	first, e := s.CreateMilestone(ctx, org, actor, proj, "Reconcile",
		"All 42 SKUs reconciled", nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	second, e := s.CreateMilestone(ctx, org, actor, proj, "Send replies",
		"Ten supplier replies sent and read back", nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e := s.AddMilestoneDependency(ctx, org, second.ID, first.ID); e != nil {
		t.Fatalf("dependency: %v", e)
	}

	// No evidence, no completion.
	if e := s.CompleteMilestone(ctx, org, actor, second.ID, "", second.Version); e == nil {
		t.Fatal("a milestone was met with no evidence recorded")
	}
	// Even with evidence, an unmet prerequisite blocks it.
	if e := s.CompleteMilestone(ctx, org, actor, second.ID, "ten replies sent", second.Version); e == nil {
		t.Fatal("a milestone was met while its prerequisite was still unmet")
	}

	// Meet the prerequisite, then the dependent one is allowed.
	if e := s.CompleteMilestone(ctx, org, actor, first.ID, "42/42 reconciled, zero variance", first.Version); e != nil {
		t.Fatalf("meet prerequisite: %v", e)
	}
	cur, e := s.GetMilestone(ctx, org, second.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e := s.CompleteMilestone(ctx, org, actor, second.ID, "ten replies sent and verified", cur.Version); e != nil {
		t.Fatalf("meet dependent milestone: %v", e)
	}
	got, e := s.GetMilestone(ctx, org, second.ID)
	if e != nil {
		t.Fatal(e)
	}
	if got.Status != "met" {
		t.Fatalf("status %q want met", got.Status)
	}
	if got.MetEvidence == "" {
		t.Fatal("completion recorded no evidence")
	}
	if got.MetAt == "" {
		t.Fatal("completion recorded no time")
	}
}

// Milestone prerequisites must not loop, for the same reason task dependencies
// must not: an open-ended walk is not a plan.
func TestMilestoneDependencyCycleRejected(t *testing.T) {
	s, org, proj, actor := milestoneFixture(t)
	ctx := context.Background()
	a, e := s.CreateMilestone(ctx, org, actor, proj, "A", "evidence a", nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	b, e := s.CreateMilestone(ctx, org, actor, proj, "B", "evidence b", nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	c, e := s.CreateMilestone(ctx, org, actor, proj, "C", "evidence c", nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e := s.AddMilestoneDependency(ctx, org, a.ID, b.ID); e != nil {
		t.Fatalf("a after b: %v", e)
	}
	if e := s.AddMilestoneDependency(ctx, org, b.ID, c.ID); e != nil {
		t.Fatalf("b after c: %v", e)
	}
	if e := s.AddMilestoneDependency(ctx, org, c.ID, a.ID); e == nil {
		t.Fatal("a three-node milestone cycle was accepted")
	}
	if e := s.AddMilestoneDependency(ctx, org, a.ID, a.ID); e == nil {
		t.Fatal("a self-dependency was accepted")
	}
}

// A milestone must not be completable by a principal with no standing, and the
// completion must survive a re-read (it is state, not a response field).
func TestMilestoneCompletionIsDurableAndAuthorised(t *testing.T) {
	s, org, proj, actor := milestoneFixture(t)
	ctx := context.Background()
	m, e := s.CreateMilestone(ctx, org, actor, proj, "Durable", "evidence required", nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e := s.CompleteMilestone(ctx, org, actor, m.ID, "the evidence", m.Version); e != nil {
		t.Fatal(e)
	}
	// Completion is durable: a fresh read still reports it.
	again, e := s.GetMilestone(ctx, org, m.ID)
	if e != nil {
		t.Fatal(e)
	}
	if again.Status != "met" || again.MetEvidence != "the evidence" {
		t.Fatalf("completion did not survive re-read: %+v", again)
	}
	// A stale version cannot re-complete or re-open it.
	if e := s.CompleteMilestone(ctx, org, actor, m.ID, "second evidence", m.Version); e == nil {
		t.Fatal("a stale version completed an already-met milestone")
	}
}
