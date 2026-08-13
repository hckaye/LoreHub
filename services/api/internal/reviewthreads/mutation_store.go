package reviewthreads

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func (store *store) Create(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	number int64,
	input CreateInput,
) (Thread, error) {
	input, err := normalizeCreate(input)
	if err != nil {
		return Thread{}, err
	}
	if len(input.LineContent) > 8192 {
		return Thread{}, invalid("review line is too long")
	}
	tx, request, err := store.begin(ctx, actor, repository, number, "create review thread")
	if err != nil {
		return Thread{}, err
	}
	defer rollback(ctx, tx)
	if request.state != "open" {
		return Thread{}, platform.ErrConflict
	}
	if request.baseRevision != input.ExpectedBaseRevision || request.headRevision != input.ExpectedHeadRevision {
		return Thread{}, platform.ErrConflict
	}
	now := time.Now().UTC()
	thread := Thread{
		ID: uuid.NewString(), Path: input.Path, Side: input.Side, LineNumber: input.LineNumber,
		LineContent: input.LineContent, BaseRevision: input.ExpectedBaseRevision,
		HeadRevision: input.ExpectedHeadRevision, Version: 1, CreatedBy: actor.Username,
		CreatedAt: now, UpdatedAt: now, ViewerCanResolve: true, createdByID: actor.ID,
		mergeAuthorID: request.authorID, Comments: make([]Comment, 0, 1),
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO merge_request_review_threads (
			id, repository_id, merge_request_id, path, side, line_number, line_content,
			base_revision, head_revision, created_by, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
	`, thread.ID, repository.ID, request.id, thread.Path, thread.Side, thread.LineNumber,
		thread.LineContent, thread.BaseRevision, thread.HeadRevision, actor.ID, now)
	if err != nil {
		return Thread{}, translate("create review thread", err)
	}
	if err := requirePendingReview(ctx, tx, repository.ID, request.id, actor, input.PendingReviewID); err != nil {
		return Thread{}, err
	}
	comment, err := insertComment(
		ctx, tx, repository.ID, thread.ID, actor, input.Body, input.PendingReviewID, now,
	)
	if err != nil {
		return Thread{}, err
	}
	thread.Comments = append(thread.Comments, comment)
	if err := recordCreate(ctx, tx, actor.ID, repository, request.id, thread.ID, comment.ID); err != nil {
		return Thread{}, err
	}
	if err := touchRequest(ctx, tx, request.id); err != nil {
		return Thread{}, err
	}
	if err := commit(ctx, tx, "create review thread"); err != nil {
		return Thread{}, err
	}
	return thread, nil
}

func (store *store) Reply(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	number int64,
	threadID string,
	body string,
	pendingReviewID string,
) (Comment, error) {
	if err := validateID("thread ID", threadID); err != nil {
		return Comment{}, err
	}
	body, err := normalizeBody(body)
	if err != nil {
		return Comment{}, err
	}
	tx, request, err := store.begin(ctx, actor, repository, number, "reply to review thread")
	if err != nil {
		return Comment{}, err
	}
	defer rollback(ctx, tx)
	if err := requireThread(ctx, tx, repository.ID, request.id, threadID); err != nil {
		return Comment{}, err
	}
	if err := requirePendingReview(ctx, tx, repository.ID, request.id, actor, pendingReviewID); err != nil {
		return Comment{}, err
	}
	comment, err := insertComment(
		ctx, tx, repository.ID, threadID, actor, body, pendingReviewID, time.Now().UTC(),
	)
	if err != nil {
		return Comment{}, err
	}
	if err := insertAudit(
		ctx, tx, actor.ID, repository, "merge_request_review_comment.create",
		"merge_request_review_comment", comment.ID,
	); err != nil {
		return Comment{}, err
	}
	if err := insertOutbox(
		ctx, tx, "merge_request_review_comment.created", repository.ID, request.id, threadID, comment.ID,
	); err != nil {
		return Comment{}, err
	}
	if err := touchRequest(ctx, tx, request.id); err != nil {
		return Comment{}, err
	}
	if err := commit(ctx, tx, "reply to review thread"); err != nil {
		return Comment{}, err
	}
	return comment, nil
}

func insertComment(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
	threadID string,
	actor platform.User,
	body string,
	pendingReviewID string,
	now time.Time,
) (Comment, error) {
	comment := Comment{
		ID: uuid.NewString(), Author: actor.Username, Body: body, Version: 1,
		Pending: pendingReviewID != "", CreatedAt: now, UpdatedAt: now,
		ViewerCanUpdate: true, authorID: actor.ID,
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO merge_request_review_comments (
			id, repository_id, thread_id, author_id, body, pending_review_id, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::uuid, $7, $7)
	`, comment.ID, repositoryID, threadID, actor.ID, body, pendingReviewID, now)
	if err != nil {
		return Comment{}, translate("create review thread comment", err)
	}
	return comment, nil
}

// requirePendingReview locks the pending review a comment attaches to. An
// empty ID posts the comment immediately, which is the default behaviour.
func requirePendingReview(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
	requestID string,
	actor platform.User,
	pendingReviewID string,
) error {
	if pendingReviewID == "" {
		return nil
	}
	if err := validateID("pending review ID", pendingReviewID); err != nil {
		return err
	}
	var author string
	err := tx.QueryRow(ctx, `
		SELECT author FROM pending_reviews
		WHERE id = $1 AND repository_id = $2 AND merge_request_id = $3 AND state = 'pending'
		FOR UPDATE
	`, pendingReviewID, repositoryID, requestID).Scan(&author)
	if errors.Is(err, pgx.ErrNoRows) {
		return platform.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock pending review: %w", err)
	}
	if author != actor.Username {
		return platform.ErrForbidden
	}
	return nil
}

func (store *store) UpdateComment(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	number int64,
	threadID string,
	commentID string,
	body string,
	expectedVersion int,
) (Comment, error) {
	if err := validateMutation(threadID, commentID, expectedVersion); err != nil {
		return Comment{}, err
	}
	body, err := normalizeBody(body)
	if err != nil {
		return Comment{}, err
	}
	tx, request, err := store.begin(ctx, actor, repository, number, "update review comment")
	if err != nil {
		return Comment{}, err
	}
	defer rollback(ctx, tx)
	comment, err := lockComment(ctx, tx, repository.ID, request.id, threadID, commentID)
	if err != nil {
		return Comment{}, err
	}
	if comment.Deleted {
		return Comment{}, platform.ErrConflict
	}
	if actor.ID != comment.authorID && request.permission < permissionWrite {
		return Comment{}, platform.ErrForbidden
	}
	if comment.Version != expectedVersion {
		return Comment{}, platform.ErrConflict
	}
	now := time.Now().UTC()
	comment.Body = body
	comment.Version++
	comment.UpdatedAt = now
	comment.EditedAt = &now
	comment.ViewerCanUpdate = true
	_, err = tx.Exec(ctx, `
		UPDATE merge_request_review_comments
		SET body = $3, version = version + 1, edited_at = $4, updated_at = $4
		WHERE id = $1 AND thread_id = $2 AND version = $5 AND deleted_at IS NULL
	`, commentID, threadID, body, now, expectedVersion)
	if err != nil {
		return Comment{}, translate("update review thread comment", err)
	}
	if err := recordCommentMutation(
		ctx, tx, actor.ID, repository, request.id, threadID, comment.ID,
		"merge_request_review_comment.update", "merge_request_review_comment.updated",
	); err != nil {
		return Comment{}, err
	}
	if err := touchRequest(ctx, tx, request.id); err != nil {
		return Comment{}, err
	}
	if err := commit(ctx, tx, "update review comment"); err != nil {
		return Comment{}, err
	}
	return comment, nil
}

func (store *store) DeleteComment(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	number int64,
	threadID string,
	commentID string,
	expectedVersion int,
) error {
	if err := validateMutation(threadID, commentID, expectedVersion); err != nil {
		return err
	}
	tx, request, err := store.begin(ctx, actor, repository, number, "delete review comment")
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	comment, err := lockComment(ctx, tx, repository.ID, request.id, threadID, commentID)
	if err != nil {
		return err
	}
	if comment.Deleted || comment.Version != expectedVersion {
		return platform.ErrConflict
	}
	if actor.ID != comment.authorID && request.permission < permissionWrite {
		return platform.ErrForbidden
	}
	_, err = tx.Exec(ctx, `
		UPDATE merge_request_review_comments
		SET body = '', version = version + 1, deleted_at = now(), deleted_by = $3, updated_at = now()
		WHERE id = $1 AND thread_id = $2 AND version = $4 AND deleted_at IS NULL
	`, commentID, threadID, actor.ID, expectedVersion)
	if err != nil {
		return translate("delete review thread comment", err)
	}
	if err := recordCommentMutation(
		ctx, tx, actor.ID, repository, request.id, threadID, comment.ID,
		"merge_request_review_comment.delete", "merge_request_review_comment.deleted",
	); err != nil {
		return err
	}
	if err := touchRequest(ctx, tx, request.id); err != nil {
		return err
	}
	return commit(ctx, tx, "delete review comment")
}

func (store *store) SetResolved(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	number int64,
	threadID string,
	resolved bool,
	expectedVersion int,
) (Thread, error) {
	if err := validateID("thread ID", threadID); err != nil {
		return Thread{}, err
	}
	if err := validateVersion(expectedVersion); err != nil {
		return Thread{}, err
	}
	tx, request, err := store.begin(ctx, actor, repository, number, "update review thread")
	if err != nil {
		return Thread{}, err
	}
	defer rollback(ctx, tx)
	thread, err := lockThread(ctx, tx, repository.ID, request.id, threadID)
	if err != nil {
		return Thread{}, err
	}
	if actor.ID != thread.createdByID && actor.ID != request.authorID && request.permission < permissionWrite {
		return Thread{}, platform.ErrForbidden
	}
	if thread.Version != expectedVersion {
		return Thread{}, platform.ErrConflict
	}
	if thread.Resolved == resolved {
		thread.ViewerCanResolve = true
		return thread, commit(ctx, tx, "leave review thread unchanged")
	}
	now := time.Now().UTC()
	thread.Resolved = resolved
	thread.Version++
	thread.UpdatedAt = now
	thread.ViewerCanResolve = true
	topic := "merge_request_review_thread.unresolved"
	action := "merge_request_review_thread.unresolve"
	if resolved {
		thread.ResolvedBy = &actor.Username
		thread.ResolvedAt = &now
		topic = "merge_request_review_thread.resolved"
		action = "merge_request_review_thread.resolve"
	} else {
		thread.ResolvedBy = nil
		thread.ResolvedAt = nil
	}
	_, err = tx.Exec(ctx, `
		UPDATE merge_request_review_threads
		SET resolved = $3, version = version + 1,
		    resolved_by = CASE WHEN $3 THEN $4::uuid ELSE NULL END,
		    resolved_at = CASE WHEN $3 THEN $5::timestamptz ELSE NULL END,
		    updated_at = $5
		WHERE id = $1 AND merge_request_id = $2 AND version = $6
	`, threadID, request.id, resolved, actor.ID, now, expectedVersion)
	if err != nil {
		return Thread{}, translate("update review thread resolution", err)
	}
	if err := insertAudit(
		ctx, tx, actor.ID, repository, action, "merge_request_review_thread", threadID,
	); err != nil {
		return Thread{}, err
	}
	if err := insertOutbox(ctx, tx, topic, repository.ID, request.id, threadID, ""); err != nil {
		return Thread{}, err
	}
	if err := touchRequest(ctx, tx, request.id); err != nil {
		return Thread{}, err
	}
	if err := commit(ctx, tx, "update review thread"); err != nil {
		return Thread{}, err
	}
	return thread, nil
}

func validateMutation(threadID string, commentID string, expectedVersion int) error {
	if err := validateID("thread ID", threadID); err != nil {
		return err
	}
	if err := validateID("comment ID", commentID); err != nil {
		return err
	}
	return validateVersion(expectedVersion)
}

func requireThread(ctx context.Context, tx pgx.Tx, repositoryID, requestID, threadID string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM merge_request_review_threads
			WHERE id = $1 AND repository_id = $2 AND merge_request_id = $3
		)
	`, threadID, repositoryID, requestID).Scan(&exists); err != nil {
		return fmt.Errorf("find review thread: %w", err)
	}
	if !exists {
		return platform.ErrNotFound
	}
	return nil
}

func lockThread(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
	requestID string,
	threadID string,
) (Thread, error) {
	var thread Thread
	err := tx.QueryRow(ctx, `
		SELECT thread.id, thread.path, thread.side, thread.line_number, thread.line_content,
		       thread.base_revision, thread.head_revision, thread.resolved, thread.version,
		       creator.username, thread.created_by, resolver.username,
		       thread.created_at, thread.updated_at, thread.resolved_at
		FROM merge_request_review_threads thread
		JOIN users creator ON creator.id = thread.created_by
		LEFT JOIN users resolver ON resolver.id = thread.resolved_by
		WHERE thread.id = $1 AND thread.repository_id = $2 AND thread.merge_request_id = $3
		FOR UPDATE OF thread
	`, threadID, repositoryID, requestID).Scan(
		&thread.ID, &thread.Path, &thread.Side, &thread.LineNumber, &thread.LineContent,
		&thread.BaseRevision, &thread.HeadRevision, &thread.Resolved, &thread.Version,
		&thread.CreatedBy, &thread.createdByID, &thread.ResolvedBy,
		&thread.CreatedAt, &thread.UpdatedAt, &thread.ResolvedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Thread{}, platform.ErrNotFound
	}
	if err != nil {
		return Thread{}, fmt.Errorf("lock review thread: %w", err)
	}
	thread.Comments = make([]Comment, 0)
	return thread, nil
}

func lockComment(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
	requestID string,
	threadID string,
	commentID string,
) (Comment, error) {
	var comment Comment
	err := tx.QueryRow(ctx, `
		SELECT comment.id, author.username, comment.author_id, comment.body,
		       comment.deleted_at IS NOT NULL, comment.version,
		       comment.created_at, comment.updated_at, comment.edited_at
		FROM merge_request_review_comments comment
		JOIN merge_request_review_threads thread ON thread.id = comment.thread_id
		  AND thread.repository_id = comment.repository_id
		JOIN users author ON author.id = comment.author_id
		WHERE comment.id = $1 AND thread.id = $2 AND thread.repository_id = $3
		  AND thread.merge_request_id = $4
		FOR UPDATE OF comment
	`, commentID, threadID, repositoryID, requestID).Scan(
		&comment.ID, &comment.Author, &comment.authorID, &comment.Body,
		&comment.Deleted, &comment.Version, &comment.CreatedAt, &comment.UpdatedAt, &comment.EditedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Comment{}, platform.ErrNotFound
	}
	if err != nil {
		return Comment{}, fmt.Errorf("lock review thread comment: %w", err)
	}
	return comment, nil
}

func recordCreate(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repository RepositoryRef,
	requestID string,
	threadID string,
	commentID string,
) error {
	if err := insertAudit(
		ctx, tx, actorID, repository, "merge_request_review_thread.create",
		"merge_request_review_thread", threadID,
	); err != nil {
		return err
	}
	if err := insertAudit(
		ctx, tx, actorID, repository, "merge_request_review_comment.create",
		"merge_request_review_comment", commentID,
	); err != nil {
		return err
	}
	return insertOutbox(
		ctx, tx, "merge_request_review_thread.created", repository.ID, requestID, threadID, commentID,
	)
}

func recordCommentMutation(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repository RepositoryRef,
	requestID string,
	threadID string,
	commentID string,
	action string,
	topic string,
) error {
	if err := insertAudit(
		ctx, tx, actorID, repository, action, "merge_request_review_comment", commentID,
	); err != nil {
		return err
	}
	return insertOutbox(ctx, tx, topic, repository.ID, requestID, threadID, commentID)
}
