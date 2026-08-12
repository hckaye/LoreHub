package discussions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) Store {
	return &store{pool: pool}
}

func (store *store) beginAuthorized(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	permission mutationPermission,
	operation string,
) (pgx.Tx, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, fmt.Errorf("begin %s: %w", operation, err)
	}
	allowed, err := discussionPermissionAllowed(ctx, tx, actor.ID, repository, permission)
	if err != nil {
		rollback(ctx, tx)
		return nil, err
	}
	if !allowed {
		rollback(ctx, tx)
		return nil, platform.ErrForbidden
	}
	return tx, nil
}

func recordMutation(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repository RepositoryRef,
	action string,
	targetType string,
	targetID string,
	details map[string]any,
) error {
	eventDetails := make(map[string]any, len(details)+6)
	for key, value := range details {
		eventDetails[key] = value
	}
	eventDetails["repositoryId"] = repository.ID
	eventDetails["organizationId"] = repository.OrganizationID
	eventDetails["actorId"] = actorID
	eventDetails["action"] = action
	eventDetails["targetType"] = targetType
	eventDetails["targetId"] = targetID
	payload, err := json.Marshal(eventDetails)
	if err != nil {
		return fmt.Errorf("encode discussion event: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			id, organization_id, repository_id, actor_id, action, target_type, target_id, details
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, uuid.NewString(), repository.OrganizationID, repository.ID, actorID, action,
		targetType, targetID, payload); err != nil {
		return fmt.Errorf("record discussion audit event: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, $2, $3, $4)
	`, uuid.NewString(), action, targetID+":"+uuid.NewString(), payload); err != nil {
		return fmt.Errorf("record discussion outbox event: %w", err)
	}
	return nil
}

func translateStoreError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23001", "23503":
			return fmt.Errorf("%s: %w", operation, platform.ErrConflict)
		case "23505":
			return fmt.Errorf("%s: %w", operation, platform.ErrConflict)
		case "23514", "22001":
			return fmt.Errorf("%s: %w", operation, ErrInvalidInput)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func commit(ctx context.Context, tx pgx.Tx, operation string) error {
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit %s: %w", operation, err)
	}
	return nil
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(context.WithoutCancel(ctx))
}

type rowScanner interface {
	Scan(...any) error
}

type discussionQueries interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
