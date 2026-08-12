ALTER TABLE repositories
    DROP CONSTRAINT IF EXISTS repositories_lifecycle_state_check;

ALTER TABLE repositories
    ADD CONSTRAINT repositories_lifecycle_state_check
    CHECK (lifecycle_state IN ('pending', 'active', 'failed', 'deleting', 'purging'));

ALTER TABLE service_principals
    DROP CONSTRAINT IF EXISTS service_principals_kind_check;

ALTER TABLE service_principals
    ADD CONSTRAINT service_principals_kind_check
    CHECK (kind IN ('anonymous_reader', 'ci_runner', 'observer', 'provisioner', 'lifecycle'));

INSERT INTO service_principals (id, name, kind)
VALUES (
    '00000000-0000-4000-8000-000000000005',
    'lorehub-repository-lifecycle',
    'lifecycle'
)
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name,
    kind = EXCLUDED.kind,
    active = true,
    updated_at = now();

CREATE TABLE repository_deletions (
    repository_id uuid PRIMARY KEY REFERENCES repositories (id) ON DELETE CASCADE,
    requested_by uuid NOT NULL REFERENCES users (id),
    requested_at timestamptz NOT NULL DEFAULT now(),
    purge_after timestamptz NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL,
    last_error text,
    lease_owner varchar(128),
    lease_expires_at timestamptz,
    CONSTRAINT repository_deletions_retention_check CHECK (purge_after > requested_at),
    CONSTRAINT repository_deletions_attempts_check CHECK (attempts >= 0),
    CONSTRAINT repository_deletions_error_check CHECK (
        last_error IS NULL OR char_length(last_error) <= 1000
    ),
    CONSTRAINT repository_deletions_lease_check CHECK (
        (lease_owner IS NULL AND lease_expires_at IS NULL)
        OR (
            lease_owner IS NOT NULL
            AND char_length(lease_owner) BETWEEN 1 AND 128
            AND lease_expires_at IS NOT NULL
        )
    )
);

CREATE INDEX repository_deletions_due_idx
    ON repository_deletions (next_attempt_at, purge_after, repository_id);
