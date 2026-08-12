CREATE TABLE revision_comments (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    revision char(64) NOT NULL,
    author_id uuid NOT NULL REFERENCES users (id),
    body text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    edited_at timestamptz,
    CONSTRAINT revision_comments_revision_check CHECK (revision ~ '^[0-9a-f]{64}$'),
    CONSTRAINT revision_comments_body_check CHECK (
        octet_length(body) BETWEEN 1 AND 1000000
        AND body ~ '[^[:space:]]'
    ),
    CONSTRAINT revision_comments_edited_at_check CHECK (
        edited_at IS NULL OR edited_at >= created_at
    )
);

CREATE INDEX revision_comments_revision_page_idx
    ON revision_comments (repository_id, revision, created_at, id);
