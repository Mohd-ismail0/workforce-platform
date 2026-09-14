-- 012_bff_sessions: server-side login transactions and sessions for the browser flow.
--
-- Why these tables and not cookies carrying state:
--   * The cookie value is an OPAQUE RANDOM ID whose SHA-256 is what we store. A database
--     read (backup, replica, log excerpt) therefore yields no usable session cookie, which
--     a cookie holding the authority itself could never promise.
--   * A login transaction must be single-use and short-lived. Storing it lets us mark it
--     consumed atomically, so a replayed callback cannot mint a second session.
--
-- NEITHER TABLE CARRIES THE TENANT RLS POLICY, for the same reason as identity_links: both
-- are read BEFORE the organization is known. A session lookup happens on an unauthenticated
-- request, so current_setting('app.org_id') is unset and a tenant policy would make every
-- lookup return zero rows. The compensating controls are that no API route exposes these
-- tables, the only queries are the ones below, and org_id is what the lookup DISCOVERS
-- rather than what it assumes.

CREATE TABLE IF NOT EXISTS login_transactions (
  -- SHA-256 of the `state` value handed to the browser. Hashing means a leaked row cannot
  -- be turned into a forged login, and the raw state never rests in the database.
  state_hash  text PRIMARY KEY,
  -- PKCE verifier for this transaction. It is a short-lived secret that must survive the
  -- redirect round trip, so it is stored rather than derived; sensitivity is bounded by the
  -- 10-minute TTL and by also needing the authorization code, which only the browser has.
  verifier    text NOT NULL,
  -- Bound to the ID token, so a token minted for a different login cannot be replayed here.
  nonce       text NOT NULL,
  return_to   text NOT NULL DEFAULT '/',
  created_at  timestamptz NOT NULL DEFAULT clock_timestamp(),
  expires_at  timestamptz NOT NULL,
  -- NULL until used. Set exactly once, which is what makes a callback single-use.
  consumed_at timestamptz
);

-- Housekeeping walks by expiry.
CREATE INDEX IF NOT EXISTS login_transactions_expiry_idx ON login_transactions (expires_at);

CREATE TABLE IF NOT EXISTS sessions (
  -- SHA-256 of the opaque cookie value. The raw value is never stored.
  id_hash      text PRIMARY KEY,
  org_id       text NOT NULL,
  principal_id text NOT NULL,
  -- The verified external identity this session came from. Kept so a session can be traced
  -- to the account that created it even after the link is changed, and so revocation can be
  -- applied per (issuer, subject) rather than only per session.
  issuer       text NOT NULL,
  subject      text NOT NULL,
  -- Synchroniser token for CSRF. The cookie is host-scoped and therefore SENT on the SPA's
  -- cross-port requests, so the browser will happily attach it to a request it should not
  -- authorise; this token is what makes a state-changing request deliberate.
  csrf_token   text NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT clock_timestamp(),
  last_seen_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  expires_at   timestamptz NOT NULL,
  -- Set on logout. Reuse after logout must fail, so revocation is a tombstone rather than a
  -- delete: a deleted row and a never-issued row would look identical.
  revoked_at   timestamptz,
  -- Set when this session is replaced by a fresh one (rotation). A rotated-away session is
  -- refused, which is what defends against session fixation.
  rotated_to   text,
  FOREIGN KEY (org_id, principal_id) REFERENCES principals (org_id, id)
);

CREATE INDEX IF NOT EXISTS sessions_principal_idx ON sessions (org_id, principal_id);
CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions (expires_at);
-- Revoking every session for one external account is a security operation (offboarding,
-- suspected compromise), so it gets an index of its own.
CREATE INDEX IF NOT EXISTS sessions_identity_idx ON sessions (issuer, subject);

INSERT INTO workforce_migrations(version) VALUES ('012_bff_sessions')
  ON CONFLICT DO NOTHING;
