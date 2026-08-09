-- Collaboration API support indexes.
--
-- The existing schema already covers the unique constraints needed by the
-- collaboration endpoints (issue_labels, labels, branch_rules, merge_request_reviews).
-- This migration adds two secondary indexes that the new list/aggregate queries
-- rely on for efficient lookups that the unique indexes do not cover:
--
--   * issue_labels by label_id: speeds up cascade deletes and "issues for label"
--     lookups. The primary key (issue_id, label_id) cannot serve label_id-only
--     scans efficiently because label_id is the trailing column.
--   * merge_request_reviews by (merge_request_id, created_at): the unique index
--     is on (merge_request_id, source_revision, reviewer_id), which does not
--     help ordering reviews by submission time for the review history endpoint.

CREATE INDEX IF NOT EXISTS issue_labels_label_idx
    ON issue_labels (label_id);

CREATE INDEX IF NOT EXISTS merge_request_reviews_mr_created_idx
    ON merge_request_reviews (merge_request_id, created_at);
