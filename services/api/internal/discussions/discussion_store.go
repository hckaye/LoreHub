package discussions

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type discussionLock struct {
	ID              string
	AuthorID        string
	CategoryID      string
	CategoryFormat  string
	Title           string
	State           string
	Locked          bool
	AnsweredComment *string
}

func (store *store) Create(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	input CreateInput,
) (Discussion, error) {
	input, err := normalizeCreateInput(input)
	if err != nil {
		return Discussion{}, err
	}
	tx, err := store.beginAuthorized(ctx, actor, repository, permissionParticipate, "create discussion")
	if err != nil {
		return Discussion{}, err
	}
	defer rollback(ctx, tx)
	categoryID, categoryFormat, err := categoryForMutation(ctx, tx, repository.ID, input.CategorySlug)
	if err != nil {
		return Discussion{}, err
	}
	if categoryFormat == "announcement" {
		allowed, permissionErr := discussionPermissionAllowed(
			ctx, tx, actor.ID, repository, permissionModerate,
		)
		if permissionErr != nil {
			return Discussion{}, permissionErr
		}
		if !allowed {
			return Discussion{}, platform.ErrForbidden
		}
	}
	var number int64
	if err := tx.QueryRow(ctx, `
		UPDATE repository_counters
		SET next_discussion_number = next_discussion_number + 1
		WHERE repository_id = $1
		RETURNING next_discussion_number - 1
	`, repository.ID).Scan(&number); err != nil {
		return Discussion{}, fmt.Errorf("allocate discussion number: %w", err)
	}
	discussionID := uuid.NewString()
	if _, err := tx.Exec(ctx, `
		INSERT INTO discussions (
			id, repository_id, category_id, number, author_id, title, body
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, discussionID, repository.ID, categoryID, number, actor.ID, input.Title, input.Body); err != nil {
		return Discussion{}, translateStoreError("create discussion", err)
	}
	if err := recordMutation(
		ctx,
		tx,
		actor.ID,
		repository,
		"discussion.created",
		"discussion",
		discussionID,
		map[string]any{"number": number, "title": input.Title},
	); err != nil {
		return Discussion{}, err
	}
	if err := commit(ctx, tx, "create discussion"); err != nil {
		return Discussion{}, err
	}
	return store.Get(ctx, repository.ID, number, actor.ID, 1, 100)
}

func (store *store) Delete(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	number int64,
) error {
	if number < 1 {
		return platform.ErrNotFound
	}
	tx, err := store.beginAuthorized(ctx, actor, repository, permissionParticipate, "delete discussion")
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	locked, err := lockDiscussion(ctx, tx, repository.ID, number)
	if err != nil {
		return err
	}
	moderator, err := discussionPermissionAllowed(ctx, tx, actor.ID, repository, permissionModerate)
	if err != nil {
		return err
	}
	if locked.Locked && !moderator {
		return platform.ErrForbidden
	}
	if locked.AuthorID != actor.ID && !moderator {
		return platform.ErrForbidden
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM discussions WHERE id = $1 AND repository_id = $2
	`, locked.ID, repository.ID); err != nil {
		return translateStoreError("delete discussion", err)
	}
	if err := recordMutation(
		ctx,
		tx,
		actor.ID,
		repository,
		"discussion.deleted",
		"discussion",
		locked.ID,
		map[string]any{"number": number, "title": locked.Title},
	); err != nil {
		return err
	}
	return commit(ctx, tx, "delete discussion")
}

