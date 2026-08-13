package projects

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

func (s *store) beginWrite(
	ctx context.Context,
	actor platform.User,
	repo RepositoryRef,
	operation string,
) (pgx.Tx, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin %s: %w", operation, err)
	}
	allowed, err := repositoryWriteAllowed(ctx, tx, actor.ID, repo)
	if err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return nil, err
	}
	if !allowed {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return nil, platform.ErrForbidden
	}
	return tx, nil
}

func repositoryWriteAllowed(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repo RepositoryRef,
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
					  AND membership.role IN ('write', 'maintain', 'admin')
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
					  AND role.role IN ('write', 'maintain', 'admin')
				)
			  )
		)
	`, repo.ID, repo.OrganizationID, actorID).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("authorize project write: %w", err)
	}
	return allowed, nil
}

func lockProject(ctx context.Context, tx pgx.Tx, repoID string, number int64) (string, error) {
	var projectID string
	err := tx.QueryRow(ctx, `
		SELECT id FROM projects WHERE repository_id = $1 AND number = $2 FOR UPDATE
	`, repoID, number).Scan(&projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", platform.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("lock project: %w", err)
	}
	return projectID, nil
}

func insertAudit(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repo RepositoryRef,
	action string,
	targetType string,
	targetID string,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			id, organization_id, repository_id, actor_id, action, target_type, target_id
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, uuid.NewString(), repo.OrganizationID, repo.ID, actorID, action, targetType, targetID)
	if err != nil {
		return fmt.Errorf("record project audit event: %w", err)
	}
	return nil
}

func insertOutbox(
	ctx context.Context,
	tx pgx.Tx,
	topic string,
	targetID string,
	payload any,
) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode project outbox payload: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, $2, $3, $4)
	`, uuid.NewString(), topic, targetID+":"+uuid.NewString(), encoded)
	if err != nil {
		return fmt.Errorf("record project outbox event: %w", err)
	}
	return nil
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

func constraintError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return fmt.Errorf("%s: %w", operation, platform.ErrConflict)
		case "23514":
			return fmt.Errorf("%s: %w", operation, ErrInvalidInput)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func nowUTC() time.Time {
	return time.Now().UTC()
}
