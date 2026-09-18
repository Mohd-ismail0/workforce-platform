-- Handoff lifecycle completeness: an offer can be accepted, declined,
-- clarified, withdrawn or expire. Previously only "accepted" was reachable, so
-- an unanswered offer sat forever and a recipient who had a question had no way
-- to ask it without accepting work they could not do.
--
-- expires_at bounds how long an unanswered offer stays actionable. It is set at
-- creation and enforced in SQL, so a parked offer cannot outlive its usefulness
-- merely because nobody opened the page.
ALTER TABLE handoffs ADD COLUMN IF NOT EXISTS expires_at timestamptz;
ALTER TABLE handoffs ADD COLUMN IF NOT EXISTS resolved_at timestamptz;
-- One free-text note column carries whichever note is current: the clarification
-- question, the decline reason or the withdrawal reason. History of who did what
-- lives in the state transition plus resolved_at; this column is the "why now".
ALTER TABLE handoffs ADD COLUMN IF NOT EXISTS reason text NOT NULL DEFAULT '';
-- Accountability ping-pong depth, recorded so a chain of transfers is visible
-- rather than infinite.
ALTER TABLE handoffs ADD COLUMN IF NOT EXISTS hop_depth integer NOT NULL DEFAULT 0;

-- 'clarification_requested' is a new reachable state.
ALTER TABLE handoffs DROP CONSTRAINT IF EXISTS handoffs_state_check;
ALTER TABLE handoffs ADD CONSTRAINT handoffs_state_check
  CHECK (state IN ('offered','accepted','declined','expired','cancelled','clarification_requested'));

-- Expiry sweeps read by (org, state, expires_at).
CREATE INDEX IF NOT EXISTS handoffs_expiry_idx ON handoffs (org_id, state, expires_at);

INSERT INTO workforce_migrations(version) VALUES ('013_handoff_lifecycle') ON CONFLICT DO NOTHING;