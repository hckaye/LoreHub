package milestones

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

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

func (store *store) beginMutation(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
	roles []string,
	operation string,
) (pgx.Tx, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin %s: %w", operation, err)
	}
	allowed, err := mutationAllowed(ctx, tx, actor.ID, repository, roles)
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

func mutationAllowed(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repository RepositoryRef,
	roles []string,
) (bool, error) {
	var allowed bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM repositories repository
			JOIN organizations organization
			  ON organization.id = repository.organization_id AND organization.active
			JOIN users actor ON actor.id = $3 AND actor.status = 'active'
			WHERE repository.id = $1 AND repository.organization_id = $2
			  AND repository.archived_at IS NULL AND repository.migrating_at IS NULL
			  AND repository.lifecycle_state = 'active'
			  AND (
				EXISTS (
					SELECT 1 FROM organization_memberships membership
					WHERE membership.organization_id = organization.id
					  AND membership.user_id = actor.id AND membership.active
					  AND membership.role = 'owner'
				)
				OR EXISTS (
					SELECT 1 FROM repository_memberships membership
					WHERE membership.repository_id = repository.id
					  AND membership.user_id = actor.id AND membership.active
					  AND membership.role = ANY($4::varchar[])
				)
				OR EXISTS (
					SELECT 1
					FROM team_repository_roles role
					JOIN teams team
					  ON team.id = role.team_id
					 AND team.organization_id = organization.id
					 AND team.active
					JOIN team_memberships team_membership
					  ON team_membership.team_id = team.id
					 AND team_membership.user_id = actor.id
					 AND team_membership.active
					JOIN organization_memberships organization_membership
					  ON organization_membership.organization_id = organization.id
					 AND organization_membership.user_id = actor.id
					 AND organization_membership.active
					WHERE role.repository_id = repository.id AND role.active
					  AND role.role = ANY($4::varchar[])
				)
			  )
		)
	`, repository.ID, repository.OrganizationID, actorID, roles).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("authorize milestone mutation: %w", err)
	}
	return allowed, nil
}

func insertAudit(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repository RepositoryRef,
	action string,
	targetType string,
	targetID string,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			id, organization_id, repository_id, actor_id, action, target_type, target_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, uuid.NewString(), repository.OrganizationID, repository.ID, actorID, action, targetType, targetID)
	if err != nil {
		return fmt.Errorf("record milestone audit event: %w", err)
	}
	return nil
}

func insertOutbox(ctx context.Context, tx pgx.Tx, topic, eventKey string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode milestone outbox payload: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, $2, $3, $4)
	`, uuid.NewString(), topic, eventKey, encoded)
	if err != nil {
		return fmt.Errorf("record milestone outbox event: %w", err)
	}
	return nil
}

func constraintError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return fmt.Errorf("%s: %w", operation, platform.ErrConflict)
		case "23503":
			return fmt.Errorf("%s: %w", operation, platform.ErrNotFound)
		case "23514", "22001", "22007":
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

func nowUTC() time.Time {
	return time.Now().UTC()
}

var milestoneWriteRoles = []string{"write", "maintain", "admin"}
var milestoneAssignRoles = []string{"triage", "write", "maintain", "admin"}
