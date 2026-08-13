CREATE TABLE lore_servers (
    id uuid PRIMARY KEY,
    instance_scope boolean NOT NULL DEFAULT false,
    organization_id uuid REFERENCES organizations (id) ON DELETE CASCADE,
    user_id uuid REFERENCES users (id) ON DELETE CASCADE,
    name varchar(160) NOT NULL,
    public_url text NOT NULL,
    status varchar(16) NOT NULL DEFAULT 'active',
    credential_digest bytea,
    credential_key_id varchar(128),
    credential_expires_at timestamptz,
    credential_last_used_at timestamptz,
    revoked_at timestamptz,
    lore_build_version varchar(64) NOT NULL DEFAULT '',
    last_seen_at timestamptz,
    health_metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT lore_servers_scope_check CHECK (
        (CASE WHEN instance_scope THEN 1 ELSE 0 END) +
        (CASE WHEN organization_id IS NOT NULL THEN 1 ELSE 0 END) +
        (CASE WHEN user_id IS NOT NULL THEN 1 ELSE 0 END) = 1
    ),
    CONSTRAINT lore_servers_name_check CHECK (
        name = btrim(name) AND length(name) BETWEEN 1 AND 160 AND name !~ '[[:cntrl:]]'
    ),
    CONSTRAINT lore_servers_public_url_check CHECK (length(public_url) BETWEEN 1 AND 2048),
    CONSTRAINT lore_servers_status_check CHECK (status IN ('active', 'revoked')),
    CONSTRAINT lore_servers_credential_check CHECK (
        (credential_digest IS NULL AND credential_key_id IS NULL AND credential_expires_at IS NULL) OR
        (credential_digest IS NOT NULL AND octet_length(credential_digest) = 32 AND
         credential_key_id IS NOT NULL AND credential_expires_at IS NOT NULL)
    ),
    CONSTRAINT lore_servers_credential_expiry_check CHECK (
        credential_expires_at IS NULL OR
        (credential_expires_at > created_at AND
         credential_expires_at <= created_at + interval '1830 days')
    ),
    CONSTRAINT lore_servers_credential_last_used_check CHECK (
        credential_last_used_at IS NULL OR credential_last_used_at >= created_at
    ),
    CONSTRAINT lore_servers_revoked_at_check CHECK (
        revoked_at IS NULL OR revoked_at >= created_at
    ),
    CONSTRAINT lore_servers_status_revocation_check CHECK (
        (status = 'active' AND revoked_at IS NULL) OR
        (status = 'revoked' AND revoked_at IS NOT NULL)
    ),
    CONSTRAINT lore_servers_health_metadata_check CHECK (jsonb_typeof(health_metadata) = 'object')
);

CREATE UNIQUE INDEX lore_servers_single_instance_idx
    ON lore_servers (instance_scope)
    WHERE instance_scope;

CREATE UNIQUE INDEX lore_servers_active_public_url_unique
    ON lore_servers (lower(public_url))
    WHERE revoked_at IS NULL;

CREATE INDEX lore_servers_organization_idx
    ON lore_servers (organization_id, status, created_at DESC)
    WHERE organization_id IS NOT NULL;

CREATE UNIQUE INDEX lore_servers_active_credential_digest_unique
    ON lore_servers (credential_key_id, credential_digest)
    WHERE credential_digest IS NOT NULL AND revoked_at IS NULL;

CREATE TABLE lore_server_registration_tokens (
    id uuid PRIMARY KEY,
    instance_scope boolean NOT NULL DEFAULT false,
    organization_id uuid REFERENCES organizations (id) ON DELETE CASCADE,
    user_id uuid REFERENCES users (id) ON DELETE CASCADE,
    token_digest bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    created_by uuid NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT lore_server_registration_tokens_scope_check CHECK (
        (CASE WHEN instance_scope THEN 1 ELSE 0 END) +
        (CASE WHEN organization_id IS NOT NULL THEN 1 ELSE 0 END) +
        (CASE WHEN user_id IS NOT NULL THEN 1 ELSE 0 END) = 1
    ),
    CONSTRAINT lore_server_registration_tokens_expiry_check CHECK (expires_at > created_at),
    CONSTRAINT lore_server_registration_tokens_max_age_check CHECK (
        expires_at <= created_at + interval '1 hour'
    ),
    CONSTRAINT lore_server_registration_tokens_digest_check CHECK (octet_length(token_digest) = 32),
    CONSTRAINT lore_server_registration_tokens_consumed_at_check CHECK (
        consumed_at IS NULL OR (consumed_at >= created_at AND consumed_at < expires_at)
    ),
    CONSTRAINT lore_server_registration_tokens_digest_unique UNIQUE (token_digest)
);

CREATE INDEX lore_server_registration_tokens_scope_idx
    ON lore_server_registration_tokens (organization_id, expires_at DESC)
    WHERE organization_id IS NOT NULL AND consumed_at IS NULL;

ALTER TABLE organizations
    ADD COLUMN default_lore_server_id uuid REFERENCES lore_servers (id) ON DELETE SET NULL;

ALTER TABLE repositories
    ADD COLUMN lore_server_id uuid REFERENCES lore_servers (id);

CREATE INDEX repositories_lore_server_idx ON repositories (lore_server_id);
