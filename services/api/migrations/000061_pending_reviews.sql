CREATE TABLE pending_reviews (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    merge_request_id uuid NOT NULL,
    author varchar(64) NOT NULL,
    state varchar(16) NOT NULL DEFAULT 'pending',
    body text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pending_reviews_repository_unique UNIQUE (id, repository_id),
    CONSTRAINT pending_reviews_request_fk
        FOREIGN KEY (merge_request_id, repository_id)
        REFERENCES merge_requests (id, repository_id) ON DELETE CASCADE,
    CONSTRAINT pending_reviews_state_check CHECK (state = 'pending'),
    CONSTRAINT pending_reviews_author_check CHECK (octet_length(btrim(author)) BETWEEN 1 AND 64),
    CONSTRAINT pending_reviews_body_check CHECK (octet_length(body) <= 1048576)
);

CREATE UNIQUE INDEX pending_reviews_author_unique
    ON pending_reviews (merge_request_id, author)
    WHERE state = 'pending';

ALTER TABLE merge_request_review_comments
    ADD COLUMN pending_review_id uuid,
    ADD CONSTRAINT merge_request_review_comments_pending_review_fk
        FOREIGN KEY (pending_review_id, repository_id)
        REFERENCES pending_reviews (id, repository_id) ON DELETE CASCADE;

CREATE INDEX merge_request_review_comments_pending_review_idx
    ON merge_request_review_comments (pending_review_id)
    WHERE pending_review_id IS NOT NULL;
