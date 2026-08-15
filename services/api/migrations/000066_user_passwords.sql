CREATE TABLE user_passwords (
    user_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    email varchar(320) NOT NULL,
    password_hash text NOT NULL,
    failed_attempts integer NOT NULL DEFAULT 0,
    locked_until timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT user_passwords_email_unique UNIQUE (email),
    CONSTRAINT user_passwords_email_lower_check CHECK (email = lower(email)),
    CONSTRAINT user_passwords_failed_attempts_check CHECK (failed_attempts >= 0)
);
