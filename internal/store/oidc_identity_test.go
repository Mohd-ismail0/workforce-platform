package store

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"workforce.local/platform/internal/platform"
)

// Identity linking is the join between "who Logto says you are" and "what this platform
// lets you do". It is security-critical: a bug here either locks people out or hands one
// person another's authority, so each property below is asserted rather than assumed.
//
// Every issuer/subject is unique per run because the database persists between runs and
// (issuer, subject) is a global primary key.

// identityFixture creates an org with two principals, so that "which principal did we
// resolve to" is a real question rather than a foregone conclusion.
func identityFixture(t *testing.T) (s *Store, org, alice, bob string) {
	t.Helper()
	// Skips rather than fails when DATABASE_URL is absent, matching the other
	// DB-backed store tests: these properties live in SQL, so they cannot be faked.
	u := os.Getenv("DATABASE_URL")
	if u == "" {
		t.Skip("DATABASE_URL required")
	}
	ctx := context.Background()
	s, e := Open(ctx, u)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)

	org, alice, bob = platform.NewID(), platform.NewID(), platform.NewID()
	e = s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, "INSERT INTO organizations(id,name) VALUES($1,'identity fixture')", org); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			"INSERT INTO principals(id,org_id,name,role) VALUES($1,$3,'Alice','requester'),($2,$3,'Bob','approver')",
			alice, bob, org)
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	return s, org, alice, bob
}

func uniqueIssuer(t *testing.T) string {
	return "https://auth.xsama.org/oidc#" + platform.NewID()
}

func TestIdentityLinkResolvesToTheLinkedPrincipal(t *testing.T) {
	s, org, alice, _ := identityFixture(t)
	ctx := context.Background()
	iss, sub := uniqueIssuer(t), platform.NewID()

	if e := s.LinkOIDCIdentity(ctx, org, iss, sub, alice, alice); e != nil {
		t.Fatalf("link: %v", e)
	}
	got, e := s.ResolveOIDCIdentity(ctx, iss, sub)
	if e != nil {
		t.Fatalf("resolve: %v", e)
	}
	if got.ID != alice || got.OrgID != org {
		t.Fatalf("resolved to %s/%s, want %s/%s", got.OrgID, got.ID, org, alice)
	}
	// Authority must come from the principals row, not the token: role and name are
	// read from the database, which is what keeps a token a claim about identity.
	if got.Role != "requester" {
		t.Fatalf("role = %q, want requester (from the principals row)", got.Role)
	}
	if got.Name != "Alice" {
		t.Fatalf("name = %q, want Alice", got.Name)
	}
}

// The core anti-takeover property: one external account maps to exactly one principal,
// platform-wide. This is also what keeps separation of duties meaningful — if one
// account could resolve to two principal rows, "a different human approved it" would
// become a formality that one person could satisfy alone.
func TestIdentityLinkCannotPointAtTwoPrincipals(t *testing.T) {
	s, org, alice, bob := identityFixture(t)
	ctx := context.Background()
	iss, sub := uniqueIssuer(t), platform.NewID()

	if e := s.LinkOIDCIdentity(ctx, org, iss, sub, alice, alice); e != nil {
		t.Fatalf("first link: %v", e)
	}
	if e := s.LinkOIDCIdentity(ctx, org, iss, sub, bob, alice); e == nil {
		t.Fatal("re-linking one external account to a different principal was accepted")
	}
	// ...and the original binding must be untouched by the refused attempt.
	got, e := s.ResolveOIDCIdentity(ctx, iss, sub)
	if e != nil {
		t.Fatalf("resolve after refused re-link: %v", e)
	}
	if got.ID != alice {
		t.Fatalf("a refused re-link changed the binding: got %s want %s", got.ID, alice)
	}
}

// The same (issuer, subject) cannot be linked a second time, even in a DIFFERENT org.
// A per-org key would have allowed one human to become two principals by signing in
// twice, which is the duplication this guards against.
func TestIdentityLinkIsGloballyUniqueNotPerOrg(t *testing.T) {
	s, orgA, alice, _ := identityFixture(t)
	ctx := context.Background()

	orgB, carol := platform.NewID(), platform.NewID()
	if e := s.WithOrg(ctx, orgB, func(tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, "INSERT INTO organizations(id,name) VALUES($1,'second org')", orgB); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, "INSERT INTO principals(id,org_id,name,role) VALUES($1,$2,'Carol','requester')", carol, orgB)
		return e
	}); e != nil {
		t.Fatal(e)
	}

	iss, sub := uniqueIssuer(t), platform.NewID()
	if e := s.LinkOIDCIdentity(ctx, orgA, iss, sub, alice, alice); e != nil {
		t.Fatalf("link in org A: %v", e)
	}
	if e := s.LinkOIDCIdentity(ctx, orgB, iss, sub, carol, carol); e == nil {
		t.Fatal("the same external account was linked in a second organization")
	}
	// Resolution must still yield org A. If the org were taken from anything other than
	// the stored link, this is where a cross-tenant read would show up.
	got, e := s.ResolveOIDCIdentity(ctx, iss, sub)
	if e != nil {
		t.Fatalf("resolve: %v", e)
	}
	if got.OrgID != orgA || got.ID != alice {
		t.Fatalf("resolved to %s/%s, want %s/%s", got.OrgID, got.ID, orgA, alice)
	}
}

