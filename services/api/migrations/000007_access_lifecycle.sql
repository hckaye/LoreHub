-- Active access lifecycle fields used by protected Lore merge authorization.
-- Lore repository contents remain outside PostgreSQL.

ALTER TABLE organizations
    ADD COLUMN IF NOT EXISTS active boolean NOT NULL DEFAULT true;

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_status_check;
ALTER TABLE users ADD CONSTRAINT users_status_check
    CHECK (status IN ('active', 'suspended', 'inactive'));

ALTER TABLE organization_memberships
    ADD COLUMN IF NOT EXISTS active boolean NOT NULL DEFAULT true;

ALTER TABLE repository_memberships
    ADD COLUMN IF NOT EXISTS active boolean NOT NULL DEFAULT true;
ALTER TABLE repository_memberships DROP CONSTRAINT IF EXISTS repository_memberships_role_check;
ALTER TABLE repository_memberships ADD CONSTRAINT repository_memberships_role_check
    CHECK (role IN ('admin', 'maintain', 'write', 'triage', 'read'));

ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS lifecycle_state varchar(16) NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS provisioning_error text;
ALTER TABLE repositories DROP CONSTRAINT IF EXISTS repositories_lifecycle_state_check;
ALTER TABLE repositories ADD CONSTRAINT repositories_lifecycle_state_check
    CHECK (lifecycle_state IN ('pending', 'active', 'failed'));

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'repositories_lore_repository_id_format_check'
    ) THEN
        ALTER TABLE repositories ADD CONSTRAINT repositories_lore_repository_id_format_check
            CHECK (lore_repository_id ~ '^[0-9a-f]{32}$');
    END IF;
END $$;

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

CREATE INDEX IF NOT EXISTS organization_memberships_active_idx
    ON organization_memberships (organization_id, user_id) WHERE active;
CREATE INDEX IF NOT EXISTS repository_memberships_active_idx
    ON repository_memberships (repository_id, user_id) WHERE active;
CREATE INDEX IF NOT EXISTS team_memberships_user_active_idx
    ON team_memberships (user_id, team_id) WHERE active;
CREATE INDEX IF NOT EXISTS team_repository_roles_repository_active_idx
    ON team_repository_roles (repository_id, team_id) WHERE active;
