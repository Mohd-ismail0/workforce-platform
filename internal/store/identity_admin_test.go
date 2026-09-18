package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"workforce.local/platform/internal/platform"
)

// Identity onboarding. Each property below is a lockout or a privilege bug if it breaks, and
// none of them is visible in a happy-path sign-in.

func TestBootstrapIdentityCreatesOrgPrincipalAndLink(t *testing.T) {
	s, _, _, _ := identityFixture(t)
	ctx := context.Background()
	org, principal := platform.NewID(), platform.NewID()
	issuer, subject := "https://issuer.test", "subject-"+platform.NewID()

	res, err := s.BootstrapIdentity(ctx, BootstrapRequest{
		OrgID: org, OrgName: "Bootstrap Org",
		PrincipalID: principal, PrincipalName: "First Admin", Role: "admin",
		Issuer: issuer, Subject: subject, Actor: "operator@host",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !res.OrgCreated || !res.PrincipalCreated || !res.Linked {
		t.Fatalf("bootstrap reported an incomplete result: %+v", res)
	}

	// The whole point of bootstrapping: this identity now RESOLVES to a principal, which is
	// what makes the browser flow usable at all.
	id, err := s.ResolveOIDCIdentity(ctx, issuer, subject)
	if err != nil {
		t.Fatalf("the bootstrapped identity does not resolve: %v", err)
	}
	if id.OrgID != org || id.ID != principal || id.Role != "admin" {
		t.Fatalf("resolved to the wrong identity: %+v", id)
	}

	// Idempotent: re-running must be safe, and must not silently rewrite the role of a
	// principal that already exists (a bootstrap is not a privilege change).
	again, err := s.BootstrapIdentity(ctx, BootstrapRequest{
		OrgID: org, PrincipalID: principal, Role: "requester",
		Issuer: issuer, Subject: subject, Actor: "operator@host",
	})
	if err != nil {
		t.Fatalf("re-running bootstrap must be safe: %v", err)
	}
	if again.OrgCreated || again.PrincipalCreated {
		t.Fatalf("bootstrap created resources twice: %+v", again)
	}
	after, err := s.ResolveIdentity(ctx, platform.Identity{OrgID: org, ID: principal})
	if err != nil {
		t.Fatal(err)
	}
	if after.Role != "admin" {
		t.Fatalf("an operator bootstrap demoted an existing principal to %q", after.Role)
	}
}

func TestBootstrapIdentityRequiresCompleteInput(t *testing.T) {
	s, _, _, _ := identityFixture(t)
	ctx := context.Background()
	org, principal := platform.NewID(), platform.NewID()

	cases := []struct {
		name string
		q    BootstrapRequest
	}{
		{"no org", BootstrapRequest{PrincipalID: principal, Issuer: "i", Subject: "s", Actor: "a"}},
		{"no principal", BootstrapRequest{OrgID: org, Issuer: "i", Subject: "s", Actor: "a"}},
		{"no issuer", BootstrapRequest{OrgID: org, PrincipalID: principal, Subject: "s", Actor: "a"}},
		{"no subject", BootstrapRequest{OrgID: org, PrincipalID: principal, Issuer: "i", Actor: "a"}},
		{"no actor", BootstrapRequest{OrgID: org, PrincipalID: principal, Issuer: "i", Subject: "s"}},
		{"bad role", BootstrapRequest{OrgID: org, PrincipalID: principal, Issuer: "i", Subject: "s", Actor: "a", Role: "superuser"}},
	}
	for _, c := range cases {
		if _, err := s.BootstrapIdentity(ctx, c.q); err == nil {
			t.Fatalf("%s: bootstrap accepted an incomplete request", c.name)
		}
	}
}

func TestBootstrapIdentityRefusesOneSubjectTwoPrincipals(t *testing.T) {
	s, org, alice, bob := identityFixture(t)
	ctx := context.Background()
	issuer, subject := "https://issuer.test", "sub-"+platform.NewID()

	if err := s.LinkIdentityByAdmin(ctx, org, issuer, subject, alice, "operator"); err != nil {
		t.Fatalf("first link: %v", err)
	}
	// One external account maps to exactly one principal platform-wide. If this were allowed,
	// the same human could become two principals and inherit both sets of authority.
	res, err := s.BootstrapIdentity(ctx, BootstrapRequest{
		OrgID: org, PrincipalID: bob, Role: "admin",
		Issuer: issuer, Subject: subject, Actor: "operator",
	})
	if err == nil {
		t.Fatalf("the same subject was linked to a second principal (result %+v)", res)
	}
}

func TestListIdentityLinksIsScopedToOneOrg(t *testing.T) {
	s, org, alice, _ := identityFixture(t)
	ctx := context.Background()

	// identity_links carries no tenant RLS policy (resolution must happen before the org is
	// known), so the org filter inside ListIdentityLinks is the ONLY thing preventing one
	// tenant from enumerating another tenant's external accounts.
	if err := s.LinkIdentityByAdmin(ctx, org, "https://issuer.test", "sub-mine-"+platform.NewID(), alice, "operator"); err != nil {
		t.Fatal(err)
	}
	mine, err := s.ListIdentityLinks(ctx, org)
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) == 0 {
		t.Fatal("the link just created was not listed")
	}
	for _, l := range mine {
		if l.OrgID != org {
			t.Fatalf("ListIdentityLinks leaked a link from another organization: %+v", l)
		}
	}
	// A different tenant must see none of them.
	other, err := s.ListIdentityLinks(ctx, platform.NewID())
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Fatalf("an unrelated organization saw %d foreign links", len(other))
	}
}

