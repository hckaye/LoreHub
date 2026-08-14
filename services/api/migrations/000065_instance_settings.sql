CREATE TABLE instance_settings (
    key text PRIMARY KEY,
    value jsonb NOT NULL,
    updated_by text,
    updated_at timestamptz NOT NULL DEFAULT now()
);
