DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'ci_runs_id_repository_unique'
    ) THEN
        ALTER TABLE ci_runs
            ADD CONSTRAINT ci_runs_id_repository_unique UNIQUE (id, repository_id);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'ci_jobs_id_run_unique'
    ) THEN
        ALTER TABLE ci_jobs
            ADD CONSTRAINT ci_jobs_id_run_unique UNIQUE (id, run_id);
    END IF;
END $$;

CREATE TABLE ci_sarif_uploads (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    run_id uuid NOT NULL,
    job_id uuid NOT NULL,
    attempt integer NOT NULL,
    tools jsonb NOT NULL,
    revision varchar(128) NOT NULL,
    ref varchar(255) NOT NULL,
    sarif_version varchar(16) NOT NULL,
    document_size integer NOT NULL,
    results_count integer NOT NULL,
    sarif jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ci_sarif_uploads_id_repository_unique UNIQUE (id, repository_id),
    CONSTRAINT ci_sarif_uploads_run_repository_fk
        FOREIGN KEY (run_id, repository_id)
        REFERENCES ci_runs (id, repository_id) ON DELETE CASCADE,
    CONSTRAINT ci_sarif_uploads_job_run_fk
        FOREIGN KEY (job_id, run_id)
        REFERENCES ci_jobs (id, run_id) ON DELETE CASCADE,
    CONSTRAINT ci_sarif_uploads_attempt_check CHECK (attempt > 0),
    CONSTRAINT ci_sarif_uploads_tools_check CHECK (jsonb_typeof(tools) = 'array'),
    CONSTRAINT ci_sarif_uploads_version_check CHECK (sarif_version = '2.1.0'),
    CONSTRAINT ci_sarif_uploads_document_size_check CHECK (document_size BETWEEN 1 AND 10485760),
    CONSTRAINT ci_sarif_uploads_results_count_check CHECK (results_count BETWEEN 0 AND 50000)
);

CREATE INDEX ci_sarif_uploads_repository_created_idx
    ON ci_sarif_uploads (repository_id, created_at DESC, id DESC);

CREATE INDEX ci_sarif_uploads_job_attempt_idx
    ON ci_sarif_uploads (job_id, attempt, created_at DESC);

CREATE TABLE ci_code_scanning_alerts (
    id uuid PRIMARY KEY,
    upload_id uuid NOT NULL,
    repository_id uuid NOT NULL,
    tool_name varchar(255) NOT NULL,
    rule_id varchar(512) NOT NULL,
    level varchar(16) NOT NULL,
    message text NOT NULL,
    path varchar(1024) NOT NULL,
    start_line integer,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ci_code_scanning_alerts_upload_repository_fk
        FOREIGN KEY (upload_id, repository_id)
        REFERENCES ci_sarif_uploads (id, repository_id) ON DELETE CASCADE,
    CONSTRAINT ci_code_scanning_alerts_level_check
        CHECK (level IN ('none', 'note', 'warning', 'error')),
    CONSTRAINT ci_code_scanning_alerts_start_line_check CHECK (start_line IS NULL OR start_line > 0)
);

CREATE INDEX ci_code_scanning_alerts_repository_created_idx
    ON ci_code_scanning_alerts (repository_id, created_at DESC, id DESC);

CREATE INDEX ci_code_scanning_alerts_upload_idx
    ON ci_code_scanning_alerts (upload_id, id);
