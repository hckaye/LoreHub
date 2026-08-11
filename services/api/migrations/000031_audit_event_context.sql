ALTER TABLE audit_events
    ADD COLUMN actor_username varchar(64),
    ADD COLUMN actor_display_name varchar(160),
    ADD COLUMN repository_owner varchar(64),
    ADD COLUMN repository_slug varchar(128);

UPDATE audit_events event
SET actor_username = account.username,
    actor_display_name = account.display_name
FROM users account
WHERE account.id = event.actor_id;

UPDATE audit_events event
SET repository_owner = organization.slug,
    repository_slug = repository.slug
FROM repositories repository
JOIN organizations organization ON organization.id = repository.organization_id
WHERE repository.id = event.repository_id;

CREATE FUNCTION capture_audit_event_context() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.actor_id IS NOT NULL THEN
        SELECT account.username, account.display_name
        INTO NEW.actor_username, NEW.actor_display_name
        FROM users account
        WHERE account.id = NEW.actor_id;
    END IF;

    IF NEW.repository_id IS NOT NULL THEN
        SELECT organization.slug, repository.slug
        INTO NEW.repository_owner, NEW.repository_slug
        FROM repositories repository
        JOIN organizations organization ON organization.id = repository.organization_id
        WHERE repository.id = NEW.repository_id;
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER audit_events_capture_context
BEFORE INSERT ON audit_events
FOR EACH ROW
EXECUTE FUNCTION capture_audit_event_context();
