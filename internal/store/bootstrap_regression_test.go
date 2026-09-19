package store

import (
	"context"
	"testing"

	"workforce.local/platform/internal/platform"
)

// Onboarding a principal into an organization that ALREADY EXISTS must create
// that principal.
//
// This is the second-approver case, and it was broken: the existence check for
// the principal reused the flag from the organization check, so an existing
// organization left the flag `true` and the code took the "already exists"
// branch — skipping the insert. The symptom was a clear but misleading error
// ("principal does not exist in this organization") thrown by the linking step
// afterwards, so the real cause was one step earlier.
func TestBootstrapIntoExistingOrganizationCreatesThePrincipal(t *testing.T) {
	s, org, actor, _ := fixture(t) // the fixture already created this organization
	ctx := context.Background()

	// BOTH values must be unique per run, because this schema is global where you
	// might expect per-org: `principals.id` is a global primary key, and
	// `identity_links` is unique on (issuer, subject). Hardcoding either makes the
	// test pass once and fail on every subsequent run against the same database.
	pid := platform.NewID()
	sub := "subject-" + pid
	res, e := s.BootstrapIdentity(ctx, BootstrapRequest{
		OrgID:         org,
		PrincipalID:   pid,
		PrincipalName: "Second Approver",
		Role:          "approver",
		Issuer:        "https://issuer.test/oidc",
		Subject:       sub,
		Actor:         actor,
	})
	if e != nil {
		t.Fatalf("bootstrap into an existing organization: %v", e)
	}
	if res.OrgCreated {
		t.Fatal("an existing organization was reported as created")
	}
	if !res.PrincipalCreated {
		t.Fatal("the principal was NOT created, which is the bug this test exists for")
	}
	if !res.Linked {
		t.Fatal("the identity was not linked")
	}

	// The principal must actually be there, and usable as an approver.
	people, e := s.ListPeople(ctx, org)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, p := range people {
		if p.ID == pid {
			found = true
			if p.Role != "approver" {
				t.Fatalf("role = %q want approver", p.Role)
			}
			if !p.Active {
				t.Fatal("a freshly bootstrapped principal is not active")
			}
		}
	}
	if !found {
		t.Fatal("the bootstrapped principal is absent from the organization")
	}

	// And the identity resolves, which is what makes sign-in possible.
	got, e := s.ResolveOIDCIdentity(ctx, "https://issuer.test/oidc", sub)
	if e != nil {
		t.Fatalf("the bootstrapped identity does not resolve: %v", e)
	}
	if got.ID != pid || got.OrgID != org || got.Role != "approver" {
		t.Fatalf("resolved identity is wrong: %+v", got)
	}

	// Re-running must be idempotent: it may re-activate, but it must NOT create a
	// second time and must not silently change the role.
	again, e := s.BootstrapIdentity(ctx, BootstrapRequest{
		OrgID: org, PrincipalID: pid, PrincipalName: "Renamed",
		Role: "admin", Issuer: "https://issuer.test/oidc", Subject: sub,
		Actor: actor,
	})
	if e != nil {
		t.Fatalf("idempotent re-run: %v", e)
	}
	if again.PrincipalCreated {
		t.Fatal("a re-run reported creating the principal again")
	}
	if got, e := s.ResolveOIDCIdentity(ctx, "https://issuer.test/oidc", sub); e != nil {
		t.Fatal(e)
	} else if got.Role != "approver" {
		t.Fatalf("an operator re-run silently changed the role to %q", got.Role)
	}
}
