CREATE TABLE users (
    id uuid PRIMARY KEY,
    username varchar(64) NOT NULL,
    display_name varchar(160) NOT NULL,
    email varchar(320),
    locale varchar(16) NOT NULL DEFAULT 'en',
    status varchar(16) NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_username_unique UNIQUE (username),
    CONSTRAINT users_status_check CHECK (status IN ('active', 'suspended'))
);

CREATE UNIQUE INDEX users_email_unique
    ON users (lower(email))
    WHERE email IS NOT NULL;

CREATE TABLE user_identities (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    issuer text NOT NULL,
    subject text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT user_identities_issuer_subject_unique UNIQUE (issuer, subject)
);

CREATE TABLE organizations (
    id uuid PRIMARY KEY,
    slug varchar(64) NOT NULL,
    display_name varchar(160) NOT NULL,
    description text NOT NULL DEFAULT '',
    visibility varchar(16) NOT NULL DEFAULT 'private',
    created_by uuid NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT organizations_slug_unique UNIQUE (slug),
    CONSTRAINT organizations_visibility_check CHECK (visibility IN ('private', 'internal', 'public'))
);

CREATE TABLE organization_memberships (
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role varchar(16) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, user_id),
    CONSTRAINT organization_memberships_role_check CHECK (role IN ('owner', 'maintainer', 'member'))
);

CREATE TABLE repositories (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    slug varchar(128) NOT NULL,
    display_name varchar(200) NOT NULL,
    description text NOT NULL DEFAULT '',
    visibility varchar(16) NOT NULL DEFAULT 'private',
    lore_repository_id varchar(64) NOT NULL,
    lore_url text NOT NULL,
    default_branch varchar(255) NOT NULL,
    archived_at timestamptz,
    created_by uuid NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT repositories_organization_slug_unique UNIQUE (organization_id, slug),
    CONSTRAINT repositories_lore_repository_id_unique UNIQUE (lore_repository_id),
    CONSTRAINT repositories_lore_url_unique UNIQUE (lore_url),
    CONSTRAINT repositories_visibility_check CHECK (visibility IN ('private', 'internal', 'public'))
);

CREATE TABLE repository_memberships (
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role varchar(16) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository_id, user_id),
    CONSTRAINT repository_memberships_role_check CHECK (role IN ('admin', 'write', 'triage', 'read'))
);

CREATE TABLE repository_counters (
    repository_id uuid PRIMARY KEY REFERENCES repositories (id) ON DELETE CASCADE,
    next_issue_number bigint NOT NULL DEFAULT 1,
    next_merge_request_number bigint NOT NULL DEFAULT 1,
    next_ci_run_number bigint NOT NULL DEFAULT 1
);

CREATE TABLE issues (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    number bigint NOT NULL,
    title varchar(512) NOT NULL,
    body text NOT NULL DEFAULT '',
    state varchar(16) NOT NULL DEFAULT 'open',
    author_id uuid NOT NULL REFERENCES users (id),
    assignee_id uuid REFERENCES users (id),
    closed_by uuid REFERENCES users (id),
    closed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT issues_repository_number_unique UNIQUE (repository_id, number),
    CONSTRAINT issues_state_check CHECK (state IN ('open', 'closed'))
);

CREATE INDEX issues_repository_state_updated_idx
    ON issues (repository_id, state, updated_at DESC);

CREATE TABLE labels (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    name varchar(128) NOT NULL,
    description text NOT NULL DEFAULT '',
    color char(6) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT labels_repository_name_unique UNIQUE (repository_id, name),
    CONSTRAINT labels_color_check CHECK (color ~ '^[0-9A-Fa-f]{6}$')
);

CREATE TABLE issue_labels (
    issue_id uuid NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    label_id uuid NOT NULL REFERENCES labels (id) ON DELETE CASCADE,
    PRIMARY KEY (issue_id, label_id)
);

