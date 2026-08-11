CREATE INDEX issues_global_state_updated_idx
    ON issues (state, updated_at DESC, id DESC);

CREATE INDEX merge_requests_global_state_updated_idx
    ON merge_requests (state, updated_at DESC, id DESC);

CREATE INDEX issues_search_idx
    ON issues USING gin (to_tsvector('simple', title || ' ' || body));

CREATE INDEX merge_requests_search_idx
    ON merge_requests USING gin (to_tsvector('simple', title || ' ' || body));

CREATE INDEX issue_assignees_user_issue_idx
    ON issue_assignees (user_id, issue_id);

CREATE INDEX merge_request_assignees_user_request_idx
    ON merge_request_assignees (user_id, merge_request_id);

CREATE INDEX merge_request_review_requests_user_request_idx
    ON merge_request_review_requests (reviewer_user_id, merge_request_id)
    WHERE reviewer_user_id IS NOT NULL AND removed_at IS NULL;

CREATE INDEX merge_request_review_requests_team_request_idx
    ON merge_request_review_requests (reviewer_team_id, merge_request_id)
    WHERE reviewer_team_id IS NOT NULL AND removed_at IS NULL;
