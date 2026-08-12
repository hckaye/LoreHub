CREATE OR REPLACE FUNCTION update_issue_comment_count()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        UPDATE issues SET comment_count = comment_count + 1 WHERE id = NEW.issue_id;
        RETURN NEW;
    END IF;
    IF TG_OP = 'DELETE' THEN
        UPDATE issues SET comment_count = comment_count - 1 WHERE id = OLD.issue_id;
        RETURN OLD;
    END IF;
    IF NEW.issue_id IS DISTINCT FROM OLD.issue_id THEN
        UPDATE issues SET comment_count = comment_count - 1 WHERE id = OLD.issue_id;
        UPDATE issues SET comment_count = comment_count + 1 WHERE id = NEW.issue_id;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER issue_comments_count_change ON issue_comments;

CREATE TRIGGER issue_comments_count_change
AFTER INSERT OR DELETE OR UPDATE OF issue_id ON issue_comments
FOR EACH ROW EXECUTE FUNCTION update_issue_comment_count();

CREATE OR REPLACE FUNCTION update_merge_request_comment_count()
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
    IF TG_OP = 'DELETE' THEN
        UPDATE merge_requests
        SET comment_count = comment_count - 1
        WHERE id = OLD.merge_request_id;
        RETURN OLD;
    END IF;
    IF NEW.merge_request_id IS DISTINCT FROM OLD.merge_request_id THEN
        UPDATE merge_requests
        SET comment_count = comment_count - 1
        WHERE id = OLD.merge_request_id;
        UPDATE merge_requests
        SET comment_count = comment_count + 1
        WHERE id = NEW.merge_request_id;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER merge_request_comments_count_change ON merge_request_comments;

CREATE TRIGGER merge_request_comments_count_change
AFTER INSERT OR DELETE OR UPDATE OF merge_request_id ON merge_request_comments
FOR EACH ROW EXECUTE FUNCTION update_merge_request_comment_count();
