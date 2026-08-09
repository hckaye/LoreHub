-- LoreHub control-plane authorization. 000007 is reserved for this boundary.
-- repositories.lore_repository_id is the one canonical 32-hex Lore partition ID.

ALTER TABLE organizations
    ADD COLUMN IF NOT EXISTS active boolean NOT NULL DEFAULT true;

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_status_check;

ALTER TABLE users ADD CONSTRAINT users_status_check
    CHECK (status IN ('active', 'suspended', 'inactive'));

ALTER TABLE organization_memberships
    ADD COLUMN IF NOT EXISTS active boolean NOT NULL DEFAULT true;

ALTER TABLE repository_memberships
    ADD COLUMN IF NOT EXISTS active boolean NOT NULL DEFAULT true;

ALTER TABLE repository_memberships
    DROP CONSTRAINT IF EXISTS repository_memberships_role_check;

ALTER TABLE repository_memberships
    ADD CONSTRAINT repository_memberships_role_check
    CHECK (role IN ('admin', 'maintain', 'write', 'triage', 'read'));

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'repositories_lore_repository_id_format_check'
    ) THEN
        ALTER TABLE repositories
            ADD CONSTRAINT repositories_lore_repository_id_format_check
            CHECK (lore_repository_id ~ '^[0-9a-f]{32}$');
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS organization_memberships_active_idx
    ON organization_memberships (organization_id, user_id)
    WHERE active;

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

ALTER TABLE teams ADD COLUMN IF NOT EXISTS active boolean NOT NULL DEFAULT true;

CREATE TABLE IF NOT EXISTS team_memberships (
    team_id uuid NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role varchar(24) NOT NULL DEFAULT 'member',
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, user_id)
);

ALTER TABLE team_memberships ADD COLUMN IF NOT EXISTS active boolean NOT NULL DEFAULT true;
ALTER TABLE team_memberships DROP CONSTRAINT IF EXISTS team_memberships_role_check;
ALTER TABLE team_memberships ADD CONSTRAINT team_memberships_role_check
    CHECK (role IN ('maintain', 'maintainer', 'member'));

-- This is the shared Actions/control-plane table. Do not create a second
-- team-to-repository role table.
CREATE TABLE IF NOT EXISTS team_repository_roles (
    team_id uuid NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    role varchar(24) NOT NULL DEFAULT 'read',
    created_by uuid NOT NULL REFERENCES users (id),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, repository_id)
);

ALTER TABLE team_repository_roles ADD COLUMN IF NOT EXISTS active boolean NOT NULL DEFAULT true;
ALTER TABLE team_repository_roles DROP CONSTRAINT IF EXISTS team_repository_roles_role_check;
ALTER TABLE team_repository_roles ADD CONSTRAINT team_repository_roles_role_check
    CHECK (role IN ('admin', 'maintain', 'write', 'triage', 'read'));

CREATE INDEX IF NOT EXISTS team_memberships_user_active_idx
    ON team_memberships (user_id, team_id) WHERE active;
CREATE INDEX IF NOT EXISTS team_repository_roles_repository_active_idx
    ON team_repository_roles (repository_id, team_id) WHERE active;

