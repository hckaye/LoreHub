-- Organization, repository, and environment scoped Actions variables and secrets.
-- Secret plaintext never enters PostgreSQL; AES-256-GCM material is stored with its key ID.

CREATE UNIQUE INDEX IF NOT EXISTS repositories_id_organization_unique_idx
    ON repositories (id, organization_id);

CREATE TABLE actions_execution_context_entries (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    repository_id uuid,
    environment_name varchar(128),
    scope_kind varchar(16) NOT NULL,
    value_kind varchar(16) NOT NULL,
    name varchar(100) NOT NULL,
    variable_value text,
    encrypted_value bytea,
    nonce bytea,
    key_id varchar(128),
    updated_by uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT actions_execution_context_repository_boundary_fk
        FOREIGN KEY (repository_id, organization_id)
        REFERENCES repositories (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT actions_execution_context_scope_check CHECK (
        (scope_kind = 'organization' AND repository_id IS NULL AND environment_name IS NULL)
        OR (scope_kind = 'repository' AND repository_id IS NOT NULL AND environment_name IS NULL)
        OR (
            scope_kind = 'environment'
            AND repository_id IS NOT NULL
            AND environment_name IS NOT NULL
            AND char_length(environment_name) BETWEEN 1 AND 128
            AND environment_name = btrim(environment_name)
            AND environment_name !~ '[[:cntrl:]]'
        )
    ),
    CONSTRAINT actions_execution_context_value_kind_check
        CHECK (value_kind IN ('variable', 'secret')),
    CONSTRAINT actions_execution_context_name_check CHECK (
        name ~ '^[A-Za-z_][A-Za-z0-9_]{0,99}$'
        AND name = upper(name)
        AND upper(name) !~ '^(GITHUB_|RUNNER_|ACTIONS_|DOCKER_)'
        AND upper(name) NOT IN ('CI', 'PATH', 'HOME', 'HTTP_PROXY', 'HTTPS_PROXY', 'NO_PROXY')
    ),
    CONSTRAINT actions_execution_context_value_check CHECK (
        (
            value_kind = 'variable'
            AND variable_value IS NOT NULL
            AND char_length(variable_value) <= 1048576
            AND encrypted_value IS NULL
            AND nonce IS NULL
            AND key_id IS NULL
        )
        OR (
            value_kind = 'secret'
            AND variable_value IS NULL
            AND encrypted_value IS NOT NULL
            AND octet_length(encrypted_value) BETWEEN 16 AND 1048592
            AND nonce IS NOT NULL
            AND octet_length(nonce) = 12
            AND key_id ~ '^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$'
        )
    ),
    CONSTRAINT actions_execution_context_timestamps_check CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX actions_execution_context_organization_name_idx
    ON actions_execution_context_entries (organization_id, value_kind, lower(name))
    WHERE scope_kind = 'organization';

CREATE UNIQUE INDEX actions_execution_context_repository_name_idx
    ON actions_execution_context_entries (repository_id, value_kind, lower(name))
    WHERE scope_kind = 'repository';

CREATE UNIQUE INDEX actions_execution_context_environment_name_idx
    ON actions_execution_context_entries (
        repository_id, lower(environment_name), value_kind, lower(name)
    ) WHERE scope_kind = 'environment';

CREATE INDEX actions_execution_context_resolver_idx
    ON actions_execution_context_entries (organization_id, repository_id, scope_kind);
