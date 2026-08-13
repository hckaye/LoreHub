CREATE TABLE work_item_events (
    id uuid PRIMARY KEY,
    repository_id uuid NOT NULL REFERENCES repositories (id) ON DELETE CASCADE,
    item_kind varchar(16) NOT NULL,
    item_id uuid NOT NULL,
    actor varchar(255) NOT NULL,
    event_kind varchar(32) NOT NULL,
    payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT work_item_events_item_kind_check CHECK (
        item_kind IN ('issue', 'merge_request')
    ),
    CONSTRAINT work_item_events_event_kind_check CHECK (
        event_kind IN (
            'closed', 'reopened', 'labeled', 'unlabeled', 'assigned', 'unassigned',
            'milestoned', 'demilestoned', 'retitled', 'merged', 'review_requested',
            'draft_ready'
        )
    ),
    CONSTRAINT work_item_events_payload_check CHECK (jsonb_typeof(payload) = 'object')
);

CREATE INDEX work_item_events_timeline_idx
    ON work_item_events (repository_id, item_kind, item_id, created_at, id);
