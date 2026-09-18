-- Effective-dated organization structure: positions and principal relationships.
--
-- A reporting line is a fact with a validity period, not a mutable column. An
-- employee who changes manager, joins a matrix team for six weeks, or covers for
-- a colleague on leave must be representable as intervals, or history is silently
-- rewritten every time someone's situation changes and "who reported to whom in
-- March?" becomes unanswerable.
--
-- Intervals are half-open [from, to): a period ending at T and the next beginning
-- at T do not overlap and do not leave a gap. This is the standard choice, and it
-- is enforced by the database rather than by convention.
--
-- Overlap prevention uses a GiST exclusion constraint, which needs btree_gist on
-- the equality columns; btree_gist is installed by migration 001.
CREATE TABLE IF NOT EXISTS positions (
  id text PRIMARY KEY,
  org_id text NOT NULL REFERENCES organizations(id),
  name text NOT NULL,
  -- Self-referential structure. A position with no holder is a vacancy, which is
  -- a first-class state rather than a missing row.
  parent_id text,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  UNIQUE(org_id,id),
  FOREIGN KEY(org_id,parent_id) REFERENCES positions(org_id,id)
);

CREATE TABLE IF NOT EXISTS org_relationships (
  id text PRIMARY KEY,
  org_id text NOT NULL REFERENCES organizations(id),
  -- The principal the relationship belongs to.
  subject_id text NOT NULL,
  -- reports_to / covers_for name a PRINCIPAL; member_of names a POSITION. The
  -- target is therefore checked per kind rather than by a single foreign key.
  kind text NOT NULL CHECK (kind IN ('reports_to','member_of','covers_for')),
  object_id text NOT NULL,
  -- Validity in the real world. NULL upper bound means still in effect.
  valid tstzrange NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  created_by text NOT NULL,
  UNIQUE(org_id,id),
  FOREIGN KEY(org_id,subject_id) REFERENCES principals(org_id,id),
  FOREIGN KEY(org_id,created_by) REFERENCES principals(org_id,id),
  -- An interval must actually be an interval.
  CONSTRAINT org_relationships_valid_not_empty CHECK (NOT isempty(valid))
);

-- At most ONE manager at a time. Two concurrent reports_to intervals for the same
-- person are a contradiction, not a feature: the chain would be ambiguous and
-- "who approves this" would depend on row order.
--
-- Matrix membership and cover deliberately have NO such constraint: a person is
-- expected to belong to several teams at once, and to cover for several
-- colleagues simultaneously. Constraining them the same way would make the
-- matrix unrepresentable.
ALTER TABLE org_relationships DROP CONSTRAINT IF EXISTS org_relationships_one_manager;
ALTER TABLE org_relationships ADD CONSTRAINT org_relationships_one_manager
  EXCLUDE USING gist (org_id WITH =, subject_id WITH =, valid WITH &&)
  WHERE (kind = 'reports_to');

CREATE INDEX IF NOT EXISTS org_relationships_asof_idx ON org_relationships (org_id, kind, subject_id);
CREATE INDEX IF NOT EXISTS org_relationships_valid_idx ON org_relationships USING gist (valid);
-- Positions are part of the tenant boundary like every other table.
ALTER TABLE positions FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON positions;
CREATE POLICY tenant_isolation ON positions
  USING (org_id = current_setting('app.org_id', true))
  WITH CHECK (org_id = current_setting('app.org_id', true));
DO $$ BEGIN
  ALTER TABLE org_relationships ENABLE ROW LEVEL SECURITY;
  ALTER TABLE org_relationships FORCE ROW LEVEL SECURITY;
END $$;
DROP POLICY IF EXISTS tenant_isolation ON org_relationships;
CREATE POLICY tenant_isolation ON org_relationships
  USING (org_id = current_setting('app.org_id', true))
  WITH CHECK (org_id = current_setting('app.org_id', true));

INSERT INTO workforce_migrations(version) VALUES ('014_org_temporal') ON CONFLICT DO NOTHING;