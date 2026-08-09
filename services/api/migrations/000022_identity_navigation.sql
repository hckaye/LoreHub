ALTER TABLE users
    ADD COLUMN IF NOT EXISTS bio text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS avatar_url text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS website_url text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS location text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS company text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS pronouns varchar(80) NOT NULL DEFAULT '';

ALTER TABLE organizations
    ADD COLUMN IF NOT EXISTS website_url text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS contact_email varchar(320) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS default_repository_visibility varchar(16) NOT NULL DEFAULT 'private';

ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS homepage_url text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS allow_issues boolean NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS allow_merge_requests boolean NOT NULL DEFAULT true;

ALTER TABLE organizations
    DROP CONSTRAINT IF EXISTS organizations_default_repository_visibility_check;

ALTER TABLE organizations
    ADD CONSTRAINT organizations_default_repository_visibility_check
    CHECK (default_repository_visibility IN ('private', 'internal', 'public'));

CREATE TABLE IF NOT EXISTS teams (
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

CREATE INDEX IF NOT EXISTS teams_organization_updated_idx
    ON teams (organization_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS team_memberships (
    team_id uuid NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role varchar(16) NOT NULL DEFAULT 'member',
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, user_id),
    CONSTRAINT team_memberships_role_check CHECK (role IN ('maintainer', 'member'))
);

CREATE INDEX IF NOT EXISTS team_memberships_user_idx
    ON team_memberships (user_id, team_id);

CREATE TABLE IF NOT EXISTS notification_preferences (
    user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    in_app_enabled boolean NOT NULL DEFAULT true,
    email_enabled boolean NOT NULL DEFAULT false,
    mention_enabled boolean NOT NULL DEFAULT true,
    team_enabled boolean NOT NULL DEFAULT true,
    repository_enabled boolean NOT NULL DEFAULT true,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS notifications (
    id uuid PRIMARY KEY,
    recipient_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    source_event_id uuid NOT NULL REFERENCES outbox_events (id) ON DELETE CASCADE,
    topic varchar(160) NOT NULL,
    title varchar(512) NOT NULL,
    body text NOT NULL DEFAULT '',
    href text NOT NULL DEFAULT '/',
    read_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT notifications_recipient_event_unique UNIQUE (recipient_id, source_event_id)
);

CREATE INDEX IF NOT EXISTS notifications_recipient_created_idx
    ON notifications (recipient_id, created_at DESC);

CREATE INDEX IF NOT EXISTS notifications_unread_idx
    ON notifications (recipient_id, created_at DESC)
    WHERE read_at IS NULL;

CREATE INDEX IF NOT EXISTS notifications_source_event_idx
    ON notifications (source_event_id);

CREATE INDEX IF NOT EXISTS users_profile_search_idx
    ON users USING gin (
        to_tsvector(
            'simple'::regconfig,
            COALESCE(username, '') || ' ' || COALESCE(display_name, '') || ' ' ||
            COALESCE(bio, '') || ' ' || COALESCE(company, '') || ' ' || COALESCE(location, '')
        )
    );

CREATE INDEX IF NOT EXISTS organizations_search_idx
    ON organizations USING gin (
        to_tsvector(
            'simple'::regconfig,
            COALESCE(slug, '') || ' ' || COALESCE(display_name, '') || ' ' || COALESCE(description, '')
        )
    );

CREATE INDEX IF NOT EXISTS repositories_search_idx
    ON repositories USING gin (
        to_tsvector(
            'simple'::regconfig,
            COALESCE(slug, '') || ' ' || COALESCE(display_name, '') || ' ' || COALESCE(description, '')
        )
    );
