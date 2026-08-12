CREATE FUNCTION valid_required_status_checks(checks text[])
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
STRICT
AS $$
DECLARE
    value text;
    normalized text[] := ARRAY[]::text[];
BEGIN
    IF cardinality(checks) > 50 OR array_position(checks, NULL) IS NOT NULL THEN
        RETURN false;
    END IF;
    FOREACH value IN ARRAY checks LOOP
        IF value = '' OR value <> btrim(value) OR char_length(value) > 100
            OR value ~ '[[:cntrl:]]' OR lower(value) = ANY(normalized) THEN
            RETURN false;
        END IF;
        normalized := array_append(normalized, lower(value));
    END LOOP;
    RETURN true;
END;
$$;

ALTER TABLE branch_rules
    ADD COLUMN required_status_checks text[] NOT NULL DEFAULT '{}'::text[],
    ADD CONSTRAINT branch_rules_required_status_checks_check
        CHECK (valid_required_status_checks(required_status_checks));

CREATE TABLE revision_statuses (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    revision char(64) NOT NULL,
    context varchar(100) NOT NULL,
    state varchar(16) NOT NULL,
    description varchar(140) NOT NULL DEFAULT '',
    target_url text NOT NULL DEFAULT '',
    creator_id uuid NOT NULL REFERENCES users (id),
    idempotency_key varchar(255),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT revision_statuses_id_repository_unique UNIQUE (id, repository_id),
    CONSTRAINT revision_statuses_revision_check CHECK (revision ~ '^[0-9a-f]{64}$'),
    CONSTRAINT revision_statuses_context_check CHECK (
        char_length(context) BETWEEN 1 AND 100
        AND context = btrim(context)
        AND context !~ '[[:cntrl:]]'
    ),
    CONSTRAINT revision_statuses_state_check CHECK (
        state IN ('pending', 'success', 'failure', 'error')
    ),
    CONSTRAINT revision_statuses_description_check CHECK (
        char_length(description) <= 140 AND description !~ '[[:cntrl:]]'
    ),
    CONSTRAINT revision_statuses_target_url_check CHECK (
        octet_length(target_url) <= 8192 AND target_url !~ '[[:cntrl:]]'
    ),
    CONSTRAINT revision_statuses_idempotency_key_check CHECK (
        idempotency_key IS NULL
        OR (
            char_length(idempotency_key) BETWEEN 1 AND 255
            AND idempotency_key = btrim(idempotency_key)
            AND idempotency_key !~ '[[:cntrl:]]'
        )
    )
);

CREATE INDEX revision_statuses_revision_history_idx
    ON revision_statuses (repository_id, revision, created_at DESC, id DESC);

CREATE INDEX revision_statuses_revision_context_idx
    ON revision_statuses (repository_id, revision, lower(context), created_at DESC, id DESC);

CREATE UNIQUE INDEX revision_statuses_idempotency_idx
    ON revision_statuses (repository_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

ALTER TABLE deployments
    DROP CONSTRAINT deployments_environment_boundary_fk,
    ADD CONSTRAINT deployments_environment_boundary_fk
        FOREIGN KEY (environment_id, repository_id)
        REFERENCES repository_environments (id, repository_id) ON DELETE CASCADE;

ALTER TABLE repository_webhooks
    DROP CONSTRAINT repository_webhooks_events_check;

ALTER TABLE repository_webhooks
    ADD CONSTRAINT repository_webhooks_events_check CHECK (
        cardinality(events) BETWEEN 1 AND 15
        AND array_position(events, NULL) IS NULL
        AND events <@ ARRAY[
            'actions', 'branch_rules', 'branches', 'comments', 'discussions', 'issues',
            'labels', 'milestones', 'projects', 'pull_requests', 'releases', 'repository',
            'reviews', 'statuses', 'wiki'
        ]::text[]
    );
