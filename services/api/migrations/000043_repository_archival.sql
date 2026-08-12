ALTER TABLE repositories
    ADD COLUMN IF NOT EXISTS archived_by uuid REFERENCES users (id);

UPDATE repositories
SET archived_by = created_by
WHERE archived_at IS NOT NULL AND archived_by IS NULL;

ALTER TABLE repositories
    DROP CONSTRAINT IF EXISTS repositories_archive_actor_check;

ALTER TABLE repositories
    ADD CONSTRAINT repositories_archive_actor_check CHECK (
        (archived_at IS NULL AND archived_by IS NULL)
        OR (archived_at IS NOT NULL AND archived_by IS NOT NULL)
    );

CREATE INDEX IF NOT EXISTS repositories_organization_archived_idx
    ON repositories (organization_id, archived_at DESC)
    WHERE archived_at IS NOT NULL;
