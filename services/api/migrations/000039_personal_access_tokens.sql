CREATE TABLE personal_access_tokens (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name varchar(80) NOT NULL,
    token_prefix varchar(16) NOT NULL,
    token_digest bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    last_used_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT personal_access_tokens_digest_unique UNIQUE (token_digest),
    CONSTRAINT personal_access_tokens_name_check CHECK (
        name = btrim(name) AND name <> '' AND name !~ '[[:cntrl:]]'
    ),
    CONSTRAINT personal_access_tokens_prefix_check CHECK (
        token_prefix ~ '^lhp_[A-Za-z0-9_-]{8}$'
    ),
    CONSTRAINT personal_access_tokens_digest_check CHECK (octet_length(token_digest) = 32),
    CONSTRAINT personal_access_tokens_expiry_check CHECK (
        expires_at > created_at AND expires_at <= created_at + interval '366 days'
    ),
    CONSTRAINT personal_access_tokens_last_used_check CHECK (
        last_used_at IS NULL OR last_used_at >= created_at
    ),
    CONSTRAINT personal_access_tokens_revoked_check CHECK (
        revoked_at IS NULL OR revoked_at >= created_at
    )
);

CREATE INDEX personal_access_tokens_user_created_idx
    ON personal_access_tokens (user_id, created_at DESC);

CREATE TABLE personal_access_token_scopes (
    token_id uuid NOT NULL REFERENCES personal_access_tokens (id) ON DELETE CASCADE,
    scope varchar(32) NOT NULL,
    PRIMARY KEY (token_id, scope),
    CONSTRAINT personal_access_token_scopes_value_check CHECK (
        scope IN ('read_api', 'api', 'read_repository', 'write_repository')
    )
);
