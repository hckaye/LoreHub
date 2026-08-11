CREATE TABLE repository_webhooks (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    url text NOT NULL,
    events text[] NOT NULL,
    active boolean NOT NULL DEFAULT true,
    secret_ciphertext bytea NOT NULL,
    secret_nonce bytea NOT NULL,
    secret_key_id varchar(128) NOT NULL,
    created_by uuid NOT NULL REFERENCES users (id),
    updated_by uuid NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT repository_webhooks_repository_url_unique UNIQUE (repository_id, url),
    CONSTRAINT repository_webhooks_events_check CHECK (
        cardinality(events) BETWEEN 1 AND 12
        AND array_position(events, NULL) IS NULL
        AND events <@ ARRAY[
            'actions', 'branch_rules', 'branches', 'comments', 'issues', 'labels',
            'milestones', 'projects', 'pull_requests', 'releases', 'repository', 'reviews'
        ]::text[]
    ),
    CONSTRAINT repository_webhooks_nonce_check CHECK (octet_length(secret_nonce) = 12),
    CONSTRAINT repository_webhooks_ciphertext_check CHECK (octet_length(secret_ciphertext) >= 16),
    CONSTRAINT repository_webhooks_key_id_check CHECK (secret_key_id ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$')
);

CREATE INDEX repository_webhooks_active_repository_idx
    ON repository_webhooks (repository_id, created_at, id)
    WHERE active;

CREATE TABLE webhook_projection_ledger (
    source_event_id uuid PRIMARY KEY REFERENCES outbox_events (id) ON DELETE CASCADE,
    status varchar(16) NOT NULL,
    claimed_at timestamptz NOT NULL,
    processed_at timestamptz,
    CONSTRAINT webhook_projection_ledger_status_check CHECK (status IN ('processing', 'processed'))
);

CREATE INDEX webhook_projection_ledger_claim_idx
    ON webhook_projection_ledger (status, claimed_at);

CREATE TABLE webhook_deliveries (
    id uuid PRIMARY KEY,
    webhook_id uuid NOT NULL REFERENCES repository_webhooks (id) ON DELETE CASCADE,
    source_event_id uuid NOT NULL REFERENCES outbox_events (id) ON DELETE CASCADE,
    event_name varchar(160) NOT NULL,
    request_body bytea NOT NULL,
    status varchar(24) NOT NULL DEFAULT 'queued',
    attempt_count integer NOT NULL DEFAULT 0,
    automatic_attempts integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    lease_owner uuid,
    lease_expires_at timestamptz,
    response_status integer,
    response_body text NOT NULL DEFAULT '',
    last_error text NOT NULL DEFAULT '',
    delivered_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT webhook_deliveries_source_unique UNIQUE (webhook_id, source_event_id),
    CONSTRAINT webhook_deliveries_status_check CHECK (
        status IN ('queued', 'delivering', 'succeeded', 'failed', 'exhausted')
    ),
    CONSTRAINT webhook_deliveries_attempt_count_check CHECK (
        attempt_count >= 0 AND automatic_attempts >= 0 AND automatic_attempts <= attempt_count
    ),
    CONSTRAINT webhook_deliveries_response_status_check CHECK (
        response_status IS NULL OR response_status BETWEEN 100 AND 599
    ),
    CONSTRAINT webhook_deliveries_lease_check CHECK (
        (status = 'delivering' AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)
        OR (status <> 'delivering' AND lease_owner IS NULL AND lease_expires_at IS NULL)
    )
);

CREATE INDEX webhook_deliveries_claim_idx
    ON webhook_deliveries (next_attempt_at, created_at, id)
    WHERE status IN ('queued', 'failed', 'delivering');

CREATE INDEX webhook_deliveries_history_idx
    ON webhook_deliveries (webhook_id, created_at DESC, id DESC);

CREATE TABLE webhook_delivery_attempts (
    id uuid PRIMARY KEY,
    delivery_id uuid NOT NULL REFERENCES webhook_deliveries (id) ON DELETE CASCADE,
    attempt_number integer NOT NULL,
    started_at timestamptz NOT NULL,
    finished_at timestamptz NOT NULL,
    response_status integer,
    response_body text NOT NULL DEFAULT '',
    error_message text NOT NULL DEFAULT '',
    CONSTRAINT webhook_delivery_attempts_number_unique UNIQUE (delivery_id, attempt_number),
    CONSTRAINT webhook_delivery_attempts_number_check CHECK (attempt_number > 0),
    CONSTRAINT webhook_delivery_attempts_time_check CHECK (finished_at >= started_at),
    CONSTRAINT webhook_delivery_attempts_response_status_check CHECK (
        response_status IS NULL OR response_status BETWEEN 100 AND 599
    )
);

CREATE INDEX webhook_delivery_attempts_delivery_idx
    ON webhook_delivery_attempts (delivery_id, attempt_number DESC);
