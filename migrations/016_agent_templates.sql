-- Agent templates: a validated, versioned, DECLARATIVE starting point.
--
-- A template is what an employee picks instead of configuring an agent from
-- nothing. It is data, not executable configuration: it names a harness, the
-- capabilities agents created from it may use, and optional instructions. It
-- cannot run anything, and it cannot widen a ceiling.
--
-- Versioning matters because agents are created FROM a specific version and stay
-- pinned to it. A template update is a new row, so an existing agent's effective
-- configuration does not silently change underneath work that is already parked
-- or in flight; adopting a new version is an explicit act.
CREATE TABLE IF NOT EXISTS agent_templates (
  id text PRIMARY KEY,
  org_id text NOT NULL REFERENCES organizations(id),
  name text NOT NULL,
  version text NOT NULL,
  harness text NOT NULL,
  description text NOT NULL DEFAULT '',
  -- The scope of customization: an agent created from this template may declare
  -- a SUBSET of these, never more.
  capabilities jsonb NOT NULL DEFAULT '[]',
  instructions text NOT NULL DEFAULT '',
  -- Draft templates are not selectable. Publishing is a deliberate act, so a
  -- half-written template cannot be handed to someone by accident.
  status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','retired')),
  created_by text NOT NULL,
  revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  UNIQUE(org_id,id),
  -- A version is immutable once published; revising means a new version.
  UNIQUE(org_id,name,version),
  FOREIGN KEY(org_id,created_by) REFERENCES principals(org_id,id)
);

-- An agent records which template version it came from, so its configuration is
-- attributable rather than merely similar.
ALTER TABLE agents ADD COLUMN IF NOT EXISTS template_id text;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS template_version text;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS instructions text NOT NULL DEFAULT '';
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='agents_template_fk') THEN
    ALTER TABLE agents ADD CONSTRAINT agents_template_fk
      FOREIGN KEY (org_id, template_id) REFERENCES agent_templates(org_id, id);
  END IF;
END $$;

CREATE INDEX IF NOT EXISTS agent_templates_org_idx ON agent_templates (org_id, status, name);

ALTER TABLE agent_templates ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_templates FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON agent_templates;
CREATE POLICY tenant_isolation ON agent_templates
  USING (org_id = current_setting('app.org_id', true))
  WITH CHECK (org_id = current_setting('app.org_id', true));

INSERT INTO workforce_migrations(version) VALUES ('016_agent_templates') ON CONFLICT DO NOTHING;