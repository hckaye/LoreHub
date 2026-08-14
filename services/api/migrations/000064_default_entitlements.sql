ALTER TABLE entitlements
    DROP CONSTRAINT entitlements_grant_source_check;

ALTER TABLE entitlements
    ADD CONSTRAINT entitlements_grant_source_check CHECK (
        grant_source IN ('admin', 'default', 'migration')
    );