func (store *store) Update(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	number int64,
	input UpdateInput,
) (Discussion, error) {
	if number < 1 {
		return Discussion{}, platform.ErrNotFound
	}
	input, err := normalizeUpdateInput(input)
	if err != nil {
		return Discussion{}, err
	}
	tx, err := store.beginAuthorized(ctx, actor, repository, permissionParticipate, "update discussion")
	if err != nil {
		return Discussion{}, err
	}
	defer rollback(ctx, tx)
	locked, err := lockDiscussion(ctx, tx, repository.ID, number)
	if err != nil {
		return Discussion{}, err
	}
	moderator, err := discussionPermissionAllowed(ctx, tx, actor.ID, repository, permissionModerate)
	if err != nil {
		return Discussion{}, err
	}
	isAuthor := locked.AuthorID == actor.ID
	if !isAuthor && !moderator {
		return Discussion{}, platform.ErrForbidden
	}
	if (input.Locked != nil || input.Pinned != nil) && !moderator {
		return Discussion{}, platform.ErrForbidden
	}
	if locked.Locked && !moderator {
		return Discussion{}, platform.ErrForbidden
	}
	categoryID := locked.CategoryID
	categoryFormat := locked.CategoryFormat
	if input.CategorySlug != nil {
		categoryID, categoryFormat, err = categoryForMutation(ctx, tx, repository.ID, *input.CategorySlug)
		if err != nil {
			return Discussion{}, err
		}
		if categoryFormat == "announcement" && !moderator {
			return Discussion{}, platform.ErrForbidden
		}
	}
	_, err = tx.Exec(ctx, `
		UPDATE discussions
		SET category_id = $3,
		    title = COALESCE($4, title),
		    body = COALESCE($5, body),
		    state = COALESCE($6, state),
		    closed_at = CASE
		      WHEN COALESCE($6, state) = 'closed' THEN COALESCE(closed_at, now())
		      ELSE NULL
		    END,
		    locked = COALESCE($7, locked),
		    pinned = COALESCE($8, pinned),
		    answered_comment_id = CASE WHEN $9 = 'question' THEN answered_comment_id ELSE NULL END,
		    updated_at = now()
		WHERE id = $1 AND repository_id = $2
	`, locked.ID, repository.ID, categoryID, input.Title, input.Body, input.State,
		input.Locked, input.Pinned, categoryFormat)
	if err != nil {
		return Discussion{}, translateStoreError("update discussion", err)
	}
	if err := recordMutation(
		ctx,
		tx,
		actor.ID,
		repository,
		"discussion.updated",
		"discussion",
		locked.ID,
		map[string]any{"number": number},
	); err != nil {
		return Discussion{}, err
	}
	if err := commit(ctx, tx, "update discussion"); err != nil {
		return Discussion{}, err
	}
	return store.Get(ctx, repository.ID, number, actor.ID, 1, 100)
}

func (store *store) SetVote(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	number int64,
	enabled bool,
) (Summary, error) {
	if number < 1 {
		return Summary{}, platform.ErrNotFound
	}
	tx, err := store.beginAuthorized(ctx, actor, repository, permissionParticipate, "set discussion vote")
	if err != nil {
		return Summary{}, err
	}
	defer rollback(ctx, tx)
	locked, err := lockDiscussion(ctx, tx, repository.ID, number)
	if err != nil {
		return Summary{}, err
	}
	var changed bool
	if enabled {
		command, execErr := tx.Exec(ctx, `
			INSERT INTO discussion_votes (discussion_id, user_id)
			VALUES ($1, $2) ON CONFLICT DO NOTHING
		`, locked.ID, actor.ID)
		err = execErr
		changed = command.RowsAffected() == 1
	} else {
		command, execErr := tx.Exec(ctx, `
			DELETE FROM discussion_votes WHERE discussion_id = $1 AND user_id = $2
		`, locked.ID, actor.ID)
		err = execErr
		changed = command.RowsAffected() == 1
	}
	if err != nil {
		return Summary{}, fmt.Errorf("set discussion vote: %w", err)
	}
	if err := recordMutation(
		ctx,
		tx,
		actor.ID,
		repository,
		"discussion.vote.updated",
		"discussion",
		locked.ID,
		map[string]any{"number": number, "enabled": enabled, "changed": changed},
	); err != nil {
		return Summary{}, err
	}
	if err := commit(ctx, tx, "set discussion vote"); err != nil {
		return Summary{}, err
	}
	discussion, err := store.Get(ctx, repository.ID, number, actor.ID, 1, 1)
	return discussion.Summary, err
}

