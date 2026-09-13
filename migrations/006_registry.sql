CREATE TABLE IF NOT EXISTS registry_releases (
  id text NOT NULL,
  org_id text NOT NULL REFERENCES organizations(id),
  family text NOT NULL,
  kind text NOT NULL CHECK (kind IN ('connector','harness','renderer','automation')),
  version text NOT NULL,
  digest text NOT NULL,
  state text NOT NULL DEFAULT 'quarantined' CHECK (state IN ('quarantined','verified','approved','installed','active','draining','disabled','revoked')),
  manifest jsonb NOT NULL,
  requested_capabilities jsonb NOT NULL DEFAULT '[]',
  granted_capabilities jsonb NOT NULL DEFAULT '[]',
  compatibility_range text NOT NULL DEFAULT '',
  simulation boolean NOT NULL DEFAULT false,
  provenance jsonb NOT NULL DEFAULT '{}',
  license_info jsonb NOT NULL DEFAULT '{}',
  created_by text NOT NULL,
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (org_id,id),
  UNIQUE (org_id,family,version),
  FOREIGN KEY (org_id,created_by) REFERENCES principals(org_id,id)
);
CREATE UNIQUE INDEX IF NOT EXISTS registry_active_family ON registry_releases(org_id,family) WHERE state='active';
ALTER TABLE registry_releases ENABLE ROW LEVEL SECURITY;
ALTER TABLE registry_releases FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON registry_releases;
CREATE POLICY tenant_isolation ON registry_releases USING (org_id = current_setting('app.org_id', true)) WITH CHECK (org_id = current_setting('app.org_id', true));
INSERT INTO workforce_migrations(version) VALUES ('006_registry') ON CONFLICT DO NOTHING;
