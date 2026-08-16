CREATE TABLE password_resets (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_digest bytea NOT NULL UNIQUE,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    CONSTRAINT password_resets_expiry_check CHECK (expires_at > created_at)
);

CREATE INDEX password_resets_expiry_idx
    ON password_resets (expires_at);

CREATE INDEX password_resets_user_idx
    ON password_resets (user_id);
