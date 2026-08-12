ALTER TABLE notifications
    ADD COLUMN IF NOT EXISTS in_app_enabled boolean NOT NULL DEFAULT true,
    ADD COLUMN IF NOT EXISTS email_enabled boolean NOT NULL DEFAULT false;

CREATE TABLE notification_email_deliveries (
    id uuid PRIMARY KEY,
    notification_id uuid NOT NULL UNIQUE REFERENCES notifications (id) ON DELETE CASCADE,
    status varchar(16) NOT NULL DEFAULT 'queued',
    attempt_count integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    lease_owner uuid,
    lease_expires_at timestamptz,
    sent_at timestamptz,
    last_error varchar(1024) NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT notification_email_deliveries_status_check
        CHECK (status IN ('queued', 'delivering', 'failed', 'sent', 'exhausted', 'cancelled')),
    CONSTRAINT notification_email_deliveries_attempt_count_check
        CHECK (attempt_count >= 0),
    CONSTRAINT notification_email_deliveries_lease_check
        CHECK (
            (status = 'delivering' AND lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)
            OR (status <> 'delivering' AND lease_owner IS NULL AND lease_expires_at IS NULL)
        ),
    CONSTRAINT notification_email_deliveries_sent_check
        CHECK ((status = 'sent' AND sent_at IS NOT NULL) OR (status <> 'sent' AND sent_at IS NULL))
);

CREATE INDEX notification_email_deliveries_claim_idx
    ON notification_email_deliveries (next_attempt_at, created_at, id)
    WHERE status IN ('queued', 'failed', 'delivering');

CREATE INDEX notifications_in_app_recipient_created_idx
    ON notifications (recipient_id, created_at DESC, id DESC)
    WHERE in_app_enabled;
