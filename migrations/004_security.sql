-- Security hardening: tenant-safe project references and forced RLS.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='tasks_project_org_fk') THEN
    ALTER TABLE tasks ADD CONSTRAINT tasks_project_org_fk FOREIGN KEY (org_id, project_id) REFERENCES projects(org_id, id);
  END IF;
END $$;
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY['principals','projects','tasks','task_dependencies','agents','simulator_records','proposals','proposal_decisions','effects','receipts','domain_events','handoffs'] LOOP
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
  END LOOP;
END $$;
INSERT INTO workforce_migrations(version) VALUES ('004_security') ON CONFLICT DO NOTHING;
