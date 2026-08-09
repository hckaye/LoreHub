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
    scope_kind varchar(16),
    CONSTRAINT notifications_recipient_event_unique UNIQUE (recipient_id, source_event_id)
);

ALTER TABLE notifications
    ADD COLUMN IF NOT EXISTS scope_organization_id uuid,
    ADD COLUMN IF NOT EXISTS scope_repository_id uuid,
    ADD COLUMN IF NOT EXISTS scope_team_id uuid,
    ADD COLUMN IF NOT EXISTS scope_visibility varchar(16),
    ADD COLUMN IF NOT EXISTS scope_kind varchar(16);

ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_scope_kind_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_scope_kind_check
    CHECK (scope_kind IS NULL OR scope_kind IN ('user', 'organization', 'repository', 'team'));

UPDATE notifications
SET scope_organization_id = NULL
WHERE scope_organization_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM organizations WHERE organizations.id = notifications.scope_organization_id);

UPDATE notifications
SET scope_repository_id = NULL
WHERE scope_repository_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM repositories WHERE repositories.id = notifications.scope_repository_id);

UPDATE notifications
SET scope_team_id = NULL
WHERE scope_team_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM teams WHERE teams.id = notifications.scope_team_id);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'notifications_scope_organization_fk'
    ) THEN
        ALTER TABLE notifications
            ADD CONSTRAINT notifications_scope_organization_fk
            FOREIGN KEY (scope_organization_id) REFERENCES organizations (id) ON DELETE SET NULL;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'notifications_scope_repository_fk'
    ) THEN
        ALTER TABLE notifications
            ADD CONSTRAINT notifications_scope_repository_fk
            FOREIGN KEY (scope_repository_id) REFERENCES repositories (id) ON DELETE SET NULL;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'notifications_scope_team_fk'
    ) THEN
        ALTER TABLE notifications
            ADD CONSTRAINT notifications_scope_team_fk
            FOREIGN KEY (scope_team_id) REFERENCES teams (id) ON DELETE SET NULL;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS notifications_recipient_created_idx
    ON notifications (recipient_id, created_at DESC);

CREATE INDEX IF NOT EXISTS notifications_unread_idx
    ON notifications (recipient_id, created_at DESC)
    WHERE read_at IS NULL;

CREATE INDEX IF NOT EXISTS notifications_source_event_idx
    ON notifications (source_event_id);

CREATE TABLE IF NOT EXISTS notification_projection_ledger (
    source_event_id uuid PRIMARY KEY REFERENCES outbox_events (id) ON DELETE CASCADE,
    status varchar(16) NOT NULL DEFAULT 'processing',
    claimed_at timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz,
    CONSTRAINT notification_projection_ledger_status_check
        CHECK (status IN ('processing', 'processed'))
);

CREATE INDEX IF NOT EXISTS notification_projection_ledger_claim_idx
    ON notification_projection_ledger (status, claimed_at);

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
