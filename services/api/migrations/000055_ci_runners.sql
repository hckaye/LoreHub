CREATE TABLE ci_runners (
    id uuid PRIMARY KEY,
    organization_id uuid REFERENCES organizations (id) ON DELETE CASCADE,
    repository_id uuid,
    user_id uuid REFERENCES users (id) ON DELETE CASCADE,
    name varchar(100) NOT NULL,
    labels jsonb NOT NULL,
    credential_digest bytea NOT NULL,
    credential_key_id varchar(128) NOT NULL,
    credential_expires_at timestamptz NOT NULL,
    last_used_at timestamptz,
    revoked_at timestamptz,
    runner_version varchar(64) NOT NULL,
    last_seen_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ci_runners_repository_boundary_fk
        FOREIGN KEY (repository_id, organization_id)
        REFERENCES repositories (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT ci_runners_scope_check CHECK (
        (repository_id IS NOT NULL AND organization_id IS NOT NULL AND user_id IS NULL)
        OR (repository_id IS NULL AND organization_id IS NOT NULL AND user_id IS NULL)
        OR (repository_id IS NULL AND organization_id IS NULL AND user_id IS NOT NULL)
    ),
    CONSTRAINT ci_runners_name_check CHECK (
        name = btrim(name) AND name <> '' AND name !~ '[[:cntrl:]]'
    ),
    CONSTRAINT ci_runners_labels_shape_check CHECK (
        jsonb_typeof(labels) = 'array'
        AND jsonb_array_length(labels) BETWEEN 1 AND 100
        AND NOT jsonb_path_exists(labels, '$[*] ? (@.type() != "string")')
    ),
    CONSTRAINT ci_runners_labels_value_check CHECK (
        labels = lower(labels::text)::jsonb
        AND NOT jsonb_path_exists(labels, '$[*] ? (@ == "")')
        AND NOT jsonb_path_exists(labels, '$[*] ? (@ like_regex "^.{101}" flag "s")')
    ),
    CONSTRAINT ci_runners_credential_digest_unique UNIQUE (credential_digest),
    CONSTRAINT ci_runners_credential_digest_check CHECK (octet_length(credential_digest) = 32),
    CONSTRAINT ci_runners_credential_key_id_check CHECK (
        credential_key_id ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$'
    ),
    CONSTRAINT ci_runners_credential_expiry_check CHECK (
        credential_expires_at > created_at
        AND credential_expires_at <= created_at + interval '366 days'
    ),
    CONSTRAINT ci_runners_last_used_check CHECK (
        last_used_at IS NULL OR last_used_at >= created_at
    ),
    CONSTRAINT ci_runners_revoked_check CHECK (
        revoked_at IS NULL OR revoked_at >= created_at
    ),
    CONSTRAINT ci_runners_version_check CHECK (
        runner_version = btrim(runner_version)
        AND runner_version <> ''
        AND runner_version !~ '[[:cntrl:]]'
    ),
    CONSTRAINT ci_runners_last_seen_check CHECK (
        last_seen_at IS NULL OR last_seen_at >= created_at
    )
);

CREATE INDEX ci_runners_organization_created_idx
    ON ci_runners (organization_id, created_at DESC)
    WHERE repository_id IS NULL AND user_id IS NULL;

CREATE INDEX ci_runners_repository_created_idx
    ON ci_runners (repository_id, created_at DESC)
    WHERE repository_id IS NOT NULL;

CREATE INDEX ci_runners_user_created_idx
    ON ci_runners (user_id, created_at DESC)
    WHERE user_id IS NOT NULL;

CREATE TABLE runner_registration_tokens (
    id uuid PRIMARY KEY,
    organization_id uuid REFERENCES organizations (id) ON DELETE CASCADE,
    repository_id uuid,
    user_id uuid REFERENCES users (id) ON DELETE CASCADE,
    token_digest bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_by uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT runner_registration_tokens_repository_boundary_fk
        FOREIGN KEY (repository_id, organization_id)
        REFERENCES repositories (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT runner_registration_tokens_scope_check CHECK (
        (repository_id IS NOT NULL AND organization_id IS NOT NULL AND user_id IS NULL)
        OR (repository_id IS NULL AND organization_id IS NOT NULL AND user_id IS NULL)
        OR (repository_id IS NULL AND organization_id IS NULL AND user_id IS NOT NULL)
    ),
    CONSTRAINT runner_registration_tokens_digest_unique UNIQUE (token_digest),
    CONSTRAINT runner_registration_tokens_digest_check CHECK (octet_length(token_digest) = 32),
    CONSTRAINT runner_registration_tokens_expiry_check CHECK (
        expires_at > created_at AND expires_at <= created_at + interval '1 day'
    ),
    CONSTRAINT runner_registration_tokens_consumed_check CHECK (
        consumed_at IS NULL OR (consumed_at >= created_at AND consumed_at < expires_at)
    )
);

CREATE INDEX runner_registration_tokens_expiry_idx
    ON runner_registration_tokens (expires_at)
    WHERE consumed_at IS NULL;
