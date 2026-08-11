CREATE UNIQUE INDEX IF NOT EXISTS teams_id_organization_unique_idx
    ON teams (id, organization_id);

CREATE TABLE merge_request_review_requests (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    repository_id uuid NOT NULL,
    merge_request_id uuid NOT NULL,
    reviewer_user_id uuid REFERENCES users (id) ON DELETE CASCADE,
    reviewer_team_id uuid,
    requested_by uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    requested_at timestamptz NOT NULL DEFAULT now(),
    removed_by uuid REFERENCES users (id) ON DELETE RESTRICT,
    removed_at timestamptz,
    CONSTRAINT merge_request_review_requests_repository_fk
        FOREIGN KEY (repository_id, organization_id)
        REFERENCES repositories (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT merge_request_review_requests_merge_request_fk
        FOREIGN KEY (merge_request_id, repository_id)
        REFERENCES merge_requests (id, repository_id) ON DELETE CASCADE,
    CONSTRAINT merge_request_review_requests_team_fk
        FOREIGN KEY (reviewer_team_id, organization_id)
        REFERENCES teams (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT merge_request_review_requests_reviewer_check CHECK (
        (reviewer_user_id IS NOT NULL AND reviewer_team_id IS NULL)
        OR (reviewer_user_id IS NULL AND reviewer_team_id IS NOT NULL)
    ),
    CONSTRAINT merge_request_review_requests_removal_check CHECK (
        (removed_at IS NULL AND removed_by IS NULL)
        OR (removed_at IS NOT NULL AND removed_by IS NOT NULL)
    )
);

CREATE UNIQUE INDEX merge_request_review_requests_user_unique_idx
    ON merge_request_review_requests (merge_request_id, reviewer_user_id)
    WHERE reviewer_user_id IS NOT NULL AND removed_at IS NULL;

CREATE UNIQUE INDEX merge_request_review_requests_team_unique_idx
    ON merge_request_review_requests (merge_request_id, reviewer_team_id)
    WHERE reviewer_team_id IS NOT NULL AND removed_at IS NULL;

CREATE INDEX merge_request_review_requests_repository_idx
    ON merge_request_review_requests (repository_id, merge_request_id, requested_at, id)
    WHERE removed_at IS NULL;
