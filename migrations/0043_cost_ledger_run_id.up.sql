-- The run id on every ledger row, so one run of a job reads
-- back only its own model calls (a re-run is a second cost, not a sum).
-- Rows written before this migration keep run_id '' and match no run.
ALTER TABLE cost_ledger ADD COLUMN IF NOT EXISTS run_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS cost_ledger_job_run_action_idx ON cost_ledger (job_id, run_id, action);
