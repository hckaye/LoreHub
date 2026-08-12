package collab

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type MergeRequestConversationStore interface {
	ListMergeRequestComments(
		context.Context, string, int64, Page,
	) (Result[MergeRequestComment], error)
	CreateMergeRequestComment(
		context.Context, platform.User, string, int64, string,
	) (MergeRequestComment, error)
	UpdateMergeRequestComment(
		context.Context, platform.User, string, int64, string, string,
	) (MergeRequestComment, error)
	DeleteMergeRequestComment(context.Context, platform.User, string, int64, string) error
}

func (s *store) ListMergeRequestComments(
	ctx context.Context,
	repoID string,
	number int64,
	page Page,
) (Result[MergeRequestComment], error) {
	offset, err := pageOffset(page)
	if err != nil {
		return Result[MergeRequestComment]{}, err
	}
	limit := page.Limit
	if limit < 1 {
		limit = defaultPageLimit
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return Result[MergeRequestComment]{}, fmt.Errorf("begin pull request comment list: %w", err)
	}
	defer rollback(ctx, tx)
	var totalCount int64
	err = tx.QueryRow(ctx, `
		SELECT merge_request.comment_count
		FROM merge_requests merge_request
		JOIN repositories repository
		  ON repository.id = merge_request.repository_id AND repository.lifecycle_state = 'active'
		JOIN organizations organization
		  ON organization.id = repository.organization_id AND organization.active
		WHERE merge_request.repository_id = $1 AND merge_request.number = $2
	`, repoID, number).Scan(&totalCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result[MergeRequestComment]{}, platform.ErrNotFound
	}
	if err != nil {
		return Result[MergeRequestComment]{}, fmt.Errorf("count pull request comments: %w", err)
	}
	rows, err := tx.Query(ctx, `
		SELECT comment.id, comment.merge_request_id, author.username, comment.author_id,
		       comment.body, comment.created_at, comment.edited_at
		FROM merge_request_comments comment
		JOIN merge_requests merge_request ON merge_request.id = comment.merge_request_id
		JOIN users author ON author.id = comment.author_id
		WHERE merge_request.repository_id = $1 AND merge_request.number = $2
		ORDER BY comment.created_at, comment.id
		LIMIT $3 OFFSET $4
	`, repoID, number, limit+1, offset)
	if err != nil {
		return Result[MergeRequestComment]{}, fmt.Errorf("list pull request comments: %w", err)
	}
	defer rows.Close()
	comments := make([]MergeRequestComment, 0)
	for rows.Next() {
		var comment MergeRequestComment
		if err := rows.Scan(
			&comment.ID, &comment.MergeRequestID, &comment.Author, &comment.AuthorID,
			&comment.Body, &comment.CreatedAt, &comment.EditedAt,
		); err != nil {
			return Result[MergeRequestComment]{}, fmt.Errorf("scan pull request comment: %w", err)
		}
		comments = append(comments, comment)
	}
	if err := rows.Err(); err != nil {
		return Result[MergeRequestComment]{}, fmt.Errorf("list pull request comments: %w", err)
	}
	result := paginate(comments, limit, offset)
	result.TotalCount = &totalCount
	if err := tx.Commit(ctx); err != nil {
		return Result[MergeRequestComment]{}, fmt.Errorf("commit pull request comment list: %w", err)
	}
	return result, nil
}

