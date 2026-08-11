package collab

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

// ListIssueComments returns a paginated, chronologically ordered comment list
// for an issue. The cursor is a simple offset string.
func (s *store) ListIssueComments(
	ctx context.Context,
	repoID string,
	number int64,
	page Page,
) (Result[IssueComment], error) {
	var issueExists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM issues WHERE repository_id = $1 AND number = $2)
	`, repoID, number).Scan(&issueExists); err != nil {
		return Result[IssueComment]{}, fmt.Errorf("check issue for comments: %w", err)
	}
	if !issueExists {
		return Result[IssueComment]{}, platform.ErrNotFound
	}
	offset, err := pageOffset(page)
	if err != nil {
		return Result[IssueComment]{}, err
	}
	limit := page.Limit
	if limit < 1 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.issue_id, author.username, c.author_id, c.body,
		       c.created_at, c.edited_at
		FROM issue_comments c
		JOIN issues i ON i.id = c.issue_id
		JOIN users author ON author.id = c.author_id
		WHERE i.repository_id = $1 AND i.number = $2
		ORDER BY c.created_at ASC, c.id ASC
		LIMIT $3 OFFSET $4
	`, repoID, number, limit+1, offset)
	if err != nil {
		return Result[IssueComment]{}, fmt.Errorf("list issue comments: %w", err)
	}
	defer rows.Close()
	comments, err := scanComments(rows)
	if err != nil {
		return Result[IssueComment]{}, err
	}
	return paginate(comments, limit, offset), nil
}

