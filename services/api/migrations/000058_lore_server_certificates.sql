ALTER TABLE lore_servers
    ADD COLUMN hook_certificate_serial varchar(64),
    ADD COLUMN hook_certificate_issued_at timestamptz,
    ADD COLUMN hook_certificate_expires_at timestamptz,
    ADD CONSTRAINT lore_servers_hook_certificate_metadata_check CHECK (
        (hook_certificate_serial IS NULL AND hook_certificate_issued_at IS NULL AND
         hook_certificate_expires_at IS NULL) OR
        (hook_certificate_serial IS NOT NULL AND hook_certificate_serial ~ '^[0-9a-f]{1,64}$' AND
         hook_certificate_issued_at IS NOT NULL AND hook_certificate_expires_at IS NOT NULL AND
         hook_certificate_expires_at > hook_certificate_issued_at)
    );

CREATE UNIQUE INDEX lore_servers_hook_certificate_serial_unique
    ON lore_servers (hook_certificate_serial)
    WHERE hook_certificate_serial IS NOT NULL;
