package reviewthreads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

// StartPendingReview returns the actor's unsubmitted review, creating it on the
// first call. Repeated calls return the same review so a reload never loses the
// comments already batched into it.
func (store *store) StartPendingReview(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	number int64,
) (PendingReview, bool, error) {
	tx, request, err := store.begin(ctx, actor, repository, number, "start pending review")
	if err != nil {
		return PendingReview{}, false, err
	}
	defer rollback(ctx, tx)
	if request.state != "open" {
		return PendingReview{}, false, platform.ErrConflict
	}
	pending := PendingReview{ID: uuid.NewString(), Author: actor.Username, CreatedAt: time.Now().UTC()}
	tag, err := tx.Exec(ctx, `
		INSERT INTO pending_reviews (id, repository_id, merge_request_id, author, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (merge_request_id, author) WHERE state = 'pending' DO NOTHING
	`, pending.ID, repository.ID, request.id, actor.Username, pending.CreatedAt)
	if err != nil {
		return PendingReview{}, false, translate("start pending review", err)
	}
	created := tag.RowsAffected() == 1
	if !created {
		pending, err = lockPendingReview(ctx, tx, repository.ID, request.id, actor.Username)
		if err != nil {
			return PendingReview{}, false, err
		}
		return pending, false, commit(ctx, tx, "reuse pending review")
	}
	if err := insertAudit(
		ctx, tx, actor.ID, repository, "merge_request_review.pending_start",
		"merge_request_review", pending.ID,
	); err != nil {
		return PendingReview{}, false, err
	}
	if err := commit(ctx, tx, "start pending review"); err != nil {
		return PendingReview{}, false, err
	}
	return pending, true, nil
}

