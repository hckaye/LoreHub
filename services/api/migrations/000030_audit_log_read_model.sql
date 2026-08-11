DROP INDEX IF EXISTS audit_events_organization_occurred_idx;

CREATE INDEX audit_events_organization_occurred_idx
    ON audit_events (organization_id, occurred_at DESC, id DESC);

CREATE INDEX audit_events_organization_actor_occurred_idx
    ON audit_events (organization_id, actor_id, occurred_at DESC, id DESC)
    WHERE actor_id IS NOT NULL;

CREATE INDEX audit_events_organization_repository_occurred_idx
    ON audit_events (organization_id, repository_id, occurred_at DESC, id DESC)
    WHERE repository_id IS NOT NULL;
