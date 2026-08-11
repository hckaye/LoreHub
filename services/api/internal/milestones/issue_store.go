package milestones

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/collab"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func (store *store) AssignIssue(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	issueNumber int64,
	milestoneNumber int64,
) (collab.MilestoneSummary, error) {
	tx, err := store.beginMutation(ctx, actor, repository, milestoneAssignRoles, "milestone assignment")
	if err != nil {
		return collab.MilestoneSummary{}, err
	}
	defer rollback(ctx, tx)
	milestone, err := milestoneSummary(ctx, tx, repository.ID, milestoneNumber)
	if err != nil {
		return collab.MilestoneSummary{}, err
	}
	var issueID string
	var previousMilestoneID *string
	err = tx.QueryRow(ctx, `
		SELECT id, milestone_id FROM issues
		WHERE repository_id = $1 AND number = $2
		FOR UPDATE
	`, repository.ID, issueNumber).Scan(&issueID, &previousMilestoneID)
	if errors.Is(err, pgx.ErrNoRows) {
		return collab.MilestoneSummary{}, platform.ErrNotFound
	}
	if err != nil {
		return collab.MilestoneSummary{}, fmt.Errorf("lock issue for milestone assignment: %w", err)
	}
	if previousMilestoneID != nil && *previousMilestoneID == milestone.ID {
		if err := commit(ctx, tx, "milestone assignment"); err != nil {
			return collab.MilestoneSummary{}, err
		}
		return milestone, nil
	}
	_, err = tx.Exec(ctx, `
		UPDATE issues
		SET milestone_id = $1, updated_at = $2
		WHERE id = $3
	`, milestone.ID, nowUTC(), issueID)
	if err != nil {
		return collab.MilestoneSummary{}, constraintError("assign issue milestone", err)
	}
	payload := map[string]any{"issueNumber": issueNumber, "milestone": milestone}
	if err := insertAudit(
		ctx, tx, actor.ID, repository, "issue.milestone.set", "issue", issueID,
	); err != nil {
		return collab.MilestoneSummary{}, err
	}
	if err := insertOutbox(ctx, tx, "issue.milestone.updated", issueID+":"+uuid.NewString(), payload); err != nil {
		return collab.MilestoneSummary{}, err
	}
	if err := commit(ctx, tx, "milestone assignment"); err != nil {
		return collab.MilestoneSummary{}, err
	}
	return milestone, nil
}

func (store *store) RemoveIssue(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	issueNumber int64,
) error {
	tx, err := store.beginMutation(ctx, actor, repository, milestoneAssignRoles, "milestone removal")
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	var issueID string
	var previousMilestoneID *string
	err = tx.QueryRow(ctx, `
		SELECT id, milestone_id FROM issues
		WHERE repository_id = $1 AND number = $2
		FOR UPDATE
	`, repository.ID, issueNumber).Scan(&issueID, &previousMilestoneID)
	if errors.Is(err, pgx.ErrNoRows) {
		return platform.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock issue for milestone removal: %w", err)
	}
	if previousMilestoneID == nil {
		return commit(ctx, tx, "milestone removal")
	}
	if _, err := tx.Exec(ctx, `
		UPDATE issues SET milestone_id = NULL, updated_at = $1 WHERE id = $2
	`, nowUTC(), issueID); err != nil {
		return fmt.Errorf("remove issue milestone: %w", err)
	}
	payload := map[string]any{"issueNumber": issueNumber, "milestone": nil}
	if err := insertAudit(
		ctx, tx, actor.ID, repository, "issue.milestone.remove", "issue", issueID,
	); err != nil {
		return err
	}
	if err := insertOutbox(ctx, tx, "issue.milestone.updated", issueID+":"+uuid.NewString(), payload); err != nil {
		return err
	}
	return commit(ctx, tx, "milestone removal")
}

func milestoneSummary(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
	number int64,
) (collab.MilestoneSummary, error) {
	var milestone collab.MilestoneSummary
	err := tx.QueryRow(ctx, `
		SELECT id, number, title, state, to_char(due_on, 'YYYY-MM-DD')
		FROM repository_milestones
		WHERE repository_id = $1 AND number = $2
		FOR SHARE
	`, repositoryID, number).Scan(
		&milestone.ID, &milestone.Number, &milestone.Title, &milestone.State, &milestone.DueOn,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return collab.MilestoneSummary{}, platform.ErrNotFound
	}
	if err != nil {
		return collab.MilestoneSummary{}, fmt.Errorf("get milestone for assignment: %w", err)
	}
	return milestone, nil
}
