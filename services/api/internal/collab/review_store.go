package collab

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

// GetMergeRequest loads a merge request by repository and number.
func (s *store) GetMergeRequest(
	ctx context.Context,
	repoID string,
	number int64,
) (MergeRequest, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT mr.id, mr.number, mr.title, mr.body, mr.state,
		       mr.source_branch, mr.target_branch, mr.source_revision, mr.target_revision,
		       author.username, mr.author_id, merged.username, mr.merged_revision,
		       mr.merged_at, mr.created_at, mr.updated_at, mr.closed_at
		FROM merge_requests mr
		JOIN users author ON author.id = mr.author_id
		LEFT JOIN users merged ON merged.id = mr.merged_by
		WHERE mr.repository_id = $1 AND mr.number = $2
	`, repoID, number)
	mr, err := scanMergeRequest(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return MergeRequest{}, platform.ErrNotFound
	}
	if err != nil {
		return MergeRequest{}, fmt.Errorf("get merge request: %w", err)
	}
	return mr, nil
}

func scanMergeRequest(row pgx.Row) (MergeRequest, error) {
	var mr MergeRequest
	err := row.Scan(
		&mr.ID, &mr.Number, &mr.Title, &mr.Body, &mr.State,
		&mr.SourceBranch, &mr.TargetBranch, &mr.SourceRevision, &mr.TargetRevision,
		&mr.Author, &mr.AuthorID, &mr.MergedBy, &mr.MergedRevision, &mr.MergedAt,
		&mr.CreatedAt, &mr.UpdatedAt, &mr.ClosedAt,
	)
	return mr, err
}

// UpdateMergeRequest edits title/body or closes/reopens. The merged state is
// terminal and not reachable here. The author or a triage+ actor may update.
func (s *store) UpdateMergeRequest(
	ctx context.Context,
	actor platform.User,
	repoID string,
	number int64,
	input UpdateMergeRequestInput,
) (MergeRequest, error) {
	allowed, orgID, state, err := s.checkMergeRequestMutation(ctx, actor, repoID, number)
	if err != nil {
		return MergeRequest{}, err
	}
	if state == "merged" {
		return MergeRequest{}, platform.ErrConflict
	}
	if !allowed {
		return MergeRequest{}, platform.ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return MergeRequest{}, fmt.Errorf("begin merge request update: %w", err)
	}
	defer rollback(ctx, tx)

	query, args, err := buildMergeRequestUpdateQuery(repoID, number, input, nowUTC())
	if err != nil {
		return MergeRequest{}, err
	}
	tag, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return MergeRequest{}, translateConstraintError("update merge request", err)
	}
	if tag.RowsAffected() == 0 {
		mr, lookupErr := s.GetMergeRequest(ctx, repoID, number)
		if lookupErr != nil {
			return MergeRequest{}, lookupErr
		}
		if input.IfMatch != nil && !mr.UpdatedAt.Equal(*input.IfMatch) {
			return MergeRequest{}, ErrPreconditionFailed
		}
		return MergeRequest{}, platform.ErrNotFound
	}
	mr, err := scanMergeRequestByTx(ctx, tx, repoID, number)
	if err != nil {
		return MergeRequest{}, err
	}
	action := "merge_request.update"
	if input.State != nil {
		switch *input.State {
		case "closed":
			action = "merge_request.close"
		case "open":
			action = "merge_request.reopen"
		}
	}
	if err := insertAudit(ctx, tx, actor.ID, orgID, repoID, action, "merge_request", mr.ID); err != nil {
		return MergeRequest{}, err
	}
	if err := insertOutbox(ctx, tx, "merge_request.updated", mr.ID+":"+uuidArg(), mr); err != nil {
		return MergeRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MergeRequest{}, fmt.Errorf("commit merge request update: %w", err)
	}
	return mr, nil
}

func (s *store) checkMergeRequestMutation(
	ctx context.Context,
	actor platform.User,
	repoID string,
	number int64,
) (bool, string, string, error) {
	var orgID, state, authorID string
	err := s.pool.QueryRow(ctx, `
		SELECT r.organization_id, mr.state, mr.author_id
		FROM merge_requests mr
		JOIN repositories r ON r.id = mr.repository_id
		WHERE mr.repository_id = $1 AND mr.number = $2
	`, repoID, number).Scan(&orgID, &state, &authorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, "", "", platform.ErrNotFound
	}
	if err != nil {
		return false, "", "", fmt.Errorf("find merge request for mutation: %w", err)
	}
	access, err := s.permFromRef(ctx, actor, repoID, orgID)
	if err != nil {
		return false, "", "", err
	}
	allowed := access.AtLeast(PermTriage) || actor.ID == authorID && access.AtLeast(PermRead)
	return allowed, orgID, state, nil
}

func buildMergeRequestUpdateQuery(
	repoID string,
	number int64,
	input UpdateMergeRequestInput,
	now time.Time,
) (string, []any, error) {
	sets := []string{"updated_at = $1"}
	args := []any{now}
	idx := 2
	if input.Title != nil {
		sets = append(sets, fmt.Sprintf("title = $%d", idx))
		args = append(args, *input.Title)
		idx++
	}
	if input.Body != nil {
		sets = append(sets, fmt.Sprintf("body = $%d", idx))
		args = append(args, *input.Body)
		idx++
	}
	if input.State != nil {
		state := *input.State
		switch state {
		case "closed":
			sets = append(sets, fmt.Sprintf("state = $%d", idx), "closed_at = $1")
			args = append(args, state)
			idx++
		case "open":
			sets = append(sets, fmt.Sprintf("state = $%d", idx), "closed_at = NULL")
			args = append(args, state)
			idx++
		default:
			return "", nil, ErrInvalidState
		}
	}
	where := []string{fmt.Sprintf("repository_id = $%d", idx), fmt.Sprintf("number = $%d", idx+1)}
	args = append(args, repoID, number)
	if input.IfMatch != nil {
		where = append(where, fmt.Sprintf("updated_at = $%d", idx+2))
		args = append(args, *input.IfMatch)
	}
	query := fmt.Sprintf(
		"UPDATE merge_requests SET %s WHERE %s",
		joinStrings(sets, ", "), joinStrings(where, " AND "),
	)
	return query, args, nil
}

func scanMergeRequestByTx(
	ctx context.Context,
	tx pgx.Tx,
	repoID string,
	number int64,
) (MergeRequest, error) {
	row := tx.QueryRow(ctx, `
		SELECT mr.id, mr.number, mr.title, mr.body, mr.state,
		       mr.source_branch, mr.target_branch, mr.source_revision, mr.target_revision,
		       author.username, mr.author_id, merged.username, mr.merged_revision,
		       mr.merged_at, mr.created_at, mr.updated_at, mr.closed_at
		FROM merge_requests mr
		JOIN users author ON author.id = mr.author_id
		LEFT JOIN users merged ON merged.id = mr.merged_by
		WHERE mr.repository_id = $1 AND mr.number = $2
	`, repoID, number)
	return scanMergeRequest(row)
}

// ListReviews returns all reviews for a merge request plus an aggregate over
// the current source revision. The current revision is taken from the stored
// request, not from any client-supplied value.
func (s *store) ListReviews(
	ctx context.Context,
	repoID string,
	number int64,
) (ReviewSummary, error) {
	mr, err := s.GetMergeRequest(ctx, repoID, number)
	if err != nil {
		return ReviewSummary{}, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT rv.id, rv.merge_request_id, reviewer.username, rv.reviewer_id,
		       rv.source_revision, rv.decision, rv.body, rv.created_at
		FROM merge_request_reviews rv
		JOIN merge_requests mr ON mr.id = rv.merge_request_id
		JOIN users reviewer ON reviewer.id = rv.reviewer_id
		WHERE mr.repository_id = $1 AND mr.number = $2
		ORDER BY rv.created_at ASC, rv.id ASC
	`, repoID, number)
	if err != nil {
		return ReviewSummary{}, fmt.Errorf("list reviews: %w", err)
	}
	defer rows.Close()
	reviews := make([]Review, 0)
	for rows.Next() {
		var review Review
		if err := rows.Scan(
			&review.ID, &review.MergeRequestID, &review.Reviewer, &review.ReviewerID,
			&review.SourceRevision, &review.Decision, &review.Body, &review.CreatedAt,
		); err != nil {
			return ReviewSummary{}, fmt.Errorf("scan review: %w", err)
		}
		reviews = append(reviews, review)
	}
	if err := rows.Err(); err != nil {
		return ReviewSummary{}, fmt.Errorf("iterate reviews: %w", err)
	}
	return summarizeReviews(mr.SourceRevision, reviews), nil
}