func (store *store) SetAnswer(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	number int64,
	commentID string,
	accepted bool,
) (Discussion, error) {
	commentUUID, err := uuid.Parse(commentID)
	if err != nil || number < 1 {
		return Discussion{}, platform.ErrNotFound
	}
	tx, err := store.beginAuthorized(ctx, actor, repository, permissionParticipate, "set discussion answer")
	if err != nil {
		return Discussion{}, err
	}
	defer rollback(ctx, tx)
	locked, err := lockDiscussion(ctx, tx, repository.ID, number)
	if err != nil {
		return Discussion{}, err
	}
	moderator, err := discussionPermissionAllowed(ctx, tx, actor.ID, repository, permissionModerate)
	if err != nil {
		return Discussion{}, err
	}
	if locked.AuthorID != actor.ID && !moderator {
		return Discussion{}, platform.ErrForbidden
	}
	if locked.CategoryFormat != "question" {
		return Discussion{}, platform.ErrConflict
	}
	var commentExists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM discussion_comments
			WHERE id = $1 AND discussion_id = $2 AND repository_id = $3 AND archived_at IS NULL
		)
	`, commentUUID, locked.ID, repository.ID).Scan(&commentExists); err != nil {
		return Discussion{}, fmt.Errorf("find discussion answer comment: %w", err)
	}
	if !commentExists {
		return Discussion{}, platform.ErrNotFound
	}
	if accepted {
		_, err = tx.Exec(ctx, `
			UPDATE discussions SET answered_comment_id = $2, updated_at = now() WHERE id = $1
		`, locked.ID, commentUUID)
	} else {
		_, err = tx.Exec(ctx, `
			UPDATE discussions SET answered_comment_id = NULL, updated_at = now()
			WHERE id = $1 AND answered_comment_id = $2
		`, locked.ID, commentUUID)
	}
	if err != nil {
		return Discussion{}, fmt.Errorf("set discussion answer: %w", err)
	}
	if err := recordMutation(
		ctx,
		tx,
		actor.ID,
		repository,
		"discussion.answer.updated",
		"discussion",
		locked.ID,
		map[string]any{"number": number, "commentId": commentID, "accepted": accepted},
	); err != nil {
		return Discussion{}, err
	}
	if err := commit(ctx, tx, "set discussion answer"); err != nil {
		return Discussion{}, err
	}
	return store.Get(ctx, repository.ID, number, actor.ID, 1, 100)
}

func categoryForMutation(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
	slug string,
) (string, string, error) {
	var categoryID, format string
	err := tx.QueryRow(ctx, `
		SELECT id::text, format FROM discussion_categories
		WHERE repository_id = $1 AND lower(slug) = lower($2)
	`, repositoryID, slug).Scan(&categoryID, &format)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", platform.ErrNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("find discussion category: %w", err)
	}
	return categoryID, format, nil
}

func lockDiscussion(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
	number int64,
) (discussionLock, error) {
	var discussion discussionLock
	err := tx.QueryRow(ctx, `
		SELECT discussion.id::text, discussion.author_id::text, discussion.category_id::text,
		       category.format, discussion.title, discussion.state, discussion.locked,
		       discussion.answered_comment_id::text
		FROM discussions discussion
		JOIN discussion_categories category ON category.id = discussion.category_id
		WHERE discussion.repository_id = $1 AND discussion.number = $2
		FOR UPDATE OF discussion
	`, repositoryID, number).Scan(
		&discussion.ID,
		&discussion.AuthorID,
		&discussion.CategoryID,
		&discussion.CategoryFormat,
		&discussion.Title,
		&discussion.State,
		&discussion.Locked,
		&discussion.AnsweredComment,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return discussionLock{}, platform.ErrNotFound
	}
	if err != nil {
		return discussionLock{}, fmt.Errorf("lock discussion: %w", err)
	}
	return discussion, nil
}