func TestUnlinkProtectsLastAdministratorLink(t *testing.T) {
	s, org, alice, _ := identityFixture(t)
	ctx := context.Background()
	issuer, subject := "https://issuer.test", "sub-"+platform.NewID()

	// Promote alice to admin so the last-administrator guard applies. This uses the
	// tenant-scoped transaction, because forced RLS hides `principals` without app.org_id.
	if err := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE principals SET role='admin' WHERE org_id=$1 AND id=$2`, org, alice)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.LinkIdentityByAdmin(ctx, org, issuer, subject, alice, "operator"); err != nil {
		t.Fatal(err)
	}

	// Refuse to strip the last live link from an active administrator: that removes the
	// organization's only authenticated path into onboarding, with no way back in.
	if _, err := s.UnlinkIdentityByAdmin(ctx, org, issuer, subject, "operator"); err == nil {
		t.Fatal("unlinking the last external account of an active administrator was allowed")
	}
}

func TestUnlinkIdentityRevokesItsSessions(t *testing.T) {
	s, org, alice, _ := identityFixture(t)
	ctx := context.Background()
	issuer, subject := "https://issuer.test", "sub-"+platform.NewID()

	// alice stays a requester, so the last-admin guard does not apply and the unlink proceeds.
	if err := s.LinkIdentityByAdmin(ctx, org, issuer, subject, alice, "operator"); err != nil {
		t.Fatal(err)
	}
	raw, err := s.CreateSession(ctx, org, alice, issuer, subject, "csrf", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReadSession(ctx, raw); err != nil {
		t.Fatalf("session should be valid before unlink: %v", err)
	}

	revoked, err := s.UnlinkIdentityByAdmin(ctx, org, issuer, subject, "operator")
	if err != nil {
		t.Fatalf("unlink: %v", err)
	}
	if revoked == 0 {
		t.Fatal("unlink revoked no sessions, so access was not actually removed")
	}
	// The load-bearing property: the PRINCIPAL is still active, but the session must not
	// survive the link that authorised it. Otherwise "remove access" would not remove access
	// until the cookie happened to expire.
	if _, _, err := s.ReadSession(ctx, raw); err == nil {
		t.Fatal("a session remained usable after the identity link that authorised it was removed")
	}
	// And the identity itself no longer resolves, so a fresh login is refused too.
	if _, err := s.ResolveOIDCIdentity(ctx, issuer, subject); err == nil {
		t.Fatal("an unlinked identity still resolved to a principal")
	}
}

func TestUnlinkIdentityRefusesUnknownLink(t *testing.T) {
	s, org, _, _ := identityFixture(t)
	ctx := context.Background()
	if _, err := s.UnlinkIdentityByAdmin(ctx, org, "https://issuer.test", "never-linked-"+platform.NewID(), "operator"); err == nil {
		t.Fatal("unlinking a link that does not exist was accepted")
	}
}
