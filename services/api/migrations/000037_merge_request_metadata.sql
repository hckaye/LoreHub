ALTER TABLE labels
    ADD CONSTRAINT labels_id_repository_unique UNIQUE (id, repository_id);

CREATE TABLE merge_request_labels (
    merge_request_id uuid NOT NULL,
    repository_id uuid NOT NULL,
    label_id uuid NOT NULL,
    applied_by uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    applied_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (merge_request_id, label_id),
    CONSTRAINT merge_request_labels_request_fk
        FOREIGN KEY (merge_request_id, repository_id)
        REFERENCES merge_requests (id, repository_id) ON DELETE CASCADE,
    CONSTRAINT merge_request_labels_label_fk
        FOREIGN KEY (label_id, repository_id)
        REFERENCES labels (id, repository_id) ON DELETE CASCADE
);

CREATE INDEX merge_request_labels_repository_idx
    ON merge_request_labels (repository_id, label_id, merge_request_id);

CREATE TABLE merge_request_assignees (
    merge_request_id uuid NOT NULL,
    repository_id uuid NOT NULL,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    assigned_by uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    assigned_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (merge_request_id, user_id),
    CONSTRAINT merge_request_assignees_request_fk
        FOREIGN KEY (merge_request_id, repository_id)
        REFERENCES merge_requests (id, repository_id) ON DELETE CASCADE
);

CREATE INDEX merge_request_assignees_repository_idx
    ON merge_request_assignees (repository_id, user_id, merge_request_id);

ALTER TABLE merge_requests ADD COLUMN milestone_id uuid;

ALTER TABLE merge_requests
    ADD CONSTRAINT merge_requests_milestone_repository_fk
    FOREIGN KEY (milestone_id, repository_id)
    REFERENCES repository_milestones (id, repository_id);

CREATE INDEX merge_requests_milestone_state_idx
    ON merge_requests (milestone_id, state)
    WHERE milestone_id IS NOT NULL;
