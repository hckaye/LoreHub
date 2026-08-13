ALTER TABLE ci_jobs
    ADD COLUMN execution_target varchar(16) NOT NULL DEFAULT 'managed',
    ADD COLUMN runner_id uuid REFERENCES ci_runners (id) ON DELETE SET NULL,
    ADD CONSTRAINT ci_jobs_execution_target_check CHECK (
        execution_target IN ('managed', 'self_hosted')
    );

ALTER TABLE ci_runs
    ADD COLUMN failure_reason varchar(64);

UPDATE ci_jobs
SET execution_target = 'managed'
WHERE status = 'queued';

CREATE INDEX ci_jobs_managed_claim_idx
    ON ci_jobs (queued_at)
    WHERE status = 'queued' AND execution_target = 'managed';

CREATE INDEX ci_jobs_self_hosted_claim_idx
    ON ci_jobs (queued_at)
    WHERE status = 'queued' AND execution_target = 'self_hosted';

CREATE INDEX ci_jobs_runner_lease_idx
    ON ci_jobs (runner_id, id)
    WHERE status = 'in_progress' AND runner_id IS NOT NULL;
