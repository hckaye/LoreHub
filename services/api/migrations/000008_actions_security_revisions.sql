-- Exact-revision state for Actions. Access lifecycle and team authorization
-- are owned by 000007 and must not be duplicated in CI migrations.

CREATE TABLE IF NOT EXISTS ci_workflow_revisions (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    revision varchar(128) NOT NULL,
    path text NOT NULL,
    name varchar(255) NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    state varchar(24) NOT NULL DEFAULT 'active',
    error_code varchar(80),
    error_message text,
    trigger_config jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ci_workflow_revisions_repository_revision_path_unique
        UNIQUE (repository_id, revision, path),
    CONSTRAINT ci_workflow_revisions_state_check
        CHECK (state IN ('active', 'disabled', 'error'))
);

ALTER TABLE ci_runs
    ADD COLUMN IF NOT EXISTS workflow_revision_id uuid
        REFERENCES ci_workflow_revisions (id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS ci_runs_repository_revision_workflow_idx
    ON ci_runs (repository_id, workflow_revision_id, run_number DESC);

CREATE INDEX IF NOT EXISTS ci_workflow_revisions_repository_revision_idx
    ON ci_workflow_revisions (repository_id, revision, path);

CREATE TABLE IF NOT EXISTS ci_schedule_occurrences (
    workflow_id uuid NOT NULL REFERENCES ci_workflows (id) ON DELETE CASCADE,
    schedule_key varchar(255) NOT NULL,
    occurrence_at timestamptz NOT NULL,
    run_id uuid NOT NULL REFERENCES ci_runs (id) ON DELETE CASCADE,
    PRIMARY KEY (workflow_id, schedule_key, occurrence_at),
    CONSTRAINT ci_schedule_occurrences_run_unique UNIQUE (run_id)
);
