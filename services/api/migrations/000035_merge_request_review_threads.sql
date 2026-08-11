CREATE TABLE merge_request_review_threads (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    merge_request_id uuid NOT NULL,
    path text NOT NULL,
    side varchar(5) NOT NULL,
    line_number integer NOT NULL,
    line_content text NOT NULL,
    base_revision text NOT NULL,
    head_revision text NOT NULL,
    resolved boolean NOT NULL DEFAULT false,
    version integer NOT NULL DEFAULT 1,
    created_by uuid NOT NULL REFERENCES users (id),
    resolved_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz,
    CONSTRAINT merge_request_review_threads_repository_unique UNIQUE (id, repository_id),
    CONSTRAINT merge_request_review_threads_request_fk
        FOREIGN KEY (merge_request_id, repository_id)
        REFERENCES merge_requests (id, repository_id) ON DELETE CASCADE,
    CONSTRAINT merge_request_review_threads_path_check CHECK (
        octet_length(path) BETWEEN 1 AND 4096
        AND path !~ '[[:cntrl:]]'
        AND path NOT LIKE '/%'
        AND path NOT IN ('.', '..')
        AND path NOT LIKE '%/./%'
        AND path NOT LIKE './%'
        AND path NOT LIKE '%/.'
        AND path NOT LIKE '%/../%'
        AND path NOT LIKE '../%'
        AND path NOT LIKE '%/..'
    ),
    CONSTRAINT merge_request_review_threads_side_check CHECK (side IN ('left', 'right')),
    CONSTRAINT merge_request_review_threads_line_check CHECK (line_number > 0),
    CONSTRAINT merge_request_review_threads_content_check CHECK (octet_length(line_content) <= 8192),
    CONSTRAINT merge_request_review_threads_revisions_check CHECK (
        octet_length(btrim(base_revision)) BETWEEN 1 AND 512
        AND octet_length(btrim(head_revision)) BETWEEN 1 AND 512
    ),
    CONSTRAINT merge_request_review_threads_version_check CHECK (version > 0),
    CONSTRAINT merge_request_review_threads_resolution_check CHECK (
        (resolved = false AND resolved_at IS NULL AND resolved_by IS NULL)
        OR (resolved = true AND resolved_at IS NOT NULL AND resolved_by IS NOT NULL)
    )
);

CREATE INDEX merge_request_review_threads_request_path_idx
    ON merge_request_review_threads (merge_request_id, path, line_number, created_at, id);

CREATE INDEX merge_request_review_threads_unresolved_idx
    ON merge_request_review_threads (merge_request_id, created_at, id)
    WHERE resolved = false;

CREATE TABLE merge_request_review_comments (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    thread_id uuid NOT NULL,
    author_id uuid NOT NULL REFERENCES users (id),
    body text NOT NULL,
    version integer NOT NULL DEFAULT 1,
    edited_at timestamptz,
    deleted_at timestamptz,
    deleted_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT merge_request_review_comments_thread_fk
        FOREIGN KEY (thread_id, repository_id)
        REFERENCES merge_request_review_threads (id, repository_id) ON DELETE CASCADE,
    CONSTRAINT merge_request_review_comments_body_check CHECK (
        (
            deleted_at IS NULL
            AND deleted_by IS NULL
            AND octet_length(btrim(body)) BETWEEN 1 AND 1048576
        )
        OR (deleted_at IS NOT NULL AND deleted_by IS NOT NULL AND body = '')
    ),
    CONSTRAINT merge_request_review_comments_version_check CHECK (version > 0)
);

CREATE INDEX merge_request_review_comments_thread_created_idx
    ON merge_request_review_comments (thread_id, created_at, id);
