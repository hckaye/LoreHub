CREATE TABLE repository_topics (
    repository_id uuid NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    topic text NOT NULL,
    created_by uuid NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository_id, topic),
    CONSTRAINT repository_topics_value_check CHECK (
        char_length(topic) BETWEEN 1 AND 50
        AND topic ~ '^[a-z0-9]+(-[a-z0-9]+)*$'
    )
);

CREATE INDEX repository_topics_topic_repository_idx
    ON repository_topics (topic, repository_id);

CREATE INDEX repository_topics_topic_trgm_idx
    ON repository_topics USING gin (topic gin_trgm_ops);

CREATE FUNCTION enforce_repository_topic_limit()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM 1 FROM repositories WHERE id = NEW.repository_id FOR UPDATE;
    IF NOT EXISTS (
        SELECT 1 FROM repository_topics
        WHERE repository_id = NEW.repository_id AND topic = NEW.topic
    ) AND (SELECT count(*) FROM repository_topics WHERE repository_id = NEW.repository_id) >= 20 THEN
        RAISE EXCEPTION 'a repository can have at most 20 topics' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER repository_topics_limit
BEFORE INSERT ON repository_topics
FOR EACH ROW EXECUTE FUNCTION enforce_repository_topic_limit();
