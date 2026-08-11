CREATE TABLE merge_request_comments (
    id uuid PRIMARY KEY,
    merge_request_id uuid NOT NULL REFERENCES merge_requests (id) ON DELETE CASCADE,
    author_id uuid NOT NULL REFERENCES users (id),
    body text NOT NULL,
    edited_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT merge_request_comments_body_check CHECK (btrim(body) <> '')
);

CREATE INDEX merge_request_comments_request_created_idx
    ON merge_request_comments (merge_request_id, created_at, id);
