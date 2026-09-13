ALTER TABLE agent_runs DROP CONSTRAINT IF EXISTS agent_runs_status_check;
ALTER TABLE agent_runs ADD CONSTRAINT agent_runs_status_check CHECK (status IN ('queued','running','waiting','succeeded','failed','cancelled'));
ALTER TABLE agent_runs ADD COLUMN IF NOT EXISTS claim_token text NOT NULL DEFAULT '';
ALTER TABLE agent_runs ADD COLUMN IF NOT EXISTS attempt int NOT NULL DEFAULT 0;
ALTER TABLE agent_runs ADD COLUMN IF NOT EXISTS gate_id text;
ALTER TABLE agent_runs ADD COLUMN IF NOT EXISTS continuation jsonb NOT NULL DEFAULT '[]';
ALTER TABLE proposals ADD COLUMN IF NOT EXISTS origin text NOT NULL DEFAULT 'human';
ALTER TABLE proposals ADD COLUMN IF NOT EXISTS origin_run_id text;
CREATE TABLE IF NOT EXISTS decision_gates (
 id text primary key, org_id text not null references organizations(id), task_id text not null, run_id text,
 kind text not null check (kind in ('clarification','selection','missing_information')), prompt text not null,
 input_schema jsonb not null default '{}', revision bigint not null default 1,
 status text not null default 'pending' check (status in ('pending','resolved','cancelled','expired')),
 respondent_id text not null, response jsonb, responded_by text, responded_at timestamptz, expires_at timestamptz,
 created_by text not null, created_at timestamptz not null default clock_timestamp(), version bigint not null default 1,
 UNIQUE(org_id,id), FOREIGN KEY(org_id,task_id) REFERENCES tasks(org_id,id),
 FOREIGN KEY(org_id,respondent_id) REFERENCES principals(org_id,id), FOREIGN KEY(org_id,created_by) REFERENCES principals(org_id,id)
);
CREATE UNIQUE INDEX IF NOT EXISTS decision_gates_one_pending_per_run ON decision_gates(org_id,run_id) WHERE run_id IS NOT NULL AND status='pending';
CREATE INDEX IF NOT EXISTS decision_gates_respondent ON decision_gates(org_id,respondent_id,created_at DESC);
ALTER TABLE decision_gates ENABLE ROW LEVEL SECURITY;
ALTER TABLE decision_gates FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON decision_gates;
CREATE POLICY tenant_isolation ON decision_gates USING (org_id = current_setting('app.org_id', true)) WITH CHECK (org_id = current_setting('app.org_id', true));
INSERT INTO workforce_migrations(version) VALUES ('009_pause_resume') ON CONFLICT DO NOTHING;
