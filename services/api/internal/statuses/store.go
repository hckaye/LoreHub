package statuses

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

func (store *store) beginWrite(
	ctx context.Context,
	actor platform.User,
	repository RepositoryRef,
) (pgx.Tx, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, fmt.Errorf("begin revision status create: %w", err)
	}
	allowed, err := statusWriteAllowed(ctx, tx, actor.ID, repository)
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

func statusWriteAllowed(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repository RepositoryRef,
) (bool, error) {
	var allowed bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM repositories repository
			JOIN organizations organization
			  ON organization.id = repository.organization_id AND organization.active
			JOIN users actor ON actor.id = $3 AND actor.status = 'active'
			LEFT JOIN organization_memberships organization_member
			  ON organization_member.organization_id = organization.id
			 AND organization_member.user_id = actor.id AND organization_member.active
			WHERE repository.id = $1 AND repository.organization_id = $2
			  AND repository.lifecycle_state = 'active'
			  AND repository.archived_at IS NULL AND repository.migrating_at IS NULL
			  AND (
			    organization_member.role = 'owner'
			    OR EXISTS (
			      SELECT 1 FROM repository_memberships direct_access
			      WHERE direct_access.repository_id = repository.id
			        AND direct_access.user_id = actor.id AND direct_access.active
			        AND direct_access.role IN ('write', 'maintain', 'admin')
			    )
			    OR EXISTS (
			      SELECT 1
			      FROM team_repository_roles role
			      JOIN teams team
			        ON team.id = role.team_id
			       AND team.organization_id = organization.id AND team.active
			      JOIN team_memberships team_member
			        ON team_member.team_id = team.id
			       AND team_member.user_id = actor.id AND team_member.active
			      WHERE role.repository_id = repository.id AND role.active
			        AND role.role IN ('write', 'maintain', 'admin')
			        AND organization_member.user_id IS NOT NULL
			    )
			  )
		)
	`, repository.ID, repository.OrganizationID, actorID).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("authorize revision status create: %w", err)
	}
	return allowed, nil
}

func recordCreate(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repository RepositoryRef,
	status Status,
) error {
	details := map[string]any{
		"action":         "revision_status.created",
		"actorId":        actorID,
		"context":        status.Context,
		"description":    status.Description,
		"organizationId": repository.OrganizationID,
		"repositoryId":   repository.ID,
		"revision":       status.Revision,
		"state":          status.State,
		"targetId":       status.ID,
		"targetUrl":      status.TargetURL,
		"targetType":     "revision_status",
	}
	payload, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("encode revision status event: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			id, organization_id, repository_id, actor_id,
			action, target_type, target_id, details
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, uuid.NewString(), repository.OrganizationID, repository.ID, actorID,
		"revision_status.created", "revision_status", status.ID, payload); err != nil {
		return fmt.Errorf("record revision status audit event: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, 'revision_status.created', $2, $3)
	`, uuid.NewString(), status.ID, payload); err != nil {
		return fmt.Errorf("record revision status outbox event: %w", err)
	}
	return nil
}

func translateStoreError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503":
			return fmt.Errorf("%s: %w", operation, platform.ErrForbidden)
		case "23505":
			return fmt.Errorf("%s: %w", operation, platform.ErrConflict)
		case "22001", "23514":
			return fmt.Errorf("%s: %w", operation, ErrInvalidInput)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func serializationFailure(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) &&
		(postgresError.Code == "40001" || postgresError.Code == "40P01")
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(context.WithoutCancel(ctx))
}

func commit(ctx context.Context, tx pgx.Tx, operation string) error {
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit %s: %w", operation, err)
	}
	return nil
}

type rowScanner interface {
	Scan(...any) error
}