CREATE TABLE IF NOT EXISTS repository_policies (
    repository_id uuid PRIMARY KEY REFERENCES repositories (id) ON DELETE CASCADE,
    allow_cross_repository_links boolean NOT NULL DEFAULT false,
    obliterate_enabled boolean NOT NULL DEFAULT false,
    updated_by uuid REFERENCES users (id),
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO repository_policies (repository_id)
SELECT id FROM repositories
ON CONFLICT (repository_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS repository_obliterate_grants (
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    granted_by uuid NOT NULL REFERENCES users (id),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    PRIMARY KEY (repository_id, user_id)
);

CREATE TABLE IF NOT EXISTS repository_links (
    id uuid PRIMARY KEY,
    source_repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    target_repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    link_kind varchar(32) NOT NULL DEFAULT 'declared',
    created_by uuid NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT repository_links_distinct_check CHECK (source_repository_id <> target_repository_id),
    CONSTRAINT repository_links_kind_check CHECK (link_kind IN ('declared', 'active')),
    CONSTRAINT repository_links_source_target_unique UNIQUE (source_repository_id, target_repository_id)
);

ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS lifecycle_state varchar(16) NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS provisioning_error text;

ALTER TABLE repositories DROP CONSTRAINT IF EXISTS repositories_lifecycle_state_check;
ALTER TABLE repositories ADD CONSTRAINT repositories_lifecycle_state_check
    CHECK (lifecycle_state IN ('pending', 'active', 'failed'));

CREATE TABLE IF NOT EXISTS repository_provisioning (
    repository_id uuid PRIMARY KEY REFERENCES repositories (id) ON DELETE CASCADE,
    requested_by uuid NOT NULL REFERENCES users (id),
    public_lore_url text NOT NULL,
    state varchar(16) NOT NULL DEFAULT 'pending',
    attempts integer NOT NULL DEFAULT 0,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    CONSTRAINT repository_provisioning_state_check CHECK (state IN ('pending', 'active', 'failed')),
    CONSTRAINT repository_provisioning_attempts_check CHECK (attempts >= 0)
);

CREATE TABLE IF NOT EXISTS service_principals (
    id uuid PRIMARY KEY,
    name varchar(128) NOT NULL UNIQUE,
    kind varchar(32) NOT NULL,
    active boolean NOT NULL DEFAULT true,
    created_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT service_principals_kind_check
        CHECK (kind IN ('anonymous_reader', 'ci_runner', 'observer', 'provisioner'))
);

CREATE TABLE IF NOT EXISTS service_principal_repository_grants (
    principal_id uuid NOT NULL REFERENCES service_principals (id) ON DELETE CASCADE,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    permissions varchar(16)[] NOT NULL,
    active boolean NOT NULL DEFAULT true,
    created_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (principal_id, repository_id),
    CONSTRAINT service_principal_grants_permissions_check CHECK (
        cardinality(permissions) > 0
        AND permissions <@ ARRAY['read', 'write', 'admin', 'obliterate']::varchar[]
    )
);

CREATE INDEX IF NOT EXISTS service_principal_grants_repository_idx
    ON service_principal_repository_grants (repository_id, principal_id)
    WHERE active;

INSERT INTO service_principals (id, name, kind)
VALUES
    ('00000000-0000-4000-8000-000000000001', 'lorehub-anonymous-reader', 'anonymous_reader'),
    ('00000000-0000-4000-8000-000000000002', 'lorehub-ci-runner', 'ci_runner'),
    ('00000000-0000-4000-8000-000000000003', 'lorehub-observer', 'observer'),
    ('00000000-0000-4000-8000-000000000004', 'lorehub-provisioner', 'provisioner')
ON CONFLICT (name) DO NOTHING;

INSERT INTO service_principal_repository_grants (
    principal_id, repository_id, permissions, active, created_by
)
SELECT principal.id, repository.id, ARRAY['read']::varchar[], true, repository.created_by
FROM service_principals principal
JOIN repositories repository ON repository.lifecycle_state = 'active'
WHERE principal.name IN ('lorehub-anonymous-reader', 'lorehub-ci-runner', 'lorehub-observer')
  AND (principal.name <> 'lorehub-anonymous-reader' OR repository.visibility = 'public')
ON CONFLICT (principal_id, repository_id) DO NOTHING;

CREATE TABLE IF NOT EXISTS lore_auth_sessions (
    id uuid PRIMARY KEY,
    session_code_digest bytea NOT NULL UNIQUE,
    client_state_digest bytea NOT NULL,
    user_id uuid REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    next_poll_at timestamptz NOT NULL DEFAULT now(),
    poll_count integer NOT NULL DEFAULT 0,
    confirmed_at timestamptz,
    consumed_at timestamptz,
    CONSTRAINT lore_auth_sessions_expiry_check CHECK (expires_at > created_at),
    CONSTRAINT lore_auth_sessions_poll_count_check CHECK (poll_count >= 0)
);

CREATE INDEX IF NOT EXISTS lore_auth_sessions_expiry_idx
    ON lore_auth_sessions (expires_at);

CREATE TABLE IF NOT EXISTS lore_merge_authorizations (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    target_branch_id varchar(64) NOT NULL,
    target_branch_name varchar(255) NOT NULL,
    expected_current_revision varchar(128) NOT NULL,
    proposed_revision varchar(128) NOT NULL,
    source_revision varchar(128) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    CONSTRAINT lore_merge_authorizations_expiry_check CHECK (expires_at > created_at)
);

CREATE INDEX IF NOT EXISTS lore_merge_authorizations_lookup_idx
    ON lore_merge_authorizations (
        repository_id, user_id, target_branch_id, target_branch_name,
        expected_current_revision, proposed_revision
    ) WHERE consumed_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS lore_merge_authorizations_pending_tuple_idx
    ON lore_merge_authorizations (
        repository_id, user_id, target_branch_id, target_branch_name,
        expected_current_revision, proposed_revision
    ) WHERE consumed_at IS NULL;
