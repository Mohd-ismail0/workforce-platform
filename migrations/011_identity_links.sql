-- 011_identity_links: map a verified external identity to a platform principal.
--
-- Why this is a separate table and not columns on `principals`: resolution must happen
-- BEFORE the tenant is known. At request time we hold a verified (issuer, subject) and
-- nothing else — the org is what the lookup is supposed to discover. Any design that
-- required org scoping first would be circular.
--
-- That ordering is also why this table deliberately does NOT carry the tenant RLS policy
-- used elsewhere: current_setting('app.org_id') is unset during resolution, so a tenant
-- policy would make every lookup return zero rows. It is not added to the RLS loop in
-- 004_security.sql. The compensating controls are that no API route exposes this table
-- and the only query against it is the resolution path itself.
--
-- Invariant: (issuer, subject) is globally unique. One external account maps to exactly
-- one principal platform-wide, so the same human cannot become two different principals
-- in two organizations by signing in twice.
--
-- Deliberately NOT done here: any auto-provisioning on first login, and any linking by
-- email. Email is reassignable, so email-based linking is an account-takeover primitive.

CREATE TABLE IF NOT EXISTS identity_links (
  issuer       text NOT NULL,
  subject      text NOT NULL,
  org_id       text NOT NULL,
  principal_id text NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT clock_timestamp(),
  created_by   text NOT NULL,
  active       boolean NOT NULL DEFAULT true,
  -- The essential invariant: one external account, one principal. Platform-wide, not
  -- per-org, because a per-org key would let the same subject link to two principals.
  PRIMARY KEY (issuer, subject),
  FOREIGN KEY (org_id, principal_id) REFERENCES principals(org_id, id)
);

-- Resolution is by (issuer, subject) and hits the primary key above. This index serves
-- the administrative direction ("which external accounts back this principal").
CREATE INDEX IF NOT EXISTS identity_links_principal_idx
  ON identity_links (org_id, principal_id);

INSERT INTO workforce_migrations(version) VALUES ('011_identity_links')
  ON CONFLICT DO NOTHING;
