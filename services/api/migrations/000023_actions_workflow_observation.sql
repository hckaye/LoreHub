ALTER TABLE repository_branch_states
    ADD COLUMN workflow_observed_revision varchar(128);

UPDATE repository_branch_states
SET workflow_observed_revision = latest_revision
WHERE latest_revision = repeat('0', 64);