func (s *store) CreateMergeRequestComment(
	ctx context.Context,
	actor platform.User,
	repoID string,
	number int64,
	body string,
) (MergeRequestComment, error) {
	orgID, err := s.repoOrgID(ctx, repoID)
	if err != nil {
		return MergeRequestComment{}, err
	}
	access, err := s.permFromRef(ctx, actor, repoID, orgID)
	if err != nil {
		return MergeRequestComment{}, err
	}
	if !access.AtLeast(PermRead) {
		return MergeRequestComment{}, platform.ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return MergeRequestComment{}, fmt.Errorf("begin pull request comment: %w", err)
	}
	defer rollback(ctx, tx)
	var mergeRequestID string
	err = tx.QueryRow(ctx, `
		SELECT merge_request.id, repository.organization_id
		FROM merge_requests merge_request
		JOIN repositories repository ON repository.id = merge_request.repository_id
		WHERE merge_request.repository_id = $1 AND merge_request.number = $2
	`, repoID, number).Scan(&mergeRequestID, &orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return MergeRequestComment{}, platform.ErrNotFound
	}
	if err != nil {
		return MergeRequestComment{}, fmt.Errorf("find pull request for comment: %w", err)
	}
	comment := MergeRequestComment{
		ID: uuidArg(), MergeRequestID: mergeRequestID, Author: actor.Username,
		AuthorID: actor.ID, Body: body, CreatedAt: nowUTC(),
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO merge_request_comments (
			id, merge_request_id, author_id, body, created_at
		) VALUES ($1, $2, $3, $4, $5)
	`, comment.ID, mergeRequestID, actor.ID, comment.Body, comment.CreatedAt)
	if err != nil {
		return MergeRequestComment{}, translateConstraintError("create pull request comment", err)
	}
	if err := touchMergeRequest(ctx, tx, mergeRequestID, comment.CreatedAt); err != nil {
		return MergeRequestComment{}, err
	}
	if err := insertAudit(ctx, tx, actor.ID, orgID, repoID,
		"merge_request_comment.create", "merge_request_comment", comment.ID); err != nil {
		return MergeRequestComment{}, err
	}
	if err := insertOutbox(ctx, tx, "merge_request_comment.created", comment.ID, comment); err != nil {
		return MergeRequestComment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MergeRequestComment{}, fmt.Errorf("commit pull request comment: %w", err)
	}
	return comment, nil
}

func (s *store) UpdateMergeRequestComment(
	ctx context.Context,
	actor platform.User,
	repoID string,
	number int64,
	commentID string,
	body string,
) (MergeRequestComment, error) {
	existing, err := s.findMergeRequestComment(ctx, repoID, number, commentID)
	if err != nil {
		return MergeRequestComment{}, err
	}
	if err := s.authorizeMergeRequestComment(ctx, actor, existing); err != nil {
		return MergeRequestComment{}, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return MergeRequestComment{}, fmt.Errorf("begin pull request comment edit: %w", err)
	}
	defer rollback(ctx, tx)
	editedAt := nowUTC()
	tag, err := tx.Exec(ctx, `
		UPDATE merge_request_comments SET body = $3, edited_at = $4
		WHERE id = $1 AND merge_request_id = $2
	`, commentID, existing.MergeRequestID, body, editedAt)
	if err != nil {
		return MergeRequestComment{}, translateConstraintError("update pull request comment", err)
	}
	if tag.RowsAffected() == 0 {
		return MergeRequestComment{}, platform.ErrNotFound
	}
	if err := touchMergeRequest(ctx, tx, existing.MergeRequestID, editedAt); err != nil {
		return MergeRequestComment{}, err
	}
	if err := insertAudit(ctx, tx, actor.ID, existing.OrgID, existing.RepoID,
		"merge_request_comment.update", "merge_request_comment", commentID); err != nil {
		return MergeRequestComment{}, err
	}
	updated := existing.MergeRequestComment
	updated.Body = body
	updated.EditedAt = &editedAt
	if err := insertOutbox(
		ctx, tx, "merge_request_comment.updated", commentID+":"+uuidArg(), updated,
	); err != nil {
		return MergeRequestComment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MergeRequestComment{}, fmt.Errorf("commit pull request comment edit: %w", err)
	}
	return updated, nil
}

func (s *store) DeleteMergeRequestComment(
	ctx context.Context,
	actor platform.User,
	repoID string,
	number int64,
	commentID string,
) error {
	existing, err := s.findMergeRequestComment(ctx, repoID, number, commentID)
	if err != nil {
		return err
	}
	if err := s.authorizeMergeRequestComment(ctx, actor, existing); err != nil {
		return err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin pull request comment deletion: %w", err)
	}
	defer rollback(ctx, tx)
	tag, err := tx.Exec(ctx, `
		DELETE FROM merge_request_comments WHERE id = $1 AND merge_request_id = $2
	`, commentID, existing.MergeRequestID)
	if err != nil {
		return translateConstraintError("delete pull request comment", err)
	}
	if tag.RowsAffected() == 0 {
		return platform.ErrNotFound
	}
	if err := touchMergeRequest(ctx, tx, existing.MergeRequestID, nowUTC()); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, actor.ID, existing.OrgID, existing.RepoID,
		"merge_request_comment.delete", "merge_request_comment", commentID); err != nil {
		return err
	}
	if err := insertOutbox(
		ctx, tx, "merge_request_comment.deleted", commentID+":"+uuidArg(), existing.MergeRequestComment,
	); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit pull request comment deletion: %w", err)
	}
	return nil
}

type mergeRequestCommentRef struct {
	MergeRequestComment
	RepoID string
	OrgID  string
}

func (s *store) findMergeRequestComment(
	ctx context.Context,
	repoID string,
	number int64,
	commentID string,
) (mergeRequestCommentRef, error) {
	var ref mergeRequestCommentRef
	err := s.pool.QueryRow(ctx, `
		SELECT comment.id, comment.merge_request_id, author.username, comment.author_id,
		       comment.body, comment.created_at, comment.edited_at,
		       repository.id, repository.organization_id
		FROM merge_request_comments comment
		JOIN users author ON author.id = comment.author_id
		JOIN merge_requests merge_request ON merge_request.id = comment.merge_request_id
		JOIN repositories repository ON repository.id = merge_request.repository_id
		WHERE comment.id = $1 AND repository.id = $2 AND merge_request.number = $3
	`, commentID, repoID, number).Scan(
		&ref.ID, &ref.MergeRequestID, &ref.Author, &ref.AuthorID, &ref.Body,
		&ref.CreatedAt, &ref.EditedAt, &ref.RepoID, &ref.OrgID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return mergeRequestCommentRef{}, platform.ErrNotFound
	}
	if err != nil {
		return mergeRequestCommentRef{}, fmt.Errorf("find pull request comment: %w", err)
	}
	return ref, nil
}

func (s *store) authorizeMergeRequestComment(
	ctx context.Context,
	actor platform.User,
	comment mergeRequestCommentRef,
) error {
	access, err := s.permFromRef(ctx, actor, comment.RepoID, comment.OrgID)
	if err != nil {
		return err
	}
	if actor.ID == comment.AuthorID && access.AtLeast(PermRead) {
		return nil
	}
	if !access.AtLeast(PermTriage) {
		return platform.ErrForbidden
	}
	return nil
}

func touchMergeRequest(ctx context.Context, tx pgx.Tx, mergeRequestID string, changedAt time.Time) error {
	if _, err := tx.Exec(ctx, `
		UPDATE merge_requests SET updated_at = $2 WHERE id = $1
	`, mergeRequestID, changedAt); err != nil {
		return fmt.Errorf("update pull request timestamp: %w", err)
	}
	return nil
}
