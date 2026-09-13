CREATE TABLE IF NOT EXISTS business_operations (
 id text PRIMARY KEY, org_id text NOT NULL REFERENCES organizations(id), integration text NOT NULL, action text NOT NULL,
 business_key text NOT NULL CHECK (length(business_key) BETWEEN 1 AND 200), fingerprint text NOT NULL, proposal_id text,
 status text NOT NULL DEFAULT 'reserved' CHECK (status IN ('reserved','dispatching','verified','released','unknown')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(org_id,id), UNIQUE(org_id,integration,action,business_key), FOREIGN KEY(org_id,proposal_id) REFERENCES proposals(org_id,id)
);
ALTER TABLE proposals ADD COLUMN IF NOT EXISTS supersedes_proposal_id text;
ALTER TABLE proposals ADD CONSTRAINT proposals_supersedes_fk FOREIGN KEY (org_id,supersedes_proposal_id) REFERENCES proposals(org_id,id) NOT VALID;
ALTER TABLE effects ADD COLUMN IF NOT EXISTS business_operation_id text;
ALTER TABLE effects ADD CONSTRAINT effects_business_operation_fk FOREIGN KEY (org_id,business_operation_id) REFERENCES business_operations(org_id,id) NOT VALID;
CREATE UNIQUE INDEX IF NOT EXISTS effects_business_operation_operation_uq ON effects(org_id,business_operation_id,operation_id) WHERE business_operation_id IS NOT NULL;
ALTER TABLE business_operations ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON business_operations;
CREATE POLICY tenant_isolation ON business_operations USING (org_id = current_setting('app.org_id', true)) WITH CHECK (org_id = current_setting('app.org_id', true));
INSERT INTO workforce_migrations(version) VALUES ('007_business_operations') ON CONFLICT DO NOTHING;
