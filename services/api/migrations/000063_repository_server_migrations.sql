ALTER TABLE repositories
    ADD COLUMN migrating_at timestamptz;

CREATE INDEX repositories_migrating_idx
    ON repositories (migrating_at)
    WHERE migrating_at IS NOT NULL;

CREATE TABLE repository_migrations (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE RESTRICT,
    from_server_id uuid NOT NULL REFERENCES lore_servers (id),
    to_server_id uuid NOT NULL REFERENCES lore_servers (id),
    state varchar(16) NOT NULL DEFAULT 'pending',
    error_text text NOT NULL DEFAULT '',
    created_by uuid NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    completed_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT repository_migrations_servers_check CHECK (from_server_id <> to_server_id),
    CONSTRAINT repository_migrations_state_check CHECK (
        state IN ('pending', 'mirroring', 'repointing', 'completed', 'failed')
    ),
    CONSTRAINT repository_migrations_error_check CHECK (
        length(error_text) <= 4096
    ),
    CONSTRAINT repository_migrations_started_check CHECK (
        started_at IS NULL OR started_at >= created_at
    ),
    CONSTRAINT repository_migrations_completed_check CHECK (
        completed_at IS NULL OR completed_at >= created_at
    )
);

CREATE UNIQUE INDEX repository_migrations_active_unique
    ON repository_migrations (repository_id)
    WHERE state IN ('pending', 'mirroring', 'repointing');

CREATE INDEX repository_migrations_repository_idx
    ON repository_migrations (repository_id, created_at DESC);
