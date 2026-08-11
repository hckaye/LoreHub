CREATE TABLE issue_assignees (
    issue_id uuid NOT NULL,
    repository_id uuid NOT NULL,
    user_id uuid NOT NULL REFERENCES users (id),
    assigned_by uuid NOT NULL REFERENCES users (id),
    assigned_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, user_id),
    CONSTRAINT issue_assignees_issue_repository_fk
        FOREIGN KEY (issue_id, repository_id)
        REFERENCES issues (id, repository_id)
        ON DELETE CASCADE
);

CREATE INDEX issue_assignees_repository_user_idx
    ON issue_assignees (repository_id, user_id, issue_id);

INSERT INTO issue_assignees (issue_id, repository_id, user_id, assigned_by, assigned_at)
SELECT issue.id, issue.repository_id, issue.assignee_id, issue.author_id, issue.created_at
FROM issues issue
WHERE issue.assignee_id IS NOT NULL
ON CONFLICT (issue_id, user_id) DO NOTHING;

ALTER TABLE issues DROP COLUMN assignee_id;
