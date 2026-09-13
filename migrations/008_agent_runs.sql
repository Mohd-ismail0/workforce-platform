CREATE TABLE IF NOT EXISTS agent_runs (
 id text primary key, org_id text not null references organizations(id), task_id text not null, agent_id text not null,
 harness text not null, harness_release_id text, runner_id text not null, intent text not null default '',
 status text not null default 'queued' check (status in ('queued','running','succeeded','failed','cancelled')),
 proposal_id text, result_summary text not null default '', failure_reason text not null default '', notes jsonb not null default '[]',
 created_by text not null, version bigint not null default 1, created_at timestamptz not null default clock_timestamp(), started_at timestamptz, finished_at timestamptz,
 UNIQUE(org_id,id), FOREIGN KEY(org_id,task_id) REFERENCES tasks(org_id,id), FOREIGN KEY(org_id,agent_id) REFERENCES agents(org_id,id), FOREIGN KEY(org_id,created_by) REFERENCES principals(org_id,id)
);
ALTER TABLE agent_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_runs FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON agent_runs;
CREATE POLICY tenant_isolation ON agent_runs USING (org_id = current_setting('app.org_id', true)) WITH CHECK (org_id = current_setting('app.org_id', true));
INSERT INTO workforce_migrations(version) VALUES ('008_agent_runs') ON CONFLICT DO NOTHING;
