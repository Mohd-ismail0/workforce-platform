CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE IF NOT EXISTS organizations (
 id text PRIMARY KEY, name text NOT NULL, created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE IF NOT EXISTS principals (
 id text PRIMARY KEY, org_id text NOT NULL REFERENCES organizations(id), name text NOT NULL,
 role text NOT NULL CHECK (role IN ('requester','approver','admin')), active boolean NOT NULL DEFAULT true,
 UNIQUE(org_id,id)
);
CREATE TABLE IF NOT EXISTS projects (
 id text PRIMARY KEY, org_id text NOT NULL REFERENCES organizations(id), name text NOT NULL, description text NOT NULL DEFAULT '',
 version bigint NOT NULL DEFAULT 1, created_at timestamptz NOT NULL DEFAULT clock_timestamp(), UNIQUE(org_id,id)
);
CREATE TABLE IF NOT EXISTS tasks (
 id text PRIMARY KEY, org_id text NOT NULL REFERENCES organizations(id), project_id text REFERENCES projects(id), title text NOT NULL,
 description text NOT NULL DEFAULT '', status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','ready','in_progress','in_review','blocked','done','cancelled')),
 owner_id text NOT NULL, assignee_id text, version bigint NOT NULL DEFAULT 1, created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(org_id,id), FOREIGN KEY(org_id,owner_id) REFERENCES principals(org_id,id), FOREIGN KEY(org_id,assignee_id) REFERENCES principals(org_id,id)
);
CREATE TABLE IF NOT EXISTS task_dependencies (
 org_id text NOT NULL REFERENCES organizations(id), task_id text NOT NULL, parent_id text NOT NULL, created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(org_id,task_id,parent_id), CHECK(task_id<>parent_id), FOREIGN KEY(org_id,task_id) REFERENCES tasks(org_id,id), FOREIGN KEY(org_id,parent_id) REFERENCES tasks(org_id,id)
);
CREATE TABLE IF NOT EXISTS agents (
 id text PRIMARY KEY, org_id text NOT NULL REFERENCES organizations(id), name text NOT NULL, harness text NOT NULL,
 status text NOT NULL DEFAULT 'draft', owner_id text NOT NULL, capabilities jsonb NOT NULL DEFAULT '[]', version bigint NOT NULL DEFAULT 1,
 UNIQUE(org_id,id), FOREIGN KEY(org_id,owner_id) REFERENCES principals(org_id,id)
);
CREATE TABLE IF NOT EXISTS simulator_records (
 id text PRIMARY KEY, org_id text NOT NULL REFERENCES organizations(id), integration text NOT NULL, version bigint NOT NULL DEFAULT 1,
 data jsonb NOT NULL DEFAULT '{}', UNIQUE(org_id,id)
);
CREATE TABLE IF NOT EXISTS proposals (
 id text PRIMARY KEY, org_id text NOT NULL REFERENCES organizations(id), task_id text NOT NULL, revision bigint NOT NULL,
 digest text NOT NULL, status text NOT NULL CHECK(status IN ('pending_endorsement','pending_approval','approved','rejected','superseded','executing','done','needs_attention')),
 summary text NOT NULL, operations jsonb NOT NULL, created_by text NOT NULL, created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(org_id,id), UNIQUE(org_id,task_id,revision), FOREIGN KEY(org_id,task_id) REFERENCES tasks(org_id,id), FOREIGN KEY(org_id,created_by) REFERENCES principals(org_id,id)
);
CREATE TABLE IF NOT EXISTS proposal_decisions (
 id text PRIMARY KEY, org_id text NOT NULL REFERENCES organizations(id), proposal_id text NOT NULL, actor_id text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('endorse','approve','reject')), revision bigint NOT NULL, digest text NOT NULL, reason text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), UNIQUE(org_id,proposal_id,actor_id,kind), FOREIGN KEY(org_id,proposal_id) REFERENCES proposals(org_id,id), FOREIGN KEY(org_id,actor_id) REFERENCES principals(org_id,id)
);
CREATE TABLE IF NOT EXISTS effects (
 id text PRIMARY KEY, org_id text NOT NULL REFERENCES organizations(id), proposal_id text NOT NULL, operation_id text NOT NULL,
 integration text NOT NULL, action text NOT NULL, target_id text NOT NULL, expected_version bigint NOT NULL, payload jsonb NOT NULL,
 state text NOT NULL DEFAULT 'prepared' CHECK(state IN ('prepared','authorized','dispatching','accepted','applied','verified','unknown','failed_no_effect','needs_attention')),
 result jsonb, created_at timestamptz NOT NULL DEFAULT clock_timestamp(), UNIQUE(org_id,proposal_id,operation_id), UNIQUE(org_id,id), FOREIGN KEY(org_id,proposal_id) REFERENCES proposals(org_id,id)
);
CREATE TABLE IF NOT EXISTS receipts (
 id text PRIMARY KEY, org_id text NOT NULL REFERENCES organizations(id), effect_id text NOT NULL, kind text NOT NULL, outcome_code text NOT NULL,
 result jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT clock_timestamp(), UNIQUE(org_id,id), FOREIGN KEY(org_id,effect_id) REFERENCES effects(org_id,id)
);
CREATE TABLE IF NOT EXISTS domain_events (
 id text PRIMARY KEY, org_id text NOT NULL REFERENCES organizations(id), aggregate_id text NOT NULL, aggregate_version bigint NOT NULL,
 event_type text NOT NULL, actor_id text, payload jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(org_id,aggregate_id,aggregate_version,event_type)
);
CREATE TABLE IF NOT EXISTS handoffs (
 id text PRIMARY KEY, org_id text NOT NULL REFERENCES organizations(id), task_id text NOT NULL, offered_task_version bigint NOT NULL,
 recipient_id text NOT NULL, role text NOT NULL, summary text NOT NULL, state text NOT NULL DEFAULT 'offered' CHECK(state IN ('offered','accepted','declined','expired','cancelled')),
 created_by text NOT NULL, accepted_at timestamptz, UNIQUE(org_id,id), FOREIGN KEY(org_id,task_id) REFERENCES tasks(org_id,id), FOREIGN KEY(org_id,recipient_id) REFERENCES principals(org_id,id), FOREIGN KEY(org_id,created_by) REFERENCES principals(org_id,id)
);

CREATE TABLE IF NOT EXISTS workforce_migrations (version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT clock_timestamp());
INSERT INTO workforce_migrations(version) VALUES ('001_initial') ON CONFLICT DO NOTHING;
DO $$ DECLARE t text; BEGIN FOREACH t IN ARRAY ARRAY['principals','projects','tasks','task_dependencies','agents','simulator_records','proposals','proposal_decisions','effects','receipts','domain_events','handoffs'] LOOP EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t); EXECUTE format('DROP POLICY IF EXISTS tenant_isolation ON %I',t); EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (org_id = current_setting(''app.org_id'', true)) WITH CHECK (org_id = current_setting(''app.org_id'', true))',t); END LOOP; END $$;
