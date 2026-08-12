package discussions

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func (store *store) CreateComment(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	number int64,
	parentID *string,
	body string,
) (Comment, error) {
	if number < 1 {
		return Comment{}, platform.ErrNotFound
	}
	body, err := normalizeCommentBody(body)
	if err != nil {
		return Comment{}, err
	}
	tx, err := store.beginAuthorized(ctx, actor, repository, permissionParticipate, "create discussion comment")
	if err != nil {
		return Comment{}, err
	}
	defer rollback(ctx, tx)
	discussion, err := lockDiscussion(ctx, tx, repository.ID, number)
	if err != nil {
		return Comment{}, err
	}
	moderator, err := discussionPermissionAllowed(ctx, tx, actor.ID, repository, permissionModerate)
	if err != nil {
		return Comment{}, err
	}
	if (discussion.State != "open" || discussion.Locked) && !moderator {
		return Comment{}, platform.ErrConflict
	}
	var parentUUID *uuid.UUID
	if parentID != nil {
		parsed, parseErr := uuid.Parse(*parentID)
		if parseErr != nil {
			return Comment{}, platform.ErrNotFound
		}
		var validParent bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM discussion_comments
				WHERE id = $1 AND discussion_id = $2 AND repository_id = $3
				  AND parent_id IS NULL AND archived_at IS NULL
			)
		`, parsed, discussion.ID, repository.ID).Scan(&validParent); err != nil {
			return Comment{}, fmt.Errorf("validate discussion comment parent: %w", err)
		}
		if !validParent {
			return Comment{}, platform.ErrNotFound
		}
		parentUUID = &parsed
	}
	commentID := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		INSERT INTO discussion_comments (
			id, repository_id, discussion_id, parent_id, author_id, body
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, commentID, repository.ID, discussion.ID, parentUUID, actor.ID, body); err != nil {
		return Comment{}, translateStoreError("create discussion comment", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE discussions SET updated_at = now() WHERE id = $1
	`, discussion.ID); err != nil {
		return Comment{}, fmt.Errorf("touch discussion after comment: %w", err)
	}
	if err := recordMutation(
		ctx,
		tx,
		actor.ID,
		repository,
		"discussion.comment.created",
		"discussion_comment",
		commentID,
		map[string]any{"discussionId": discussion.ID, "number": number},
	); err != nil {
		return Comment{}, err
	}
	if err := commit(ctx, tx, "create discussion comment"); err != nil {
		return Comment{}, err
	}
	return store.commentByID(ctx, repository.ID, discussion.ID, commentID)
}

