-- Durable state for the Lore merge lifecycle. Repository file data remains in Lore.

CREATE TABLE merge_operations (
    id uuid PRIMARY KEY,
    merge_request_id uuid NOT NULL REFERENCES merge_requests (id) ON DELETE CASCADE,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    actor_id uuid REFERENCES users (id) ON DELETE SET NULL,
    source_revision varchar(128) NOT NULL,
    target_revision varchar(128) NOT NULL,
    staged_revision varchar(128),
    pushed_revision varchar(128),
    parent_revisions jsonb NOT NULL DEFAULT '[]',
    state varchar(32) NOT NULL DEFAULT 'created',
    conflict_paths jsonb NOT NULL DEFAULT '[]',
    error_code varchar(80),
    error_detail text,
    lease_owner varchar(128),
    lease_expires_at timestamptz,
    version bigint NOT NULL DEFAULT 0,
    started_at timestamptz,
    completed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT merge_operations_request_unique UNIQUE (merge_request_id),
    CONSTRAINT merge_operations_state_check CHECK (
        state IN ('created', 'started', 'conflicts', 'ready_to_push', 'pushing', 'pushed', 'aborted', 'merged')
    )
);

CREATE INDEX merge_operations_lease_idx
    ON merge_operations (lease_expires_at)
    WHERE state NOT IN ('aborted', 'merged');

CREATE INDEX merge_operations_repository_updated_idx
    ON merge_operations (repository_id, updated_at DESC);

CREATE TABLE merge_operation_resolutions (
    operation_id uuid NOT NULL REFERENCES merge_operations (id) ON DELETE CASCADE,
    path varchar(2048) NOT NULL,
    strategy varchar(16) NOT NULL,
    actor_id uuid REFERENCES users (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (operation_id, path),
    CONSTRAINT merge_operation_resolutions_strategy_check CHECK (strategy IN ('mine', 'theirs'))
);

CREATE INDEX merge_operation_resolutions_operation_idx
    ON merge_operation_resolutions (operation_id, updated_at);
