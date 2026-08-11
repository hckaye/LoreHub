ALTER TABLE repository_counters
    ADD COLUMN next_milestone_number bigint NOT NULL DEFAULT 1,
    ADD CONSTRAINT repository_counters_milestone_number_check CHECK (next_milestone_number > 0);

CREATE TABLE repository_milestones (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    number bigint NOT NULL,
    title varchar(255) NOT NULL,
    description text NOT NULL DEFAULT '',
    state varchar(16) NOT NULL DEFAULT 'open',
    due_on date,
    created_by uuid NOT NULL REFERENCES users (id),
    closed_by uuid REFERENCES users (id),
    closed_at timestamptz,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT repository_milestones_repository_number_unique UNIQUE (repository_id, number),
    CONSTRAINT repository_milestones_id_repository_unique UNIQUE (id, repository_id),
    CONSTRAINT repository_milestones_title_check CHECK (
        title <> '' AND title = btrim(title) AND title !~ '[[:cntrl:]]'
    ),
    CONSTRAINT repository_milestones_description_check CHECK (
        char_length(description) <= 65536
        AND description !~ '[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]'
    ),
    CONSTRAINT repository_milestones_state_check CHECK (state IN ('open', 'closed')),
    CONSTRAINT repository_milestones_version_check CHECK (version > 0),
    CONSTRAINT repository_milestones_closed_check CHECK (
        (state = 'open' AND closed_by IS NULL AND closed_at IS NULL)
        OR (state = 'closed' AND closed_by IS NOT NULL AND closed_at IS NOT NULL)
    )
);

CREATE UNIQUE INDEX repository_milestones_repository_title_idx
    ON repository_milestones (repository_id, lower(title));

CREATE INDEX repository_milestones_repository_state_due_idx
    ON repository_milestones (repository_id, state, due_on, number);

ALTER TABLE issues ADD COLUMN milestone_id uuid;

ALTER TABLE issues
    ADD CONSTRAINT issues_milestone_repository_fk
    FOREIGN KEY (milestone_id, repository_id)
    REFERENCES repository_milestones (id, repository_id);

CREATE INDEX issues_milestone_state_idx
    ON issues (milestone_id, state)
    WHERE milestone_id IS NOT NULL;
