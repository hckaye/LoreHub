CREATE TABLE entitlements (
    organization_id uuid REFERENCES organizations (id) ON DELETE CASCADE,
    user_id uuid REFERENCES users (id) ON DELETE CASCADE,
    feature varchar(32) NOT NULL,
    granted_by uuid REFERENCES users (id) ON DELETE SET NULL,
    grant_source varchar(16) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    CONSTRAINT entitlements_subject_check CHECK (
        (organization_id IS NOT NULL) <> (user_id IS NOT NULL)
    ),
    CONSTRAINT entitlements_feature_check CHECK (
        feature IN ('hosted_lore_server', 'hosted_runners')
    ),
    CONSTRAINT entitlements_grant_source_check CHECK (
        grant_source IN ('admin', 'migration')
    ),
    CONSTRAINT entitlements_revoked_at_check CHECK (
        revoked_at IS NULL OR revoked_at >= created_at
    )
);

CREATE UNIQUE INDEX entitlements_active_subject_feature_unique
    ON entitlements (organization_id, user_id, feature) NULLS NOT DISTINCT
    WHERE revoked_at IS NULL;

INSERT INTO entitlements (organization_id, feature, grant_source)
SELECT organization.id, feature.name, 'migration'
FROM organizations organization
CROSS JOIN (VALUES ('hosted_lore_server'), ('hosted_runners')) AS feature (name);
