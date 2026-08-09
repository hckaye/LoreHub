CREATE TABLE ci_artifacts (
    id uuid PRIMARY KEY,
    job_id uuid NOT NULL REFERENCES ci_jobs (id) ON DELETE CASCADE,
    name text NOT NULL,
    object_key text NOT NULL,
    size_bytes bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ci_artifacts_job_object_unique UNIQUE (job_id, object_key),
    CONSTRAINT ci_artifacts_size_check CHECK (size_bytes >= 0)
);