func summarizeReviews(currentRevision string, reviews []Review) ReviewSummary {
	summary := ReviewSummary{
		CurrentRevision: currentRevision,
		Reviews:         reviews,
		CurrentReviews:  make([]Review, 0),
	}
	for _, review := range reviews {
		if review.SourceRevision != currentRevision {
			continue
		}
		summary.CurrentReviews = append(summary.CurrentReviews, review)
		switch review.Decision {
		case "approved":
			summary.Approvals++
		case "changes_requested":
			summary.ChangeRequests++
		case "commented":
			summary.Comments++
		}
	}
	return summary
}

// CreateReview upserts a reviewer decision for the merge request's current
// source revision. The actor must not be the merge request author. The unique
// constraint on (merge_request_id, source_revision, reviewer_id) makes the
// decision safe to update in place.
func (s *store) CreateReview(
	ctx context.Context,
	actor platform.User,
	repoID string,
	number int64,
	input ReviewInput,
) (Review, bool, error) {
	mr, err := s.GetMergeRequest(ctx, repoID, number)
	if err != nil {
		return Review{}, false, err
	}
	if mr.AuthorID == actor.ID {
		return Review{}, false, ErrCannotReviewOwn
	}
	orgID, err := s.repoOrgID(ctx, repoID)
	if err != nil {
		return Review{}, false, err
	}
	access, err := s.permFromRef(ctx, actor, repoID, orgID)
	if err != nil {
		return Review{}, false, err
	}
	if !access.AtLeast(PermRead) {
		return Review{}, false, platform.ErrForbidden
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Review{}, false, fmt.Errorf("begin review transaction: %w", err)
	}
	defer rollback(ctx, tx)

	review := Review{
		ID:             uuidArg(),
		MergeRequestID: mr.ID,
		Reviewer:       actor.Username,
		ReviewerID:     actor.ID,
		SourceRevision: mr.SourceRevision,
		Decision:       input.Decision,
		Body:           input.Body,
		CreatedAt:      nowUTC(),
	}
	proposedID := review.ID
	err = tx.QueryRow(ctx, `
		INSERT INTO merge_request_reviews
			(id, merge_request_id, reviewer_id, source_revision, decision, body, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (merge_request_id, source_revision, reviewer_id)
		DO UPDATE SET decision = EXCLUDED.decision, body = EXCLUDED.body, created_at = EXCLUDED.created_at
		RETURNING id, merge_request_id, reviewer_id, source_revision, decision, body, created_at
	`, review.ID, mr.ID, actor.ID, review.SourceRevision, review.Decision, review.Body, review.CreatedAt).Scan(
		&review.ID, &review.MergeRequestID, &review.ReviewerID, &review.SourceRevision,
		&review.Decision, &review.Body, &review.CreatedAt,
	)
	if err != nil {
		return Review{}, false, translateConstraintError("create review", err)
	}
	review.Reviewer = actor.Username
	created := review.ID == proposedID
	action := "merge_request_review.update"
	topic := "merge_request_review.updated"
	if created {
		action = "merge_request_review.create"
		topic = "merge_request_review.created"
	}
	if err := insertAudit(ctx, tx, actor.ID, orgID, repoID,
		action, "merge_request_review", review.ID); err != nil {
		return Review{}, false, err
	}
	if err := insertOutbox(ctx, tx, topic, review.ID+":"+uuidArg(), review); err != nil {
		return Review{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Review{}, false, fmt.Errorf("commit review transaction: %w", err)
	}
	return review, created, nil
}
