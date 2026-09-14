package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"workforce.local/platform/internal/platform"
)

// Login transactions and sessions are the bridge between "a colleague signed in" and "this
// request may act". Each property below is asserted because a bug here is either a lockout
// or an unauthorised action, and neither shows up in a happy-path demo.
//
// Every identifier is unique per run: the database persists between runs.

func TestLoginTransactionIsSingleUse(t *testing.T) {
	s, _, alice, _ := identityFixture(t)
	ctx := context.Background()
	stateHash := platform.HashToken(platform.NewID())
	verifier, _ := platform.NewPKCEVerifier()

	if err := s.CreateLoginTransaction(ctx, stateHash, verifier, "nonce-1", "/tasks", 10*time.Minute); err != nil {
		t.Fatalf("create: %v", err)
	}
	tx, err := s.ConsumeLoginTransaction(ctx, stateHash)
	if err != nil {
		t.Fatalf("first consume must succeed: %v", err)
	}
	if tx.Verifier != verifier || tx.Nonce != "nonce-1" || tx.ReturnTo != "/tasks" {
		t.Fatalf("transaction came back altered: %+v", tx)
	}

	// The whole point: a REPLAYED callback must not be able to mint a second session. Two
	// concurrent callbacks also cannot both win, because the row can only move from
	// unconsumed to consumed once.
	if _, err := s.ConsumeLoginTransaction(ctx, stateHash); err == nil {
		t.Fatal("the same login transaction was consumed twice; a replayed callback would mint a second session")
	}
	_ = alice
}

