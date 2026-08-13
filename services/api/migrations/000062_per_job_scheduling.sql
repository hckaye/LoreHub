ALTER TABLE ci_jobs
    ADD COLUMN job_name varchar NOT NULL DEFAULT '',
    ADD COLUMN needs jsonb NOT NULL DEFAULT '[]';

ALTER TABLE ci_jobs
    DROP CONSTRAINT ci_jobs_run_name_attempt_unique,
    ADD CONSTRAINT ci_jobs_run_job_name_attempt_unique UNIQUE (run_id, job_name, attempt);
