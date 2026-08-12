package statuses

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func (store *store) Create(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	input CreateInput,
) (CreateResult, error) {
	input, err := validateCreate(input)
	if err != nil {
		return CreateResult{}, err
	}
	var result CreateResult
	for attempt := 0; attempt < 3; attempt++ {
		result, err = store.createOnce(ctx, actor, repository, input)
		if !serializationFailure(err) {
			return result, err
		}
	}
	return CreateResult{}, err
}

func (store *store) createOnce(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	input CreateInput,
) (CreateResult, error) {
	tx, err := store.beginWrite(ctx, actor, repository)
	if err != nil {
		return CreateResult{}, err
	}
	defer rollback(ctx, tx)
	if input.IdempotencyKey != nil {
		existing, err := statusByIdempotencyKey(ctx, tx, repository.ID, *input.IdempotencyKey)
		if err == nil {
			if !sameStatusInput(existing, input) {
				return CreateResult{}, platform.ErrConflict
			}
			if err := commit(ctx, tx, "revision status idempotency read"); err != nil {
				return CreateResult{}, err
			}
			return CreateResult{Status: existing, Created: false}, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return CreateResult{}, fmt.Errorf("read idempotent revision status: %w", err)
		}
	}
	statusID := uuid.NewString()
	row := tx.QueryRow(ctx, `
		INSERT INTO revision_statuses (
			id, repository_id, revision, context, state, description,
			target_url, creator_id, idempotency_key
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (repository_id, idempotency_key)
			WHERE idempotency_key IS NOT NULL DO UPDATE
			SET idempotency_key = EXCLUDED.idempotency_key
		RETURNING id::text, created_at,
		          (SELECT avatar_url FROM users WHERE id = $8)
	`, statusID, repository.ID, input.Revision, input.Context, input.State,
		input.Description, input.TargetURL, actor.ID, input.IdempotencyKey)
	status := Status{
		ID: statusID, Revision: input.Revision, Context: input.Context,
		State: input.State, Description: input.Description, TargetURL: input.TargetURL,
		Creator: Creator{ID: actor.ID, Username: actor.Username, DisplayName: actor.DisplayName},
	}
	var returnedID string
	if err := row.Scan(&returnedID, &status.CreatedAt, &status.Creator.AvatarURL); err != nil {
		return CreateResult{}, translateStoreError("create revision status", err)
	}
	if returnedID != statusID {
		if input.IdempotencyKey == nil {
			return CreateResult{}, errors.New("revision status conflict omitted an idempotency key")
		}
		existing, readErr := statusByIdempotencyKey(ctx, tx, repository.ID, *input.IdempotencyKey)
		if readErr != nil {
			return CreateResult{}, fmt.Errorf("read concurrent idempotent revision status: %w", readErr)
		}
		if !sameStatusInput(existing, input) {
			return CreateResult{}, platform.ErrConflict
		}
		if err := commit(ctx, tx, "revision status idempotency read"); err != nil {
			return CreateResult{}, err
		}
		return CreateResult{Status: existing, Created: false}, nil
	}
	if err := recordCreate(ctx, tx, actor.ID, repository, status); err != nil {
		return CreateResult{}, err
	}
	if err := commit(ctx, tx, "revision status create"); err != nil {
		return CreateResult{}, err
	}
	return CreateResult{Status: status, Created: true}, nil
}

func sameStatusInput(status Status, input CreateInput) bool {
	return status.Revision == input.Revision && status.Context == input.Context &&
		status.State == input.State && status.Description == input.Description &&
		status.TargetURL == input.TargetURL
}

func statusByIdempotencyKey(
	ctx context.Context,
	tx pgx.Tx,
	repositoryID string,
	idempotencyKey string,
) (Status, error) {
	return scanStatus(tx.QueryRow(ctx, statusSelect+`
		WHERE status.repository_id = $1 AND status.idempotency_key = $2
		FOR UPDATE
	`, repositoryID, idempotencyKey))
}
