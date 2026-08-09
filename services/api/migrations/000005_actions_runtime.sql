ALTER TABLE ci_workflows
    ADD COLUMN IF NOT EXISTS name varchar(255) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS state varchar(24) NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS error_code varchar(80),
    ADD COLUMN IF NOT EXISTS error_message text,
    ADD COLUMN IF NOT EXISTS trigger_config jsonb NOT NULL DEFAULT '{}';

ALTER TABLE ci_workflows
    DROP CONSTRAINT IF EXISTS ci_workflows_state_check;

UPDATE ci_workflows
SET state = CASE WHEN enabled THEN 'active' ELSE 'disabled' END
WHERE state = 'active' AND NOT enabled;

ALTER TABLE ci_workflows
    ADD CONSTRAINT ci_workflows_state_check CHECK (state IN ('active', 'disabled', 'error'));

ALTER TABLE ci_runs
    ADD COLUMN IF NOT EXISTS run_attempt integer NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS rerun_of uuid REFERENCES ci_runs (id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS cancel_requested boolean NOT NULL DEFAULT false;

ALTER TABLE ci_runs
    DROP CONSTRAINT IF EXISTS ci_runs_run_attempt_check;

ALTER TABLE ci_runs
    ADD CONSTRAINT ci_runs_run_attempt_check CHECK (run_attempt > 0);

CREATE INDEX IF NOT EXISTS ci_runs_repository_workflow_idx
    ON ci_runs (repository_id, workflow_id, run_number DESC);

CREATE INDEX IF NOT EXISTS ci_jobs_run_status_idx
    ON ci_jobs (run_id, status);

CREATE INDEX IF NOT EXISTS ci_artifacts_job_created_idx
    ON ci_artifacts (job_id, created_at);