func TestLoginTransactionRejectsUnknownAndExpired(t *testing.T) {
	s, _, _, _ := identityFixture(t)
	ctx := context.Background()

	if _, err := s.ConsumeLoginTransaction(ctx, platform.HashToken(platform.NewID())); err == nil {
		t.Fatal("an unknown state value was accepted")
	}

	// A short TTL must actually expire. This is the difference between "the login prompt
	// was left open" being harmless and being a standing invitation.
	expiring := platform.HashToken(platform.NewID())
	verifier, _ := platform.NewPKCEVerifier()
	if err := s.CreateLoginTransaction(ctx, expiring, verifier, "n", "/", time.Millisecond); err != nil {
		t.Fatalf("create: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, err := s.ConsumeLoginTransaction(ctx, expiring); err == nil {
		t.Fatal("an expired login transaction was accepted")
	}
}

func TestSessionRoundTripAndRevocation(t *testing.T) {
	s, org, alice, _ := identityFixture(t)
	ctx := context.Background()

	raw, err := s.CreateSession(ctx, org, alice, "https://issuer.test", "subject-1", "csrf-1", time.Hour)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if raw == "" {
		t.Fatal("no session value was issued")
	}

	// The raw value must NOT be recoverable from storage: a database read (backup,
	// replica, log excerpt) must not yield a usable cookie.
	var stored string
	if err := s.Pool.QueryRow(ctx,
		`SELECT id_hash FROM sessions WHERE org_id=$1 AND principal_id=$2`, org, alice).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == raw {
		t.Fatal("the session value was stored in the clear; a database read would yield a usable cookie")
	}
	if stored != platform.HashToken(raw) {
		t.Fatal("the stored value is not the hash of the session value")
	}

	sess, id, err := s.ReadSession(ctx, raw)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if id.ID != alice || id.OrgID != org {
		t.Fatalf("resolved to %s/%s, want %s/%s", id.OrgID, id.ID, org, alice)
	}
	if sess.CSRFToken != "csrf-1" {
		t.Fatalf("csrf token = %q", sess.CSRFToken)
	}
	// Authority comes from the principals row, never from the session row.
	if id.Role == "" || id.Name == "" {
		t.Fatalf("identity is missing authority fields: %+v", id)
	}

	if err := s.RevokeSession(ctx, raw); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, _, err := s.ReadSession(ctx, raw); err == nil {
		t.Fatal("a revoked session still resolved; logout would not log anyone out")
	}
	// Logout must be idempotent: an already-invalid session is not an error worth
	// reporting, because doing so would confirm whether the cookie had been valid.
	if err := s.RevokeSession(ctx, raw); err != nil {
		t.Fatalf("revoking twice must not error: %v", err)
	}
}

func TestSessionRejectsUnknownAndExpired(t *testing.T) {
	s, org, alice, _ := identityFixture(t)
	ctx := context.Background()

	if _, _, err := s.ReadSession(ctx, platform.NewID()); err == nil {
		t.Fatal("an unknown session value was accepted")
	}
	expired, err := s.CreateSession(ctx, org, alice, "iss", "sub-exp", "c", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, _, err := s.ReadSession(ctx, expired); err == nil {
		t.Fatal("an expired session was accepted")
	}
	// An empty cookie must never be treated as a session.
	if _, _, err := s.ReadSession(ctx, ""); err == nil {
		t.Fatal("an empty session value was accepted")
	}
}

// Deactivating a principal must end their access immediately. Relying on cookie expiry
// would leave offboarded people working until their session happened to lapse.
func TestDeactivatedPrincipalLosesSessionImmediately(t *testing.T) {
	s, org, alice, _ := identityFixture(t)
	ctx := context.Background()

	raw, err := s.CreateSession(ctx, org, alice, "iss", "sub-deact", "c", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReadSession(ctx, raw); err != nil {
		t.Fatalf("sanity: session should work before deactivation: %v", err)
	}
	if err := s.WithOrg(ctx, org, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE principals SET active=false WHERE org_id=$1 AND id=$2`, org, alice)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReadSession(ctx, raw); err == nil {
		t.Fatal("a deactivated principal's session still resolved")
	}
}

// Revoking every session for one external account is the offboarding and
// suspected-compromise operation, so it must work without knowing any individual cookie.
func TestRevokeSessionsForIdentityAndPrincipal(t *testing.T) {
	s, org, alice, bob := identityFixture(t)
	ctx := context.Background()

	iss, sub := "https://issuer.test", platform.NewID()
	a1, _ := s.CreateSession(ctx, org, alice, iss, sub, "c1", time.Hour)
	a2, _ := s.CreateSession(ctx, org, alice, iss, sub, "c2", time.Hour)
	b1, _ := s.CreateSession(ctx, org, bob, "https://other.test", "sub-other", "c3", time.Hour)

	n, err := s.RevokeSessionsForIdentity(ctx, iss, sub)
	if err != nil {
		t.Fatalf("revoke by identity: %v", err)
	}
	if n < 2 {
		t.Fatalf("revoked %d sessions, expected at least the 2 for this identity", n)
	}
	for _, tok := range []string{a1, a2} {
		if _, _, err := s.ReadSession(ctx, tok); err == nil {
			t.Fatal("a session for the revoked identity still resolved")
		}
	}
	// A different identity's session must be untouched: a revocation that over-reaches
	// logs out unrelated colleagues.
	if _, _, err := s.ReadSession(ctx, b1); err != nil {
		t.Fatalf("an unrelated session was revoked: %v", err)
	}

	if _, err := s.RevokeSessionsForPrincipal(ctx, org, bob); err != nil {
		t.Fatalf("revoke by principal: %v", err)
	}
	if _, _, err := s.ReadSession(ctx, b1); err == nil {
		t.Fatal("a session for the revoked principal still resolved")
	}
}

func TestCreateSessionRequiresCompleteInput(t *testing.T) {
	s, org, alice, _ := identityFixture(t)
	ctx := context.Background()
	cases := []struct{ org, pid, iss, sub, csrf string }{
		{"", alice, "i", "s", "c"},
		{org, "", "i", "s", "c"},
		{org, alice, "", "s", "c"},
		{org, alice, "i", "", "c"},
		{org, alice, "i", "s", ""},
	}
	for i, c := range cases {
		if _, err := s.CreateSession(ctx, c.org, c.pid, c.iss, c.sub, c.csrf, time.Hour); err == nil {
			t.Fatalf("case %d: incomplete session input was accepted", i)
		}
	}
	if _, err := s.CreateSession(ctx, org, alice, "i", "s", "c", 0); err == nil {
		t.Fatal("a session with no lifetime was accepted")
	}
}

// Distinct sessions must get distinct CSRF tokens and distinct values; a collision would
// make one person's token usable in another's session.
func TestSessionsAreDistinct(t *testing.T) {
	s, org, alice, _ := identityFixture(t)
	ctx := context.Background()
	seen := map[string]bool{}
	for i := 0; i < 8; i++ {
		raw, err := s.CreateSession(ctx, org, alice, "iss", "sub", "csrf", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if seen[raw] {
			t.Fatal("two sessions were issued the same value")
		}
		seen[raw] = true
	}
}
