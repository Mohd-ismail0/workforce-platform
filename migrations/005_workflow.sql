ALTER TABLE proposals ADD COLUMN IF NOT EXISTS failure_reason text NOT NULL DEFAULT '';
INSERT INTO workforce_migrations(version) VALUES ('005_workflow') ON CONFLICT DO NOTHING;