// UpdatePendingReview stores the draft body shown in the review form.
func (store *store) UpdatePendingReview(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	number int64,
	body string,
) (PendingReview, error) {
	tx, request, err := store.begin(ctx, actor, repository, number, "update pending review")
	if err != nil {
		return PendingReview{}, err
	}
	defer rollback(ctx, tx)
	pending, err := lockPendingReview(ctx, tx, repository.ID, request.id, actor.Username)
	if err != nil {
		return PendingReview{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE pending_reviews SET body = $2 WHERE id = $1
	`, pending.ID, body); err != nil {
		return PendingReview{}, translate("update pending review", err)
	}
	pending.Body = body
	if err := commit(ctx, tx, "update pending review"); err != nil {
		return PendingReview{}, err
	}
	return pending, nil
}

// SubmitPendingReview publishes the batched comments, records the verdict and
// removes the pending review in a single transaction, so readers never observe
// a half-submitted review.
func (store *store) SubmitPendingReview(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	number int64,
	input SubmitInput,
) (SubmitResult, error) {
	tx, request, err := store.begin(ctx, actor, repository, number, "submit pending review")
	if err != nil {
		return SubmitResult{}, err
	}
	defer rollback(ctx, tx)
	if request.state != "open" {
		return SubmitResult{}, platform.ErrConflict
	}
	pending, err := lockPendingReview(ctx, tx, repository.ID, request.id, actor.Username)
	if err != nil {
		return SubmitResult{}, err
	}
	// The merge request author may publish batched comments but cannot record a
	// verdict on their own pull request, matching the review endpoint.
	if actor.ID == request.authorID && input.Decision != "commented" {
		return SubmitResult{}, platform.ErrForbidden
	}
	result := SubmitResult{
		Decision: input.Decision, Body: pending.Body, SubmittedAt: time.Now().UTC(),
	}
	if input.Body != nil {
		result.Body = *input.Body
	}
	published, err := tx.Exec(ctx, `
		UPDATE merge_request_review_comments SET pending_review_id = NULL, updated_at = $2
		WHERE pending_review_id = $1
	`, pending.ID, result.SubmittedAt)
	if err != nil {
		return SubmitResult{}, translate("publish pending review comments", err)
	}
	result.PublishedComments = int(published.RowsAffected())
	if actor.ID != request.authorID {
		result.ReviewID, err = recordReviewDecision(ctx, tx, actor, repository, request, result)
		if err != nil {
			return SubmitResult{}, err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM pending_reviews WHERE id = $1`, pending.ID); err != nil {
		return SubmitResult{}, translate("delete pending review", err)
	}
	if err := insertAudit(
		ctx, tx, actor.ID, repository, "merge_request_review.pending_submit",
		"merge_request_review", pending.ID,
	); err != nil {
		return SubmitResult{}, err
	}
	if err := touchRequest(ctx, tx, request.id); err != nil {
		return SubmitResult{}, err
	}
	if err := commit(ctx, tx, "submit pending review"); err != nil {
		return SubmitResult{}, err
	}
	return result, nil
}

// DiscardPendingReview removes the review with the comments it holds. Threads
// that only carried discarded comments disappear with them.
func (store *store) DiscardPendingReview(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	number int64,
) error {
	tx, request, err := store.begin(ctx, actor, repository, number, "discard pending review")
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	pending, err := lockPendingReview(ctx, tx, repository.ID, request.id, actor.Username)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM merge_request_review_comments WHERE pending_review_id = $1
	`, pending.ID); err != nil {
		return translate("delete pending review comments", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM merge_request_review_threads thread
		WHERE thread.repository_id = $1 AND thread.merge_request_id = $2
		  AND NOT EXISTS (
			SELECT 1 FROM merge_request_review_comments comment WHERE comment.thread_id = thread.id
		  )
	`, repository.ID, request.id); err != nil {
		return translate("delete empty review threads", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM pending_reviews WHERE id = $1`, pending.ID); err != nil {
		return translate("delete pending review", err)
	}
	if err := insertAudit(
		ctx, tx, actor.ID, repository, "merge_request_review.pending_discard",
		"merge_request_review", pending.ID,
	); err != nil {
		return err
	}
	return commit(ctx, tx, "discard pending review")
}

// recordReviewDecision writes the verdict through the merge request review
// table, upserting per source revision exactly like the review endpoint.
func recordReviewDecision(
	ctx context.Context,
	tx pgx.Tx,
	actor platform.User,
	repository RepositoryRef,
	request lockedRequest,
	result SubmitResult,
) (string, error) {
	proposedID := uuid.NewString()
	var reviewID string
	err := tx.QueryRow(ctx, `
		INSERT INTO merge_request_reviews
			(id, merge_request_id, reviewer_id, source_revision, decision, body, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (merge_request_id, source_revision, reviewer_id)
		DO UPDATE SET decision = EXCLUDED.decision, body = EXCLUDED.body, created_at = EXCLUDED.created_at
		RETURNING id
	`, proposedID, request.id, actor.ID, request.headRevision,
		result.Decision, result.Body, result.SubmittedAt).Scan(&reviewID)
	if err != nil {
		return "", translate("record review decision", err)
	}
	action, topic := "merge_request_review.update", "merge_request_review.updated"
	if reviewID == proposedID {
		action, topic = "merge_request_review.create", "merge_request_review.created"
	}
	if err := insertAudit(
		ctx, tx, actor.ID, repository, action, "merge_request_review", reviewID,
	); err != nil {
		return "", err
	}
	payload, err := json.Marshal(map[string]any{
		"id":             reviewID,
		"mergeRequestId": request.id,
		"reviewer":       actor.Username,
		"reviewerId":     actor.ID,
		"sourceRevision": request.headRevision,
		"decision":       result.Decision,
		"body":           result.Body,
		"createdAt":      result.SubmittedAt,
	})
	if err != nil {
		return "", fmt.Errorf("encode review outbox event: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO outbox_events (id, topic, event_key, payload) VALUES ($1, $2, $3, $4)
	`, uuid.NewString(), topic, reviewID+":"+uuid.NewString(), payload); err != nil {
		return "", fmt.Errorf("record review outbox event: %w", err)
	}
	return reviewID, nil
}

func lockPendingReview(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
	requestID string,
	author string,
) (PendingReview, error) {
	var pending PendingReview
	err := tx.QueryRow(ctx, `
		SELECT pending.id, pending.author, pending.body, pending.created_at,
		       (SELECT count(*) FROM merge_request_review_comments comment
		        WHERE comment.pending_review_id = pending.id)
		FROM pending_reviews pending
		WHERE pending.repository_id = $1 AND pending.merge_request_id = $2
		  AND pending.author = $3 AND pending.state = 'pending'
		FOR UPDATE OF pending
	`, repositoryID, requestID, author).Scan(
		&pending.ID, &pending.Author, &pending.Body, &pending.CreatedAt, &pending.CommentCount,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PendingReview{}, platform.ErrNotFound
	}
	if err != nil {
		return PendingReview{}, fmt.Errorf("lock pending review: %w", err)
	}
	return pending, nil
}
