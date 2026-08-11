CREATE TABLE repository_stars (
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository_id, user_id)
);

CREATE INDEX repository_stars_user_created_idx
    ON repository_stars (user_id, created_at DESC, repository_id);

CREATE TABLE repository_watches (
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository_id, user_id)
);

CREATE INDEX repository_watches_user_created_idx
    ON repository_watches (user_id, created_at DESC, repository_id);