CREATE TABLE issue_comments (
    id uuid PRIMARY KEY,
    issue_id uuid NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    author_id uuid NOT NULL REFERENCES users (id),
    body text NOT NULL,
    edited_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX issue_comments_issue_created_idx
    ON issue_comments (issue_id, created_at);

CREATE TABLE merge_requests (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    number bigint NOT NULL,
    title varchar(512) NOT NULL,
    body text NOT NULL DEFAULT '',
    state varchar(24) NOT NULL DEFAULT 'open',
    source_branch varchar(255) NOT NULL,
    target_branch varchar(255) NOT NULL,
    source_revision varchar(128) NOT NULL,
    target_revision varchar(128) NOT NULL,
    author_id uuid NOT NULL REFERENCES users (id),
    merged_by uuid REFERENCES users (id),
    merged_revision varchar(128),
    merged_at timestamptz,
    closed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT merge_requests_repository_number_unique UNIQUE (repository_id, number),
    CONSTRAINT merge_requests_state_check CHECK (state IN ('open', 'closed', 'merged')),
    CONSTRAINT merge_requests_distinct_branches_check CHECK (source_branch <> target_branch)
);

CREATE INDEX merge_requests_repository_state_updated_idx
    ON merge_requests (repository_id, state, updated_at DESC);

CREATE TABLE merge_request_reviews (
    id uuid PRIMARY KEY,
    merge_request_id uuid NOT NULL REFERENCES merge_requests (id) ON DELETE CASCADE,
    reviewer_id uuid NOT NULL REFERENCES users (id),
    source_revision varchar(128) NOT NULL,
    decision varchar(24) NOT NULL,
    body text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT merge_request_reviews_revision_reviewer_unique
        UNIQUE (merge_request_id, source_revision, reviewer_id),
    CONSTRAINT merge_request_reviews_decision_check
        CHECK (decision IN ('approved', 'changes_requested', 'commented'))
);

CREATE TABLE branch_rules (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    pattern varchar(255) NOT NULL,
    required_approvals smallint NOT NULL DEFAULT 0,
    require_ci_success boolean NOT NULL DEFAULT true,
    block_direct_push boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT branch_rules_repository_pattern_unique UNIQUE (repository_id, pattern),
    CONSTRAINT branch_rules_required_approvals_check CHECK (required_approvals BETWEEN 0 AND 100)
);

CREATE TABLE repository_branch_states (
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    branch_id varchar(64) NOT NULL,
    branch_name varchar(255) NOT NULL,
    latest_revision varchar(128) NOT NULL,
    observed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository_id, branch_id)
);

CREATE TABLE ci_workflows (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    path text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    last_seen_revision varchar(128) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ci_workflows_repository_path_unique UNIQUE (repository_id, path)
);

CREATE TABLE ci_runs (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    workflow_id uuid REFERENCES ci_workflows (id) ON DELETE SET NULL,
    run_number bigint NOT NULL,
    event_name varchar(64) NOT NULL,
    branch varchar(255) NOT NULL,
    revision varchar(128) NOT NULL,
    actor_id uuid REFERENCES users (id),
    status varchar(24) NOT NULL DEFAULT 'queued',
    conclusion varchar(24),
    event_payload jsonb NOT NULL,
    queued_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    completed_at timestamptz,
    CONSTRAINT ci_runs_repository_run_number_unique UNIQUE (repository_id, run_number),
    CONSTRAINT ci_runs_status_check CHECK (status IN ('queued', 'in_progress', 'completed', 'cancelled')),
    CONSTRAINT ci_runs_conclusion_check CHECK (
        conclusion IS NULL OR conclusion IN ('success', 'failure', 'cancelled', 'skipped', 'timed_out')
    )
);

CREATE INDEX ci_runs_queue_idx
    ON ci_runs (queued_at)
    WHERE status = 'queued';

CREATE TABLE ci_jobs (
    id uuid PRIMARY KEY,
    run_id uuid NOT NULL REFERENCES ci_runs (id) ON DELETE CASCADE,
    name varchar(255) NOT NULL,
    status varchar(24) NOT NULL DEFAULT 'queued',
    conclusion varchar(24),
    runner_labels jsonb NOT NULL DEFAULT '[]',
    attempt integer NOT NULL DEFAULT 1,
    lease_owner varchar(255),
    lease_expires_at timestamptz,
    log_object_key text,
    queued_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    completed_at timestamptz,
    CONSTRAINT ci_jobs_run_name_attempt_unique UNIQUE (run_id, name, attempt),
    CONSTRAINT ci_jobs_status_check CHECK (status IN ('queued', 'in_progress', 'completed', 'cancelled')),
    CONSTRAINT ci_jobs_conclusion_check CHECK (
        conclusion IS NULL OR conclusion IN ('success', 'failure', 'cancelled', 'skipped', 'timed_out')
    )
);

CREATE INDEX ci_jobs_claim_idx
    ON ci_jobs (queued_at)
    WHERE status = 'queued';

CREATE TABLE audit_events (
    id uuid PRIMARY KEY,
    organization_id uuid REFERENCES organizations (id) ON DELETE SET NULL,
    repository_id uuid REFERENCES repositories (id) ON DELETE SET NULL,
    actor_id uuid REFERENCES users (id) ON DELETE SET NULL,
    action varchar(160) NOT NULL,
    target_type varchar(80) NOT NULL,
    target_id text,
    remote_address inet,
    details jsonb NOT NULL DEFAULT '{}',
    occurred_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_events_organization_occurred_idx
    ON audit_events (organization_id, occurred_at DESC);

CREATE TABLE outbox_events (
    id uuid PRIMARY KEY,
    topic varchar(160) NOT NULL,
    event_key text NOT NULL,
    payload jsonb NOT NULL,
    attempts integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    processed_at timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT outbox_events_topic_key_unique UNIQUE (topic, event_key)
);

CREATE INDEX outbox_events_pending_idx
    ON outbox_events (available_at)
    WHERE processed_at IS NULL;
