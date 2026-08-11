ALTER TABLE merge_requests
    ADD COLUMN is_draft boolean NOT NULL DEFAULT false,
    ADD COLUMN draft_changed_at timestamptz,
    ADD COLUMN draft_changed_by uuid REFERENCES users (id) ON DELETE RESTRICT;

ALTER TABLE merge_requests
    ADD CONSTRAINT merge_requests_draft_change_check CHECK (
        (
            (draft_changed_at IS NULL AND draft_changed_by IS NULL)
            OR (draft_changed_at IS NOT NULL AND draft_changed_by IS NOT NULL)
        )
        AND (NOT is_draft OR draft_changed_at IS NOT NULL)
    ),
    ADD CONSTRAINT merge_requests_merged_not_draft_check CHECK (
        state <> 'merged' OR NOT is_draft
    );

CREATE INDEX merge_requests_repository_draft_updated_idx
    ON merge_requests (repository_id, is_draft, updated_at DESC)
    WHERE state = 'open';
