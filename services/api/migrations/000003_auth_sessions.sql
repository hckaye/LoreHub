CREATE TABLE login_transactions (
    id uuid PRIMARY KEY,
    state_digest bytea NOT NULL UNIQUE,
    code_verifier_digest bytea NOT NULL,
    nonce_digest bytea NOT NULL,
    return_to text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    CONSTRAINT login_transactions_expiry_check CHECK (expires_at > created_at)
);

CREATE INDEX login_transactions_expiry_idx
    ON login_transactions (expires_at);

CREATE TABLE sessions (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_digest bytea NOT NULL UNIQUE,
    csrf_token_digest bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    CONSTRAINT sessions_expiry_check CHECK (expires_at > created_at)
);

CREATE INDEX sessions_expiry_idx
    ON sessions (expires_at);

CREATE INDEX sessions_revoked_idx
    ON sessions (revoked_at)
    WHERE revoked_at IS NOT NULL;
