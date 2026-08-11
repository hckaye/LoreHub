ALTER TABLE repository_webhooks
    DROP CONSTRAINT repository_webhooks_events_check;

ALTER TABLE repository_webhooks
    ADD CONSTRAINT repository_webhooks_events_check CHECK (
        cardinality(events) BETWEEN 1 AND 13
        AND array_position(events, NULL) IS NULL
        AND events <@ ARRAY[
            'actions', 'branch_rules', 'branches', 'comments', 'issues', 'labels',
            'milestones', 'projects', 'pull_requests', 'releases', 'repository', 'reviews', 'wiki'
        ]::text[]
    );

CREATE TABLE repository_wiki_pages (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    slug varchar(160) NOT NULL,
    title varchar(256) NOT NULL,
    body text NOT NULL,
    version integer NOT NULL DEFAULT 1,
    created_by uuid NOT NULL REFERENCES users (id),
    updated_by uuid NOT NULL REFERENCES users (id),
    archived_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,
    CONSTRAINT repository_wiki_pages_identity_unique UNIQUE (id, repository_id),
    CONSTRAINT repository_wiki_pages_slug_check CHECK (
        slug = lower(slug)
        AND octet_length(slug) BETWEEN 1 AND 160
        AND slug !~ '[[:space:]/\\]'
        AND slug NOT LIKE '.%'
        AND slug NOT LIKE '%.'
    ),
    CONSTRAINT repository_wiki_pages_title_check CHECK (
        octet_length(btrim(title)) BETWEEN 1 AND 256
    ),
    CONSTRAINT repository_wiki_pages_body_check CHECK (octet_length(body) <= 1048576),
    CONSTRAINT repository_wiki_pages_version_check CHECK (version > 0),
    CONSTRAINT repository_wiki_pages_archive_check CHECK (
        (archived_at IS NULL AND archived_by IS NULL)
        OR (archived_at IS NOT NULL AND archived_by IS NOT NULL)
    )
);

CREATE UNIQUE INDEX repository_wiki_pages_active_slug_idx
    ON repository_wiki_pages (repository_id, slug)
    WHERE archived_at IS NULL;

CREATE INDEX repository_wiki_pages_repository_updated_idx
    ON repository_wiki_pages (repository_id, updated_at DESC, id DESC)
    WHERE archived_at IS NULL;

CREATE TABLE repository_wiki_page_versions (
    page_id uuid NOT NULL,
    repository_id uuid NOT NULL,
    version integer NOT NULL,
    slug varchar(160) NOT NULL,
    title varchar(256) NOT NULL,
    body text NOT NULL,
    edit_summary varchar(256) NOT NULL DEFAULT '',
    edited_by uuid NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (page_id, version),
    CONSTRAINT repository_wiki_page_versions_page_fk
        FOREIGN KEY (page_id, repository_id)
        REFERENCES repository_wiki_pages (id, repository_id) ON DELETE CASCADE,
    CONSTRAINT repository_wiki_page_versions_slug_check CHECK (
        octet_length(slug) BETWEEN 1 AND 160
    ),
    CONSTRAINT repository_wiki_page_versions_title_check CHECK (
        octet_length(btrim(title)) BETWEEN 1 AND 256
    ),
    CONSTRAINT repository_wiki_page_versions_body_check CHECK (octet_length(body) <= 1048576),
    CONSTRAINT repository_wiki_page_versions_summary_check CHECK (octet_length(edit_summary) <= 256),
    CONSTRAINT repository_wiki_page_versions_version_check CHECK (version > 0)
);

CREATE INDEX repository_wiki_page_versions_history_idx
    ON repository_wiki_page_versions (page_id, version DESC);
