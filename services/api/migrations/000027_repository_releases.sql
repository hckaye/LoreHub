CREATE TABLE repository_releases (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    tag_name varchar(128) NOT NULL,
    title varchar(512) NOT NULL,
    notes text NOT NULL DEFAULT '',
    source_branch varchar(255) NOT NULL,
    revision char(64) NOT NULL,
    state varchar(16) NOT NULL DEFAULT 'draft',
    created_by uuid NOT NULL REFERENCES users (id),
    published_by uuid REFERENCES users (id),
    published_at timestamptz,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT repository_releases_repository_tag_unique UNIQUE (repository_id, tag_name),
    CONSTRAINT repository_releases_id_repository_unique UNIQUE (id, repository_id),
    CONSTRAINT repository_releases_tag_check CHECK (
        tag_name <> '' AND tag_name = btrim(tag_name) AND tag_name !~ '[[:cntrl:]]'
    ),
    CONSTRAINT repository_releases_title_check CHECK (
        title <> '' AND title = btrim(title) AND title !~ '[[:cntrl:]]'
    ),
    CONSTRAINT repository_releases_branch_check CHECK (
        source_branch <> '' AND source_branch = btrim(source_branch)
        AND source_branch !~ '[[:cntrl:]]'
    ),
    CONSTRAINT repository_releases_notes_check CHECK (
        char_length(notes) <= 1048576 AND notes !~ '[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]'
    ),
    CONSTRAINT repository_releases_revision_check CHECK (revision ~ '^[0-9a-f]{64}$'),
    CONSTRAINT repository_releases_state_check CHECK (state IN ('draft', 'published')),
    CONSTRAINT repository_releases_version_check CHECK (version > 0),
    CONSTRAINT repository_releases_publication_check CHECK (
        (state = 'draft' AND published_by IS NULL AND published_at IS NULL)
        OR (state = 'published' AND published_by IS NOT NULL AND published_at IS NOT NULL)
    )
);

CREATE INDEX repository_releases_repository_state_updated_idx
    ON repository_releases (repository_id, state, updated_at DESC, id DESC);

CREATE TABLE release_asset_links (
    id uuid PRIMARY KEY,
    release_id uuid NOT NULL,
    repository_id uuid NOT NULL,
    name varchar(255) NOT NULL,
    external_url text NOT NULL,
    created_by uuid NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT release_asset_links_release_repository_fk
        FOREIGN KEY (release_id, repository_id)
        REFERENCES repository_releases (id, repository_id) ON DELETE CASCADE,
    CONSTRAINT release_asset_links_release_name_unique UNIQUE (release_id, name),
    CONSTRAINT release_asset_links_name_check CHECK (
        name <> '' AND name = btrim(name) AND name !~ '[[:cntrl:]]'
    ),
    CONSTRAINT release_asset_links_url_check CHECK (
        octet_length(external_url) BETWEEN 1 AND 8192
        AND external_url ~ '^https?://[^[:space:][:cntrl:]]+$'
    )
);

CREATE INDEX release_asset_links_release_created_idx
    ON release_asset_links (release_id, created_at, id);
