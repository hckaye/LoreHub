-- Actions security and exact-revision state. 000006 is reserved for Lore merge
-- operations and 000007 for the control-plane schema.

ALTER TABLE organizations
    ADD COLUMN IF NOT EXISTS active boolean NOT NULL DEFAULT true;

ALTER TABLE organization_memberships
    ADD COLUMN IF NOT EXISTS active boolean NOT NULL DEFAULT true;

ALTER TABLE repository_memberships
    ADD COLUMN IF NOT EXISTS active boolean NOT NULL DEFAULT true;

ALTER TABLE repository_memberships
    DROP CONSTRAINT IF EXISTS repository_memberships_role_check;

ALTER TABLE repository_memberships
    ADD CONSTRAINT repository_memberships_role_check
    CHECK (role IN ('admin', 'maintain', 'write', 'triage', 'read'));

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_status_check;

ALTER TABLE users
    ADD CONSTRAINT users_status_check
    CHECK (status IN ('active', 'suspended', 'inactive'));

CREATE TABLE IF NOT EXISTS teams (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    slug varchar(64) NOT NULL,
    display_name varchar(160) NOT NULL,
    description text NOT NULL DEFAULT '',
    created_by uuid NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    active boolean NOT NULL DEFAULT true,
    CONSTRAINT teams_organization_slug_unique UNIQUE (organization_id, slug)
);

CREATE TABLE IF NOT EXISTS team_memberships (
    team_id uuid NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role varchar(16) NOT NULL DEFAULT 'member',
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, user_id)
);

ALTER TABLE team_memberships
    DROP CONSTRAINT IF EXISTS team_memberships_role_check;

ALTER TABLE team_memberships
    ADD CONSTRAINT team_memberships_role_check
    CHECK (role IN ('maintain', 'maintainer', 'member'));

CREATE TABLE IF NOT EXISTS team_repository_roles (
    team_id uuid NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    role varchar(16) NOT NULL DEFAULT 'read',
    created_by uuid NOT NULL REFERENCES users (id),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, repository_id)
);

ALTER TABLE teams
    ADD COLUMN IF NOT EXISTS active boolean NOT NULL DEFAULT true;

ALTER TABLE team_repository_roles
    ADD COLUMN IF NOT EXISTS active boolean NOT NULL DEFAULT true;

ALTER TABLE team_repository_roles
    DROP CONSTRAINT IF EXISTS team_repository_roles_role_check;

ALTER TABLE team_repository_roles
    ADD CONSTRAINT team_repository_roles_role_check
    CHECK (role IN ('admin', 'maintain', 'write', 'triage', 'read'));

CREATE INDEX IF NOT EXISTS team_memberships_user_active_idx
    ON team_memberships (user_id, team_id)
    WHERE active;

CREATE INDEX IF NOT EXISTS team_repository_roles_repository_active_idx
    ON team_repository_roles (repository_id, team_id)
    WHERE active;

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
