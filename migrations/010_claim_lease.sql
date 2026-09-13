ALTER TABLE agent_runs ADD COLUMN IF NOT EXISTS claim_expires_at timestamptz;
CREATE INDEX IF NOT EXISTS agent_runs_stranded_claims ON agent_runs(org_id, claim_expires_at) WHERE status = 'running';
INSERT INTO workforce_migrations(version) VALUES ('010_claim_lease') ON CONFLICT DO NOTHING;
