CREATE INDEX audit_events_repository_occurred_idx
    ON audit_events (repository_id, occurred_at DESC, id DESC)
    WHERE repository_id IS NOT NULL;

CREATE INDEX issues_repository_created_idx
    ON issues (repository_id, created_at, id);

CREATE INDEX issues_repository_closed_idx
    ON issues (repository_id, closed_at, id)
    WHERE closed_at IS NOT NULL;

CREATE INDEX merge_requests_repository_created_idx
    ON merge_requests (repository_id, created_at, id);

CREATE INDEX merge_requests_repository_merged_idx
    ON merge_requests (repository_id, merged_at, id)
    WHERE merged_at IS NOT NULL;

CREATE INDEX ci_runs_repository_completed_idx
    ON ci_runs (repository_id, completed_at, id)
    WHERE completed_at IS NOT NULL;

CREATE INDEX repository_releases_repository_published_idx
    ON repository_releases (repository_id, published_at, id)
    WHERE published_at IS NOT NULL;
