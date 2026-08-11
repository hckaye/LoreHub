ALTER TABLE repository_counters
    ADD COLUMN next_project_number bigint NOT NULL DEFAULT 1;

ALTER TABLE issues
    ADD CONSTRAINT issues_id_repository_unique UNIQUE (id, repository_id);

CREATE TABLE projects (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    number bigint NOT NULL,
    title varchar(512) NOT NULL,
    description text NOT NULL DEFAULT '',
    state varchar(16) NOT NULL DEFAULT 'open',
    created_by uuid NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT projects_repository_number_unique UNIQUE (repository_id, number),
    CONSTRAINT projects_id_repository_unique UNIQUE (id, repository_id),
    CONSTRAINT projects_number_check CHECK (number > 0),
    CONSTRAINT projects_title_check CHECK (btrim(title) <> ''),
    CONSTRAINT projects_state_check CHECK (state IN ('open', 'closed'))
);

CREATE INDEX projects_repository_state_updated_idx
    ON projects (repository_id, state, updated_at DESC, number DESC);

CREATE TABLE project_columns (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name varchar(255) NOT NULL,
    position bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT project_columns_id_project_unique UNIQUE (id, project_id),
    CONSTRAINT project_columns_project_name_unique UNIQUE (project_id, name),
    CONSTRAINT project_columns_name_check CHECK (btrim(name) <> ''),
    CONSTRAINT project_columns_position_check CHECK (position > 0)
);

CREATE INDEX project_columns_project_position_idx
    ON project_columns (project_id, position, id);

CREATE TABLE project_items (
    id uuid PRIMARY KEY,
    project_id uuid NOT NULL,
    repository_id uuid NOT NULL,
    column_id uuid NOT NULL,
    kind varchar(24) NOT NULL,
    issue_id uuid,
    merge_request_id uuid,
    draft_title varchar(512) NOT NULL DEFAULT '',
    draft_body text NOT NULL DEFAULT '',
    position bigint NOT NULL,
    created_by uuid NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT project_items_project_repository_fk
        FOREIGN KEY (project_id, repository_id)
        REFERENCES projects (id, repository_id) ON DELETE CASCADE,
    CONSTRAINT project_items_column_project_fk
        FOREIGN KEY (column_id, project_id)
        REFERENCES project_columns (id, project_id),
    CONSTRAINT project_items_issue_repository_fk
        FOREIGN KEY (issue_id, repository_id)
        REFERENCES issues (id, repository_id) ON DELETE CASCADE,
    CONSTRAINT project_items_merge_request_repository_fk
        FOREIGN KEY (merge_request_id, repository_id)
        REFERENCES merge_requests (id, repository_id) ON DELETE CASCADE,
    CONSTRAINT project_items_position_check CHECK (position > 0),
    CONSTRAINT project_items_content_check CHECK (
        (
            kind = 'issue'
            AND issue_id IS NOT NULL
            AND merge_request_id IS NULL
            AND draft_title = ''
            AND draft_body = ''
        )
        OR (
            kind = 'merge_request'
            AND issue_id IS NULL
            AND merge_request_id IS NOT NULL
            AND draft_title = ''
            AND draft_body = ''
        )
        OR (
            kind = 'draft'
            AND issue_id IS NULL
            AND merge_request_id IS NULL
            AND btrim(draft_title) <> ''
        )
    )
);

CREATE UNIQUE INDEX project_items_issue_unique
    ON project_items (project_id, issue_id)
    WHERE issue_id IS NOT NULL;

CREATE UNIQUE INDEX project_items_merge_request_unique
    ON project_items (project_id, merge_request_id)
    WHERE merge_request_id IS NOT NULL;

CREATE INDEX project_items_column_position_idx
    ON project_items (column_id, position, id);