func scanComments(rows pgx.Rows) ([]IssueComment, error) {
	comments := make([]IssueComment, 0)
	for rows.Next() {
		var comment IssueComment
		if err := rows.Scan(
			&comment.ID,
			&comment.IssueID,
			&comment.Author,
			&comment.AuthorID,
			&comment.Body,
			&comment.CreatedAt,
			&comment.EditedAt,
		); err != nil {
			return nil, fmt.Errorf("scan issue comment: %w", err)
		}
		comments = append(comments, comment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate issue comments: %w", err)
	}
	return comments, nil
}

// CreateIssueComment appends a comment for an actor who can read the repository
// and transactionally records the issue timestamp, audit and outbox events.
func (s *store) CreateIssueComment(
	ctx context.Context,
	actor platform.User,
	repoID string,
	number int64,
	body string,
) (IssueComment, error) {
	orgID, err := s.repoOrgID(ctx, repoID)
	if err != nil {
		return IssueComment{}, err
	}
	access, err := s.permFromRef(ctx, actor, repoID, orgID)
	if err != nil {
		return IssueComment{}, err
	}
	if !access.AtLeast(PermRead) {
		return IssueComment{}, platform.ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return IssueComment{}, fmt.Errorf("begin comment transaction: %w", err)
	}
	defer rollback(ctx, tx)

	var issueID string
	err = tx.QueryRow(ctx, `
		SELECT i.id, r.organization_id
		FROM issues i
		JOIN repositories r ON r.id = i.repository_id
		WHERE i.repository_id = $1 AND i.number = $2
	`, repoID, number).Scan(&issueID, &orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return IssueComment{}, platform.ErrNotFound
	}
	if err != nil {
		return IssueComment{}, fmt.Errorf("find issue for comment: %w", err)
	}

	comment := IssueComment{
		ID:        uuidArg(),
		IssueID:   issueID,
		Author:    actor.Username,
		AuthorID:  actor.ID,
		Body:      body,
		CreatedAt: nowUTC(),
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO issue_comments (id, issue_id, author_id, body, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, comment.ID, issueID, actor.ID, comment.Body, comment.CreatedAt)
	if err != nil {
		return IssueComment{}, translateConstraintError("create comment", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE issues SET updated_at = $2 WHERE id = $1
	`, issueID, comment.CreatedAt); err != nil {
		return IssueComment{}, fmt.Errorf("bump issue updated_at: %w", err)
	}
	if err := insertAudit(ctx, tx, actor.ID, orgID, repoID,
		"issue_comment.create", "issue_comment", comment.ID); err != nil {
		return IssueComment{}, err
	}
	if err := insertOutbox(ctx, tx, "issue_comment.created", comment.ID, comment); err != nil {
		return IssueComment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return IssueComment{}, fmt.Errorf("commit comment transaction: %w", err)
	}
	return comment, nil
}

// UpdateIssueComment edits a comment body. The author or a triage+ actor may
// edit it; the original author and created_at are preserved and edited_at is set.
func (s *store) UpdateIssueComment(
	ctx context.Context,
	actor platform.User,
	repoID string,
	issueNumber int64,
	commentID string,
	body string,
) (IssueComment, error) {
	existing, err := s.findCommentForMutation(ctx, repoID, issueNumber, commentID)
	if err != nil {
		return IssueComment{}, err
	}
	access, err := s.permFromRef(ctx, actor, existing.RepoID, existing.OrgID)
	if err != nil {
		return IssueComment{}, err
	}
	if actor.ID != existing.AuthorID && !access.AtLeast(PermTriage) {
		return IssueComment{}, platform.ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return IssueComment{}, fmt.Errorf("begin comment edit: %w", err)
	}
	defer rollback(ctx, tx)

	editedAt := nowUTC()
	tag, err := tx.Exec(ctx, `
		UPDATE issue_comments SET body = $3, edited_at = $4
		WHERE id = $1 AND issue_id = $2
	`, commentID, existing.IssueID, body, editedAt)
	if err != nil {
		return IssueComment{}, translateConstraintError("update comment", err)
	}
	if tag.RowsAffected() == 0 {
		return IssueComment{}, platform.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
		UPDATE issues SET updated_at = $2 WHERE id = $1
	`, existing.IssueID, editedAt); err != nil {
		return IssueComment{}, fmt.Errorf("bump issue updated_at: %w", err)
	}
	if err := insertAudit(ctx, tx, actor.ID, existing.OrgID, existing.RepoID,
		"issue_comment.update", "issue_comment", commentID); err != nil {
		return IssueComment{}, err
	}
	updated := existing.IssueComment
	updated.Body = body
	updated.EditedAt = &editedAt
	if err := insertOutbox(ctx, tx, "issue_comment.updated", commentID+":"+uuidArg(), updated); err != nil {
		return IssueComment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return IssueComment{}, fmt.Errorf("commit comment edit: %w", err)
	}
	return updated, nil
}

// DeleteIssueComment removes a comment. The author or a triage+ actor may
// delete it; the issue's updated_at is bumped transactionally.
func (s *store) DeleteIssueComment(
	ctx context.Context,
	actor platform.User,
	repoID string,
	issueNumber int64,
	commentID string,
) error {
	existing, err := s.findCommentForMutation(ctx, repoID, issueNumber, commentID)
	if err != nil {
		return err
	}
	access, err := s.permFromRef(ctx, actor, existing.RepoID, existing.OrgID)
	if err != nil {
		return err
	}
	if actor.ID != existing.AuthorID && !access.AtLeast(PermTriage) {
		return platform.ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin comment delete: %w", err)
	}
	defer rollback(ctx, tx)

	tag, err := tx.Exec(ctx, `
		DELETE FROM issue_comments WHERE id = $1 AND issue_id = $2
	`, commentID, existing.IssueID)
	if err != nil {
		return translateConstraintError("delete comment", err)
	}
	if tag.RowsAffected() == 0 {
		return platform.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
		UPDATE issues SET updated_at = $2 WHERE id = $1
	`, existing.IssueID, nowUTC()); err != nil {
		return fmt.Errorf("bump issue updated_at: %w", err)
	}
	if err := insertAudit(ctx, tx, actor.ID, existing.OrgID, existing.RepoID,
		"issue_comment.delete", "issue_comment", commentID); err != nil {
		return err
	}
	if err := insertOutbox(ctx, tx, "issue_comment.deleted", commentID+":"+uuidArg(), existing.IssueComment); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit comment delete: %w", err)
	}
	return nil
}

type commentRef struct {
	IssueComment
	RepoID string
	OrgID  string
}

func (s *store) findCommentForMutation(
	ctx context.Context,
	repoID string,
	issueNumber int64,
	commentID string,
) (commentRef, error) {
	var ref commentRef
	err := s.pool.QueryRow(ctx, `
		SELECT c.id, c.issue_id, author.username, c.author_id, c.body,
		       c.created_at, c.edited_at, r.id, r.organization_id
		FROM issue_comments c
		JOIN users author ON author.id = c.author_id
		JOIN issues i ON i.id = c.issue_id
		JOIN repositories r ON r.id = i.repository_id
		WHERE c.id = $1 AND r.id = $2 AND i.number = $3
	`, commentID, repoID, issueNumber).Scan(
		&ref.ID, &ref.IssueID, &ref.Author, &ref.AuthorID, &ref.Body,
		&ref.CreatedAt, &ref.EditedAt, &ref.RepoID, &ref.OrgID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return commentRef{}, platform.ErrNotFound
	}
	if err != nil {
		return commentRef{}, fmt.Errorf("find comment: %w", err)
	}
	return ref, nil
}

func pageOffset(page Page) (int, error) {
	if page.Cursor == "" {
		return 0, nil
	}
	return parseCursor(page.Cursor)
}

func paginate[T any](items []T, limit int, offset int) Result[T] {
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	result := Result[T]{Items: items, HasMore: hasMore}
	if hasMore {
		result.NextCursor = encodeCursor(offset, limit, limit)
	}
	return result
}
