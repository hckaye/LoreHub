-- Control-plane authorization boundary for Lore repositories.
--
-- repositories.lore_repository_id is the one canonical Lore partition ID. It
-- is unique and non-empty; no second partition identifier is introduced.

ALTER TABLE repositories
    ADD CONSTRAINT repositories_lore_repository_id_nonempty_check
    CHECK (lore_repository_id ~ '^[0-9a-f]{32}$');

ALTER TABLE organization_memberships
    ADD COLUMN active boolean NOT NULL DEFAULT true;

ALTER TABLE repository_memberships
    DROP CONSTRAINT repository_memberships_role_check;

ALTER TABLE repository_memberships
    ADD COLUMN active boolean NOT NULL DEFAULT true;

ALTER TABLE repository_memberships
    ADD CONSTRAINT repository_memberships_role_check
    CHECK (role IN ('admin', 'maintain', 'write', 'triage', 'read'));

CREATE INDEX organization_memberships_active_idx
    ON organization_memberships (organization_id, user_id)
    WHERE active;

CREATE TABLE teams (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    slug varchar(64) NOT NULL,
    display_name varchar(160) NOT NULL,
    description text NOT NULL DEFAULT '',
    created_by uuid NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT teams_organization_slug_unique UNIQUE (organization_id, slug)
);

CREATE TABLE team_memberships (
    team_id uuid NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role varchar(24) NOT NULL DEFAULT 'member',
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, user_id),
    CONSTRAINT team_memberships_role_check CHECK (role IN ('maintainer', 'member'))
);

CREATE INDEX team_memberships_user_idx
    ON team_memberships (user_id, team_id)
    WHERE active;

CREATE TABLE team_repository_roles (
    team_id uuid NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    role varchar(24) NOT NULL,
    created_by uuid NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, repository_id),
    CONSTRAINT team_repository_roles_role_check
        CHECK (role IN ('admin', 'maintain', 'write', 'triage', 'read'))
);

CREATE INDEX team_repository_roles_repository_idx
    ON team_repository_roles (repository_id, role);

CREATE TABLE repository_policies (
    repository_id uuid PRIMARY KEY REFERENCES repositories (id) ON DELETE CASCADE,
    allow_cross_repository_links boolean NOT NULL DEFAULT false,
    obliterate_enabled boolean NOT NULL DEFAULT false,
    updated_by uuid REFERENCES users (id),
    updated_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO repository_policies (repository_id)
SELECT id FROM repositories
ON CONFLICT (repository_id) DO NOTHING;

CREATE TABLE repository_obliterate_grants (
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    granted_by uuid NOT NULL REFERENCES users (id),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    PRIMARY KEY (repository_id, user_id)
);

CREATE TABLE repository_links (
    id uuid PRIMARY KEY,
    source_repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    target_repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    link_kind varchar(32) NOT NULL DEFAULT 'declared',
    created_by uuid NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT repository_links_distinct_check
        CHECK (source_repository_id <> target_repository_id),
    CONSTRAINT repository_links_kind_check
        CHECK (link_kind IN ('declared', 'active')),
    CONSTRAINT repository_links_source_target_unique
        UNIQUE (source_repository_id, target_repository_id)
);

CREATE TABLE lore_auth_sessions (
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

CREATE INDEX lore_auth_sessions_expiry_idx
    ON lore_auth_sessions (expires_at);

CREATE TABLE lore_operation_authorizations (
    id uuid PRIMARY KEY,
    token_digest bytea NOT NULL UNIQUE,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    operation varchar(32) NOT NULL,
    branch_id varchar(64) NOT NULL,
    branch_name varchar(255) NOT NULL,
    expected_base_revision varchar(128) NOT NULL,
    expected_head_revision varchar(128) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    CONSTRAINT lore_operation_authorizations_operation_check
        CHECK (operation = 'merge'),
    CONSTRAINT lore_operation_authorizations_expiry_check
        CHECK (expires_at > created_at)
);

CREATE INDEX lore_operation_authorizations_lookup_idx
    ON lore_operation_authorizations (repository_id, user_id, operation, expires_at)
    WHERE consumed_at IS NULL;
