package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"workforce.local/platform/internal/platform"
)

// Login transactions and browser sessions.
//
// Two facts shape this file:
//
//  1. Both lookups happen on UNAUTHENTICATED requests, before the organization is known.
//     These tables therefore carry no tenant RLS policy (see migrations/012_bff_sessions.sql).
//     The principal is read back through the existing tenant-scoped path, so the
//     active-principal rule still applies once the org is known.
//  2. Nothing here trusts a token. Every session lookup re-reads the principal from the
//     database, so revoking a principal or a session takes effect on the next request
//     instead of whenever a cookie happens to expire.

var (
	// ErrLoginTransactionUnusable covers unknown, expired and already-consumed in ONE
	// error on purpose: distinguishing them would tell an attacker whether a guessed state
	// value was ever valid.
	ErrLoginTransactionUnusable = errors.New("login transaction is unknown, expired or already used")
	// ErrSessionInvalid covers every reason a session cookie is not usable.
	ErrSessionInvalid = errors.New("session is invalid")
)

// LoginTransaction is the server-side half of an in-flight login.
type LoginTransaction struct {
	Verifier string
	Nonce    string
	ReturnTo string
}

// CreateLoginTransaction records a pending login. The caller passes the HASH of the state
// value: the raw value lives only in the browser, so a database read cannot be used to
// forge a login.
func (s *Store) CreateLoginTransaction(ctx context.Context, stateHash, verifier, nonce, returnTo string, ttl time.Duration) error {
	if stateHash == "" || verifier == "" || nonce == "" {
		return errors.New("state hash, verifier and nonce are required")
	}
	if ttl <= 0 {
		return errors.New("login transaction requires a positive ttl")
	}
	if returnTo == "" {
		returnTo = "/"
	}
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO login_transactions (state_hash, verifier, nonce, return_to, expires_at)
		VALUES ($1, $2, $3, $4, clock_timestamp() + $5::interval)`,
		stateHash, verifier, nonce, returnTo, durationInterval(ttl))
	return err
}

// ConsumeLoginTransaction atomically claims a login transaction exactly once.
//
// The single-statement UPDATE ... WHERE consumed_at IS NULL is the whole point: a replayed
// callback cannot mint a second session, and two concurrent callbacks cannot both win,
// because the row can only transition from unconsumed to consumed once. A read-then-write
// pair would have a window between the two.
func (s *Store) ConsumeLoginTransaction(ctx context.Context, stateHash string) (LoginTransaction, error) {
	var tx LoginTransaction
	err := s.Pool.QueryRow(ctx, `
		UPDATE login_transactions
		   SET consumed_at = clock_timestamp()
		 WHERE state_hash = $1
		   AND consumed_at IS NULL
		   AND expires_at > clock_timestamp()
		RETURNING verifier, nonce, return_to`,
		stateHash).Scan(&tx.Verifier, &tx.Nonce, &tx.ReturnTo)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LoginTransaction{}, ErrLoginTransactionUnusable
		}
		return LoginTransaction{}, err
	}
	return tx, nil
}

// PurgeExpiredLoginTransactions removes spent and expired transactions. Kept separate from
// consumption so a failure to clean up can never fail a login.
func (s *Store) PurgeExpiredLoginTransactions(ctx context.Context) (int64, error) {
	tag, err := s.Pool.Exec(ctx, `
		DELETE FROM login_transactions
		 WHERE expires_at < clock_timestamp() - interval '1 hour'
		    OR (consumed_at IS NOT NULL AND consumed_at < clock_timestamp() - interval '1 hour')`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// Session is a verified browser session.
type Session struct {
	OrgID       string
	PrincipalID string
	Issuer      string
	Subject     string
	CSRFToken   string
	ExpiresAt   time.Time
}

// CreateSession issues a session and returns the RAW cookie value exactly once. Only its
// hash is stored, so the value cannot be recovered from the database afterwards.
func (s *Store) CreateSession(ctx context.Context, orgID, principalID, issuer, subject, csrfToken string, ttl time.Duration) (string, error) {
	if orgID == "" || principalID == "" || issuer == "" || subject == "" || csrfToken == "" {
		return "", errors.New("org, principal, issuer, subject and csrf token are required")
	}
	if ttl <= 0 {
		return "", errors.New("session requires a positive ttl")
	}
	token, err := platform.NewOpaqueToken()
	if err != nil {
		return "", err
	}
	_, err = s.Pool.Exec(ctx, `
		INSERT INTO sessions (id_hash, org_id, principal_id, issuer, subject, csrf_token, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, clock_timestamp() + $7::interval)`,
		platform.HashToken(token), orgID, principalID, issuer, subject, csrfToken, durationInterval(ttl))
	if err != nil {
		return "", err
	}
	return token, nil
}

// ReadSession resolves a raw session cookie into an authority-bearing identity.
//
// Two steps again, and for the same reason as OIDC resolution: the session row is read
// without tenant scoping (the org is what we are discovering), then the principal is read
// through the tenant-scoped path so RLS and the active check both apply. A revoked
// principal therefore invalidates their sessions immediately, without touching this table.
func (s *Store) ReadSession(ctx context.Context, rawToken string) (Session, platform.Identity, error) {
	if rawToken == "" {
		return Session{}, platform.Identity{}, ErrSessionInvalid
	}
	var sess Session
	err := s.Pool.QueryRow(ctx, `
		SELECT org_id, principal_id, issuer, subject, csrf_token, expires_at
		  FROM sessions
		 WHERE id_hash = $1
		   AND revoked_at IS NULL
		   AND expires_at > clock_timestamp()`,
		platform.HashToken(rawToken)).Scan(
		&sess.OrgID, &sess.PrincipalID, &sess.Issuer, &sess.Subject, &sess.CSRFToken, &sess.ExpiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, platform.Identity{}, ErrSessionInvalid
		}
		return Session{}, platform.Identity{}, err
	}

	id, err := s.ResolveIdentity(ctx, platform.Identity{OrgID: sess.OrgID, ID: sess.PrincipalID})
	if err != nil {
		return Session{}, platform.Identity{}, ErrSessionInvalid
	}

	// The link is what authorised this session, so it is re-checked here rather than only at
	// login. Without this, unlinking an external account would leave every session it created
	// fully usable until the cookie expired — "access removed" that did not remove access.
	//
	// A session with NO link row is refused. Sessions are only ever minted after a link has
	// resolved (/auth/callback resolves the link before it will create one), so an absent row
	// means this session was not authorised by any external identity and must not be honoured.
	// An earlier revision permitted it to keep test fixtures working; that was production
	// behaviour weakened for tests, so the fixtures now create links instead.
	var linkActive bool
	var linkOrg, linkPrincipal string
	err = s.Pool.QueryRow(ctx, `
		SELECT active, org_id, principal_id FROM identity_links
		 WHERE issuer = $1 AND subject = $2`, sess.Issuer, sess.Subject).Scan(&linkActive, &linkOrg, &linkPrincipal)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, platform.Identity{}, ErrSessionInvalid
		}
		return Session{}, platform.Identity{}, err
	}
	if !linkActive || linkOrg != sess.OrgID || linkPrincipal != sess.PrincipalID {
		return Session{}, platform.Identity{}, ErrSessionInvalid
	}

	// Best-effort liveness stamp; a failure here must not fail an otherwise valid request.
	_, _ = s.Pool.Exec(ctx,
		`UPDATE sessions SET last_seen_at = clock_timestamp() WHERE id_hash = $1`,
		platform.HashToken(rawToken))

	return sess, id, nil
}

// RevokeSession ends one session. It tombstones rather than deletes so that a revoked
// session and a never-issued one cannot be confused, and so reuse after logout fails.
func (s *Store) RevokeSession(ctx context.Context, rawToken string) error {
	tag, err := s.Pool.Exec(ctx, `
		UPDATE sessions SET revoked_at = clock_timestamp()
		 WHERE id_hash = $1 AND revoked_at IS NULL`, platform.HashToken(rawToken))
	if err != nil {
		return err
	}
	// Revoking an already-revoked or unknown session is not an error: logout must be
	// idempotent, and reporting "no such session" would confirm whether a cookie was valid.
	_ = tag
	return nil
}

// RevokeSessionsForIdentity ends every session belonging to one external account. This is
// the offboarding and suspected-compromise operation, so it works even when no session
// cookie is available to name them individually.
func (s *Store) RevokeSessionsForIdentity(ctx context.Context, issuer, subject string) (int64, error) {
	if issuer == "" || subject == "" {
		return 0, errors.New("issuer and subject are required")
	}
	tag, err := s.Pool.Exec(ctx, `
		UPDATE sessions SET revoked_at = clock_timestamp()
		 WHERE issuer = $1 AND subject = $2 AND revoked_at IS NULL`, issuer, subject)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// RevokeSessionsForPrincipal ends every session for a principal in an organization, which is
// what "disable this employee's access now" needs.
func (s *Store) RevokeSessionsForPrincipal(ctx context.Context, orgID, principalID string) (int64, error) {
	if orgID == "" || principalID == "" {
		return 0, errors.New("org and principal are required")
	}
	tag, err := s.Pool.Exec(ctx, `
		UPDATE sessions SET revoked_at = clock_timestamp()
		 WHERE org_id = $1 AND principal_id = $2 AND revoked_at IS NULL`, orgID, principalID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// durationInterval renders a Go duration as a Postgres interval literal. Passing a string
// rather than an integer keeps sub-second precision, so a test that wants a 50ms session
// gets one instead of a truncated zero.
func durationInterval(d time.Duration) string {
	return d.String()
}
