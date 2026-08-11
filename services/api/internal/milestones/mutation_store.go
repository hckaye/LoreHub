package milestones

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
	input CreateInput,
) (Milestone, error) {
	input, err := validateCreate(input)
	if err != nil {
		return Milestone{}, err
	}
	tx, err := store.beginMutation(ctx, actor, repository, milestoneWriteRoles, "milestone creation")
	if err != nil {
		return Milestone{}, err
	}
	defer rollback(ctx, tx)
	var number int64
	err = tx.QueryRow(ctx, `
		UPDATE repository_counters
		SET next_milestone_number = next_milestone_number + 1
		WHERE repository_id = $1
		RETURNING next_milestone_number - 1
	`, repository.ID).Scan(&number)
	if errors.Is(err, pgx.ErrNoRows) {
		return Milestone{}, platform.ErrNotFound
	}
	if err != nil {
		return Milestone{}, fmt.Errorf("allocate milestone number: %w", err)
	}
	id := uuid.NewString()
	now := nowUTC()
	_, err = tx.Exec(ctx, `
		INSERT INTO repository_milestones (
			id, repository_id, number, title, description, due_on,
			created_by, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6::date, $7, $8, $8)
	`, id, repository.ID, number, input.Title, input.Description, input.DueOn, actor.ID, now)
	if err != nil {
		return Milestone{}, constraintError("create milestone", err)
	}
	return store.finishMilestoneMutation(
		ctx, tx, actor, repository, number,
		"milestone.create", "milestone.created", id, "milestone creation",
	)
}

func (store *store) Update(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	number int64,
	input UpdateInput,
) (Milestone, error) {
	input, err := validateUpdate(input)
	if err != nil {
		return Milestone{}, err
	}
	tx, err := store.beginMutation(ctx, actor, repository, milestoneWriteRoles, "milestone update")
	if err != nil {
		return Milestone{}, err
	}
	defer rollback(ctx, tx)
	current, err := lockMilestone(ctx, tx, repository.ID, number, input.ExpectedVersion)
	if err != nil {
		return Milestone{}, err
	}
	if input.Title != nil {
		current.Title = *input.Title
	}
	if input.Description != nil {
		current.Description = *input.Description
	}
	if input.State != nil {
		current.State = *input.State
	}
	if input.DueOnSet {
		current.DueOn = input.DueOn
	}
	now := nowUTC()
	closedBy := current.ClosedBy
	closedAt := current.ClosedAt
	if current.State != current.OriginalState {
		if current.State == "closed" {
			closedBy = &actor.ID
			closedAt = &now
		} else {
			closedBy = nil
			closedAt = nil
		}
	}
	_, err = tx.Exec(ctx, `
		UPDATE repository_milestones
		SET title = $1, description = $2, state = $3, due_on = $4::date,
		    closed_by = $5, closed_at = $6, version = version + 1, updated_at = $7
		WHERE repository_id = $8 AND number = $9
	`, current.Title, current.Description, current.State, current.DueOn,
		closedBy, closedAt, now, repository.ID, number)
	if err != nil {
		return Milestone{}, constraintError("update milestone", err)
	}
	action := "milestone.update"
	topic := "milestone.updated"
	if input.State != nil && *input.State != current.OriginalState {
		if *input.State == "closed" {
			action, topic = "milestone.close", "milestone.closed"
		} else {
			action, topic = "milestone.reopen", "milestone.reopened"
		}
	}
	return store.finishMilestoneMutation(
		ctx, tx, actor, repository, number, action, topic,
		current.ID+":"+uuid.NewString(), "milestone update",
	)
}

func (store *store) Delete(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	number int64,
	expectedVersion int64,
) error {
	if expectedVersion < 1 {
		return invalid("expectedVersion must be positive")
	}
	tx, err := store.beginMutation(ctx, actor, repository, milestoneWriteRoles, "milestone deletion")
	if err != nil {
		return err
	}
	defer rollback(ctx, tx)
	locked, err := lockMilestone(ctx, tx, repository.ID, number, expectedVersion)
	if err != nil {
		return err
	}
	snapshot, err := loadMilestone(ctx, tx, repository.ID, number)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE issues SET milestone_id = NULL, updated_at = $1
		WHERE repository_id = $2 AND milestone_id = $3
	`, nowUTC(), repository.ID, locked.ID); err != nil {
		return fmt.Errorf("detach milestone issues: %w", err)
	}
	command, err := tx.Exec(ctx, `
		DELETE FROM repository_milestones WHERE repository_id = $1 AND number = $2
	`, repository.ID, number)
	if err != nil {
		return fmt.Errorf("delete milestone: %w", err)
	}
	if command.RowsAffected() != 1 {
		return platform.ErrNotFound
	}
	if err := insertAudit(ctx, tx, actor.ID, repository, "milestone.delete", "milestone", locked.ID); err != nil {
		return err
	}
	if err := insertOutbox(ctx, tx, "milestone.deleted", locked.ID, snapshot); err != nil {
		return err
	}
	return commit(ctx, tx, "milestone deletion")
}

type lockedMilestone struct {
	ID            string
	Title         string
	Description   string
	State         string
	OriginalState string
	DueOn         *string
	ClosedBy      *string
	ClosedAt      *time.Time
}

func lockMilestone(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
	number int64,
	expectedVersion int64,
) (lockedMilestone, error) {
	var milestone lockedMilestone
	var storedVersion int64
	err := tx.QueryRow(ctx, `
		SELECT id, title, description, state, to_char(due_on, 'YYYY-MM-DD'),
		       closed_by, closed_at, version
		FROM repository_milestones
		WHERE repository_id = $1 AND number = $2
		FOR UPDATE
	`, repositoryID, number).Scan(
		&milestone.ID, &milestone.Title, &milestone.Description,
		&milestone.State, &milestone.DueOn, &milestone.ClosedBy,
		&milestone.ClosedAt, &storedVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedMilestone{}, platform.ErrNotFound
	}
	if err != nil {
		return lockedMilestone{}, fmt.Errorf("lock milestone: %w", err)
	}
	if storedVersion != expectedVersion {
		return lockedMilestone{}, ErrVersionConflict
	}
	milestone.OriginalState = milestone.State
	return milestone, nil
}

func (store *store) finishMilestoneMutation(
	ctx context.Context,
	tx pgx.Tx,
	actor platform.User,
	repository RepositoryRef,
	number int64,
	action string,
	topic string,
	eventKey string,
	operation string,
) (Milestone, error) {
	milestone, err := loadMilestone(ctx, tx, repository.ID, number)
	if err != nil {
		return Milestone{}, err
	}
	if err := insertAudit(ctx, tx, actor.ID, repository, action, "milestone", milestone.ID); err != nil {
		return Milestone{}, err
	}
	if err := insertOutbox(ctx, tx, topic, eventKey, milestone); err != nil {
		return Milestone{}, err
	}
	if err := commit(ctx, tx, operation); err != nil {
		return Milestone{}, err
	}
	milestone.ViewerCanWrite = true
	return milestone, nil
}
