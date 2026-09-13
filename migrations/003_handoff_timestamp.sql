ALTER TABLE handoffs ADD COLUMN IF NOT EXISTS created_at timestamptz NOT NULL DEFAULT clock_timestamp();
INSERT INTO workforce_migrations(version) VALUES ('003_handoff_timestamp') ON CONFLICT DO NOTHING;
