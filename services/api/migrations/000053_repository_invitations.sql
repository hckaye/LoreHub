CREATE TABLE repository_invitations (
    id uuid PRIMARY KEY,
    organization_id uuid NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    repository_id uuid NOT NULL,
    invitee_user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    invited_by uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    role varchar(16) NOT NULL,
    status varchar(16) NOT NULL DEFAULT 'pending',
    expires_at timestamptz NOT NULL,
    responded_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT repository_invitations_repository_boundary_fk
        FOREIGN KEY (repository_id, organization_id)
        REFERENCES repositories (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT repository_invitations_distinct_users_check
        CHECK (invitee_user_id <> invited_by),
    CONSTRAINT repository_invitations_role_check
        CHECK (role IN ('admin', 'maintain', 'write', 'triage', 'read')),
    CONSTRAINT repository_invitations_status_check
        CHECK (status IN ('pending', 'accepted', 'declined', 'revoked', 'expired')),
    CONSTRAINT repository_invitations_expiry_check
        CHECK (expires_at > created_at AND expires_at <= created_at + interval '30 days'),
    CONSTRAINT repository_invitations_response_check CHECK (
        (status = 'pending' AND responded_at IS NULL)
        OR (status <> 'pending' AND responded_at IS NOT NULL)
    ),
    CONSTRAINT repository_invitations_timestamps_check CHECK (
        updated_at >= created_at
        AND (responded_at IS NULL OR responded_at BETWEEN created_at AND updated_at)
    )
);

CREATE UNIQUE INDEX repository_invitations_pending_user_idx
    ON repository_invitations (repository_id, invitee_user_id)
    WHERE status = 'pending';

CREATE INDEX repository_invitations_repository_created_idx
    ON repository_invitations (repository_id, created_at DESC, id DESC);

CREATE INDEX repository_invitations_invitee_created_idx
    ON repository_invitations (invitee_user_id, created_at DESC, id DESC);
