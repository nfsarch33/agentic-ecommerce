DROP INDEX IF EXISTS cost_ledger_job_run_action_idx;
ALTER TABLE cost_ledger DROP COLUMN IF EXISTS run_id;
