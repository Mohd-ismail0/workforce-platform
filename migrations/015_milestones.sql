-- Milestones and milestone dependencies: planning that is separate from doing.
--
-- A milestone is not a column on a task and not a "Done" column. It is a stated
-- outcome with declared ACCEPTANCE EVIDENCE, so that "met" means something
-- checkable rather than "everything under it happened to move".
--
-- forecast_date and committed_date are deliberately two columns. A forecast is a
-- guess that should be revised freely; a commitment is a promise that should be
-- visible when it slips. Collapsing them into one date makes a promise
-- indistinguishable from a guess, which is exactly how a plan loses its meaning.
CREATE TABLE IF NOT EXISTS milestones (
  id text PRIMARY KEY,
  org_id text NOT NULL REFERENCES organizations(id),
  project_id text NOT NULL,
  name text NOT NULL,
  -- What must be true for this to count as met. NOT optional and NOT defaulted:
  -- a milestone with no stated evidence can only ever be "completed" by
  -- assertion, which is the failure mode this table exists to prevent.
  acceptance_evidence text NOT NULL,
  committed_date date,
  forecast_date date,
  status text NOT NULL DEFAULT 'planned' CHECK (status IN ('planned','active','met','cancelled')),
  met_at timestamptz,
  -- The evidence actually recorded at completion, kept separate from the
  -- declaration so the two can be compared.
  met_evidence text NOT NULL DEFAULT '',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  UNIQUE(org_id,id),
  FOREIGN KEY(org_id,project_id) REFERENCES projects(org_id,id),
  -- A milestone that claims to be met must say on what evidence.
  CONSTRAINT milestones_met_requires_evidence CHECK (status <> 'met' OR met_evidence <> '')
);

-- Milestone-to-milestone prerequisites. Cycles are refused at write time.
CREATE TABLE IF NOT EXISTS milestone_dependencies (
  org_id text NOT NULL REFERENCES organizations(id),
  milestone_id text NOT NULL,
  parent_id text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(org_id, milestone_id, parent_id),
  CHECK (milestone_id <> parent_id),
  FOREIGN KEY(org_id,milestone_id) REFERENCES milestones(org_id,id),
  FOREIGN KEY(org_id,parent_id) REFERENCES milestones(org_id,id)
);

-- A task may belong to a milestone. Optional: planning a milestone does not
-- require having invented its tasks yet.
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS milestone_id text;
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='tasks_milestone_fk') THEN
    ALTER TABLE tasks ADD CONSTRAINT tasks_milestone_fk
      FOREIGN KEY (org_id, milestone_id) REFERENCES milestones(org_id, id);
  END IF;
END $$;

CREATE INDEX IF NOT EXISTS milestones_project_idx ON milestones (org_id, project_id, status);
CREATE INDEX IF NOT EXISTS tasks_milestone_idx ON tasks (org_id, milestone_id);

ALTER TABLE milestones ENABLE ROW LEVEL SECURITY;
ALTER TABLE milestones FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON milestones;
CREATE POLICY tenant_isolation ON milestones
  USING (org_id = current_setting('app.org_id', true))
  WITH CHECK (org_id = current_setting('app.org_id', true));

ALTER TABLE milestone_dependencies ENABLE ROW LEVEL SECURITY;
ALTER TABLE milestone_dependencies FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON milestone_dependencies;
CREATE POLICY tenant_isolation ON milestone_dependencies
  USING (org_id = current_setting('app.org_id', true))
  WITH CHECK (org_id = current_setting('app.org_id', true));

INSERT INTO workforce_migrations(version) VALUES ('015_milestones') ON CONFLICT DO NOTHING;