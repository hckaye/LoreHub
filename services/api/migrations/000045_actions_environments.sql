CREATE TABLE repository_environments (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    name varchar(128) NOT NULL,
    wait_timer_minutes integer NOT NULL DEFAULT 0,
    prevent_self_review boolean NOT NULL DEFAULT true,
    active boolean NOT NULL DEFAULT true,
    created_by uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT repository_environments_id_repository_unique UNIQUE (id, repository_id),
    CONSTRAINT repository_environments_name_check CHECK (
        char_length(name) BETWEEN 1 AND 128
        AND name = btrim(name)
        AND name !~ '[[:cntrl:]/\\]'
    ),
    CONSTRAINT repository_environments_wait_timer_check
        CHECK (wait_timer_minutes BETWEEN 0 AND 43200)
);

CREATE UNIQUE INDEX repository_environments_repository_name_idx
    ON repository_environments (repository_id, lower(name));

CREATE TABLE repository_environment_reviewers (
    environment_id uuid NOT NULL REFERENCES repository_environments (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (environment_id, user_id)
);

CREATE INDEX repository_environment_reviewers_user_idx
    ON repository_environment_reviewers (user_id, environment_id);

CREATE UNIQUE INDEX ci_runs_id_repository_unique_idx
    ON ci_runs (id, repository_id);

CREATE UNIQUE INDEX ci_jobs_id_run_unique_idx
    ON ci_jobs (id, run_id);

CREATE TABLE deployments (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL,
    environment_id uuid NOT NULL,
    environment_name varchar(128) NOT NULL,
    run_id uuid NOT NULL,
    job_id uuid NOT NULL,
    actor_id uuid REFERENCES users (id) ON DELETE SET NULL,
    branch varchar(255) NOT NULL,
    revision varchar(128) NOT NULL,
    status varchar(24) NOT NULL,
    wait_until timestamptz NOT NULL,
    reviewed_by uuid REFERENCES users (id) ON DELETE SET NULL,
    reviewed_at timestamptz,
    started_at timestamptz,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT deployments_environment_boundary_fk
        FOREIGN KEY (environment_id, repository_id)
        REFERENCES repository_environments (id, repository_id) ON DELETE RESTRICT,
    CONSTRAINT deployments_run_boundary_fk
        FOREIGN KEY (run_id, repository_id)
        REFERENCES ci_runs (id, repository_id) ON DELETE CASCADE,
    CONSTRAINT deployments_job_boundary_fk
        FOREIGN KEY (job_id, run_id)
        REFERENCES ci_jobs (id, run_id) ON DELETE CASCADE,
    CONSTRAINT deployments_job_unique UNIQUE (job_id),
    CONSTRAINT deployments_status_check CHECK (
        status IN ('pending', 'waiting', 'queued', 'in_progress', 'success', 'failure', 'cancelled', 'rejected')
    ),
    CONSTRAINT deployments_environment_name_check CHECK (
        char_length(environment_name) BETWEEN 1 AND 128
        AND environment_name = btrim(environment_name)
        AND environment_name !~ '[[:cntrl:]/\\]'
    ),
    CONSTRAINT deployments_review_check CHECK (
		reviewed_by IS NULL OR reviewed_at IS NOT NULL
    ),
    CONSTRAINT deployments_completion_check CHECK (
        (status IN ('success', 'failure', 'cancelled', 'rejected') AND completed_at IS NOT NULL)
        OR (status NOT IN ('success', 'failure', 'cancelled', 'rejected') AND completed_at IS NULL)
    )
);

CREATE INDEX deployments_repository_created_idx
    ON deployments (repository_id, created_at DESC, id DESC);

CREATE INDEX deployments_claim_idx
    ON deployments (wait_until, created_at)
    WHERE status IN ('waiting', 'queued');

UPDATE repository_branch_states
SET workflow_observed_revision = NULL;