// Re-linking the identical binding is a no-op, not an error: provisioning scripts and
// retries must be safe to re-run.
func TestIdentityLinkIsIdempotent(t *testing.T) {
	s, org, alice, _ := identityFixture(t)
	ctx := context.Background()
	iss, sub := uniqueIssuer(t), platform.NewID()

	for i := 0; i < 3; i++ {
		if e := s.LinkOIDCIdentity(ctx, org, iss, sub, alice, alice); e != nil {
			t.Fatalf("link attempt %d: %v", i+1, e)
		}
	}
}

// An authenticated-but-unlinked identity must be REFUSED. Authentication establishes
// who someone is; it is not an entitlement to act.
func TestUnlinkedIdentityIsRefused(t *testing.T) {
	s, _, _, _ := identityFixture(t)
	if _, e := s.ResolveOIDCIdentity(context.Background(), uniqueIssuer(t), platform.NewID()); e == nil {
		t.Fatal("an unlinked external account resolved to a platform identity")
	}
}

// A deactivated principal must stop resolving immediately, even though the token that
// proves their identity remains cryptographically valid. Otherwise offboarding would
// depend on token expiry.
func TestDeactivatedPrincipalStopsResolving(t *testing.T) {
	s, org, alice, _ := identityFixture(t)
	ctx := context.Background()
	iss, sub := uniqueIssuer(t), platform.NewID()

	if e := s.LinkOIDCIdentity(ctx, org, iss, sub, alice, alice); e != nil {
		t.Fatalf("link: %v", e)
	}
	if _, e := s.ResolveOIDCIdentity(ctx, iss, sub); e != nil {
		t.Fatalf("resolve before deactivation: %v", e)
	}
	if e := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, "UPDATE principals SET active=false WHERE org_id=$1 AND id=$2", org, alice)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ResolveOIDCIdentity(ctx, iss, sub); e == nil {
		t.Fatal("a deactivated principal still resolved")
	}
}

// Unlinking deactivates rather than deletes: the audit trail must keep showing that the
// external account once mapped here.
func TestUnlinkStopsResolutionButKeepsTheRecord(t *testing.T) {
	s, org, alice, _ := identityFixture(t)
	ctx := context.Background()
	iss, sub := uniqueIssuer(t), platform.NewID()

	if e := s.LinkOIDCIdentity(ctx, org, iss, sub, alice, alice); e != nil {
		t.Fatalf("link: %v", e)
	}
	if e := s.UnlinkOIDCIdentity(ctx, org, iss, sub); e != nil {
		t.Fatalf("unlink: %v", e)
	}
	if _, e := s.ResolveOIDCIdentity(ctx, iss, sub); e == nil {
		t.Fatal("an unlinked external account still resolved")
	}
	var active bool
	if e := s.Pool.QueryRow(ctx,
		"SELECT active FROM identity_links WHERE issuer=$1 AND subject=$2", iss, sub).Scan(&active); e != nil {
		t.Fatalf("the link row should still exist for audit, but: %v", e)
	}
	if active {
		t.Fatal("the link row is still marked active after unlink")
	}
	// Re-linking the same pair is allowed and reactivates the row.
	if e := s.LinkOIDCIdentity(ctx, org, iss, sub, alice, alice); e != nil {
		t.Fatalf("re-link: %v", e)
	}
	if _, e := s.ResolveOIDCIdentity(ctx, iss, sub); e != nil {
		t.Fatalf("resolve after re-link: %v", e)
	}
}

// Linking to a principal that does not exist in the stated org must fail loudly rather
// than silently creating a dangling binding.
func TestLinkRefusesUnknownPrincipal(t *testing.T) {
	s, org, alice, _ := identityFixture(t)
	ctx := context.Background()

	if e := s.LinkOIDCIdentity(ctx, org, uniqueIssuer(t), platform.NewID(), platform.NewID(), alice); e == nil {
		t.Fatal("linking to a non-existent principal was accepted")
	}
	// A principal in a DIFFERENT org must not be linkable by naming its id here.
	orgB, carol := platform.NewID(), platform.NewID()
	if e := s.WithOrg(ctx, orgB, func(tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, "INSERT INTO organizations(id,name) VALUES($1,'third org')", orgB); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, "INSERT INTO principals(id,org_id,name,role) VALUES($1,$2,'Carol','requester')", carol, orgB)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if e := s.LinkOIDCIdentity(ctx, org, uniqueIssuer(t), platform.NewID(), carol, alice); e == nil {
		t.Fatal("a principal from another organization was linked")
	}
}

// Missing inputs must be refused, so a caller cannot accidentally create a binding keyed
// on empty strings — which would be a single shared identity for every such request.
func TestLinkRefusesEmptyInputs(t *testing.T) {
	s, org, alice, _ := identityFixture(t)
	ctx := context.Background()
	cases := []struct{ org, iss, sub, pid, by string }{
		{"", "i", "s", alice, alice},
		{org, "", "s", alice, alice},
		{org, "i", "", alice, alice},
		{org, "i", "s", "", alice},
		{org, "i", "s", alice, ""},
	}
	for i, c := range cases {
		if e := s.LinkOIDCIdentity(ctx, c.org, c.iss, c.sub, c.pid, c.by); e == nil {
			t.Fatalf("case %d: incomplete link input was accepted", i)
		}
	}
	if _, e := s.ResolveOIDCIdentity(ctx, "", ""); e == nil {
		t.Fatal("empty issuer and subject resolved")
	}
}
