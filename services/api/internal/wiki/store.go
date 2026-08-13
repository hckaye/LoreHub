package wiki

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
	operation string,
) (pgx.Tx, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin %s: %w", operation, err)
	}
	allowed, err := repositoryWriteAllowed(ctx, tx, actor.ID, repository)
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

func repositoryWriteAllowed(
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
					JOIN teams team ON team.id = role.team_id
					  AND team.organization_id = organization.id AND team.active
					JOIN team_memberships team_membership ON team_membership.team_id = team.id
					  AND team_membership.user_id = actor.id AND team_membership.active
					JOIN organization_memberships organization_membership
					  ON organization_membership.organization_id = organization.id
					  AND organization_membership.user_id = actor.id AND organization_membership.active
					WHERE role.repository_id = repository.id AND role.active
					  AND role.role IN ('write', 'maintain', 'admin')
				)
			  )
		)
	`, repository.ID, repository.OrganizationID, actorID).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("authorize wiki write: %w", err)
	}
	return allowed, nil
}

func insertAudit(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repository RepositoryRef,
	action string,
	pageID string,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_events (
			id, organization_id, repository_id, actor_id, action, target_type, target_id
		) VALUES ($1, $2, $3, $4, $5, 'wiki_page', $6)
	`, uuid.NewString(), repository.OrganizationID, repository.ID, actorID, action, pageID)
	if err != nil {
		return fmt.Errorf("record wiki audit event: %w", err)
	}
	return nil
}

func insertOutbox(
	ctx context.Context,
	tx pgx.Tx,
	topic string,
	repositoryID string,
	page Page,
) error {
	payload, err := json.Marshal(map[string]any{
		"repositoryId": repositoryID,
		"pageId":       page.ID,
		"slug":         page.Slug,
		"title":        page.Title,
		"version":      page.Version,
	})
	if err != nil {
		return fmt.Errorf("encode wiki outbox payload: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, $2, $3, $4)
	`, uuid.NewString(), topic, page.ID+":"+uuid.NewString(), payload)
	if err != nil {
		return fmt.Errorf("record wiki outbox event: %w", err)
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

func storeError(operation string, err error) error {
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
