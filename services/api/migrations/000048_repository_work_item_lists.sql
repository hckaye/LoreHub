ALTER TABLE issues
    ADD COLUMN comment_count bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT issues_comment_count_check CHECK (comment_count >= 0);

ALTER TABLE merge_requests
    ADD COLUMN comment_count bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT merge_requests_comment_count_check CHECK (comment_count >= 0);

UPDATE issues issue
SET comment_count = (
    SELECT COUNT(*) FROM issue_comments comment WHERE comment.issue_id = issue.id
);

UPDATE merge_requests request
SET comment_count = (
    SELECT COUNT(*)
    FROM merge_request_comments comment
    WHERE comment.merge_request_id = request.id
);

CREATE FUNCTION update_issue_comment_count()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        UPDATE issues SET comment_count = comment_count + 1 WHERE id = NEW.issue_id;
        RETURN NEW;
    END IF;
    UPDATE issues SET comment_count = comment_count - 1 WHERE id = OLD.issue_id;
    RETURN OLD;
END;
$$;

CREATE TRIGGER issue_comments_count_change
AFTER INSERT OR DELETE ON issue_comments
FOR EACH ROW EXECUTE FUNCTION update_issue_comment_count();

CREATE FUNCTION update_merge_request_comment_count()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        UPDATE merge_requests
        SET comment_count = comment_count + 1
        WHERE id = NEW.merge_request_id;
        RETURN NEW;
    END IF;
    UPDATE merge_requests
    SET comment_count = comment_count - 1
    WHERE id = OLD.merge_request_id;
    RETURN OLD;
END;
$$;

CREATE TRIGGER merge_request_comments_count_change
AFTER INSERT OR DELETE ON merge_request_comments
FOR EACH ROW EXECUTE FUNCTION update_merge_request_comment_count();

CREATE INDEX issues_repository_state_updated_page_idx
    ON issues (repository_id, state, updated_at DESC, id DESC);

CREATE INDEX issues_repository_state_comments_page_idx
    ON issues (repository_id, state, comment_count DESC, id DESC);

CREATE INDEX merge_requests_repository_state_updated_page_idx
    ON merge_requests (repository_id, state, updated_at DESC, id DESC);

CREATE INDEX merge_requests_repository_state_comments_page_idx
    ON merge_requests (repository_id, state, comment_count DESC, id DESC);

CREATE INDEX merge_requests_repository_source_updated_idx
    ON merge_requests (repository_id, source_branch, updated_at DESC, id DESC);

CREATE INDEX merge_requests_repository_target_updated_idx
    ON merge_requests (repository_id, target_branch, updated_at DESC, id DESC);

CREATE INDEX users_lower_username_idx ON users (lower(username));

CREATE INDEX labels_repository_lower_name_idx
    ON labels (repository_id, lower(name), id);