func (store *store) UpdateComment(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	number int64,
	commentID string,
	body string,
) (Comment, error) {
	commentUUID, err := uuid.Parse(commentID)
	if err != nil || number < 1 {
		return Comment{}, platform.ErrNotFound
	}
	body, err = normalizeCommentBody(body)
	if err != nil {
		return Comment{}, err
	}
	tx, err := store.beginAuthorized(ctx, actor, repository, permissionParticipate, "update discussion comment")
	if err != nil {
		return Comment{}, err
	}
	defer rollback(ctx, tx)
	discussion, err := lockDiscussion(ctx, tx, repository.ID, number)
	if err != nil {
		return Comment{}, err
	}
	commentAuthor, err := lockCommentAuthor(ctx, tx, repository.ID, discussion.ID, commentUUID)
	if err != nil {
		return Comment{}, err
	}
	moderator, err := discussionPermissionAllowed(ctx, tx, actor.ID, repository, permissionModerate)
	if err != nil {
		return Comment{}, err
	}
	if commentAuthor != actor.ID && !moderator {
		return Comment{}, platform.ErrForbidden
	}
	if (discussion.State != "open" || discussion.Locked) && !moderator {
		return Comment{}, platform.ErrConflict
	}
	if _, err := tx.Exec(ctx, `
		UPDATE discussion_comments
		SET body = $4, edited_at = now(), updated_at = now()
		WHERE id = $1 AND repository_id = $2 AND discussion_id = $3 AND archived_at IS NULL
	`, commentUUID, repository.ID, discussion.ID, body); err != nil {
		return Comment{}, translateStoreError("update discussion comment", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE discussions SET updated_at = now() WHERE id = $1
	`, discussion.ID); err != nil {
		return Comment{}, fmt.Errorf("touch discussion after comment edit: %w", err)
	}
	if err := recordMutation(
		ctx,
		tx,
		actor.ID,
		repository,
		"discussion.comment.updated",
		"discussion_comment",
		commentID,
		map[string]any{"discussionId": discussion.ID, "number": number},
	); err != nil {
		return Comment{}, err
	}
	if err := commit(ctx, tx, "update discussion comment"); err != nil {
		return Comment{}, err
	}
	return store.commentByID(ctx, repository.ID, discussion.ID, commentID)
}

func (store *store) DeleteComment(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	number int64,
	commentID string,
) error {
	commentUUID, err := uuid.Parse(commentID)
	if err != nil || number < 1 {
		return platform.ErrNotFound
	}
	tx, err := store.beginAuthorized(ctx, actor, repository, permissionParticipate, "delete discussion comment")
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	discussion, err := lockDiscussion(ctx, tx, repository.ID, number)
	if err != nil {
		return err
	}
	commentAuthor, err := lockCommentAuthor(ctx, tx, repository.ID, discussion.ID, commentUUID)
	if err != nil {
		return err
	}
	moderator, err := discussionPermissionAllowed(ctx, tx, actor.ID, repository, permissionModerate)
	if err != nil {
		return err
	}
	if commentAuthor != actor.ID && !moderator {
		return platform.ErrForbidden
	}
	if discussion.Locked && !moderator {
		return platform.ErrConflict
	}
	command, err := tx.Exec(ctx, `
		UPDATE discussion_comments
		SET archived_at = now(), updated_at = now()
		WHERE id = $1 AND repository_id = $2 AND discussion_id = $3 AND archived_at IS NULL
	`, commentUUID, repository.ID, discussion.ID)
	if err != nil {
		return fmt.Errorf("delete discussion comment: %w", err)
	}
	if command.RowsAffected() != 1 {
		return platform.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
		UPDATE discussions
		SET answered_comment_id = CASE
		      WHEN answered_comment_id = $2 THEN NULL ELSE answered_comment_id
		    END,
		    updated_at = now()
		WHERE id = $1
	`, discussion.ID, commentUUID); err != nil {
		return fmt.Errorf("clear deleted discussion answer: %w", err)
	}
	if err := recordMutation(
		ctx,
		tx,
		actor.ID,
		repository,
		"discussion.comment.deleted",
		"discussion_comment",
		commentID,
		map[string]any{"discussionId": discussion.ID, "number": number},
	); err != nil {
		return err
	}
	return commit(ctx, tx, "delete discussion comment")
}

func lockCommentAuthor(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
	discussionID string,
	commentID uuid.UUID,
) (string, error) {
	var authorID string
	err := tx.QueryRow(ctx, `
		SELECT author_id::text FROM discussion_comments
		WHERE id = $1 AND repository_id = $2 AND discussion_id = $3 AND archived_at IS NULL
		FOR UPDATE
	`, commentID, repositoryID, discussionID).Scan(&authorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", platform.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("lock discussion comment: %w", err)
	}
	return authorID, nil
}

func (store *store) commentByID(
	ctx context.Context,
	repositoryID string,
	discussionID string,
	commentID string,
) (Comment, error) {
	var comment Comment
	err := store.pool.QueryRow(ctx, `
		SELECT comment.id::text, comment.parent_id::text,
		       author.id::text, author.username, author.display_name, author.avatar_url,
		comment.body, COALESCE(discussion.answered_comment_id = comment.id, false),
		       comment.created_at, comment.updated_at, comment.edited_at
		FROM discussion_comments comment
		JOIN discussions discussion
		  ON discussion.id = comment.discussion_id
		 AND discussion.repository_id = comment.repository_id
		JOIN users author ON author.id = comment.author_id
		WHERE comment.id = $1 AND comment.repository_id = $2
		  AND comment.discussion_id = $3 AND comment.archived_at IS NULL
	`, commentID, repositoryID, discussionID).Scan(
		&comment.ID,
		&comment.ParentID,
		&comment.Author.ID,
		&comment.Author.Username,
		&comment.Author.DisplayName,
		&comment.Author.AvatarURL,
		&comment.Body,
		&comment.Answer,
		&comment.CreatedAt,
		&comment.UpdatedAt,
		&comment.EditedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Comment{}, platform.ErrNotFound
	}
	if err != nil {
		return Comment{}, fmt.Errorf("get discussion comment: %w", err)
	}
	return comment, nil
}
