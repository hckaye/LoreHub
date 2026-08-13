package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (store *Store) BeginRepositoryMigration(
	ctx context.Context,
	actor User,
	owner string,
	slug string,
	targetServerID string,
) (RepositoryMigration, Repository, LoreServer, error) {
	targetServerID = strings.TrimSpace(targetServerID)
	parsedTargetServerID, err := uuid.Parse(targetServerID)
	if err != nil {
		return RepositoryMigration{}, Repository{}, LoreServer{}, ErrInvalidInput
	}
	targetServerID = parsedTargetServerID.String()
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return RepositoryMigration{}, Repository{}, LoreServer{},
			fmt.Errorf("begin repository migration transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	repository, err := repositoryForMigration(ctx, transaction, owner, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return RepositoryMigration{}, Repository{}, LoreServer{}, ErrNotFound
	}
	if err != nil {
		return RepositoryMigration{}, Repository{}, LoreServer{},
			fmt.Errorf("find repository for migration: %w", err)
	}
	if repository.ArchivedAt != nil || repository.MigratingAt != nil || repository.LifecycleState != "active" {
		return RepositoryMigration{}, Repository{}, LoreServer{}, ErrConflict
	}
	if repository.LoreServerID == "" || repository.LoreRepositoryID == "" {
		return RepositoryMigration{}, Repository{}, LoreServer{}, ErrInvalidInput
	}
	if repository.LoreServerID == targetServerID {
		return RepositoryMigration{}, Repository{}, LoreServer{}, ErrInvalidInput
	}
	target, err := activeVisibleLoreServer(ctx, transaction, repository.OrganizationID, targetServerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return RepositoryMigration{}, Repository{}, LoreServer{},
			&LoreServerSelectionError{Reason: LoreServerSelectionExplicitUnavailable}
	}
	if err != nil {
		return RepositoryMigration{}, Repository{}, LoreServer{},
			fmt.Errorf("resolve migration target Lore server: %w", err)
	}

	now := time.Now().UTC()
	migration := RepositoryMigration{
		ID:           uuid.NewString(),
		RepositoryID: repository.ID,
		FromServerID: repository.LoreServerID,
		ToServerID:   target.ID,
		State:        RepositoryMigrationPending,
		CreatedBy:    actor.ID,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	_, err = transaction.Exec(ctx, `
		INSERT INTO repository_migrations (
			id, repository_id, from_server_id, to_server_id, state, created_by,
			created_at, started_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $7)
	`, migration.ID, migration.RepositoryID, migration.FromServerID, migration.ToServerID,
		migration.State, migration.CreatedBy, migration.CreatedAt, migration.StartedAt)
	if err != nil {
		return RepositoryMigration{}, Repository{}, LoreServer{},
			translateConstraintError("create repository migration", err)
	}
	result, err := transaction.Exec(ctx, `
		UPDATE repositories
		SET migrating_at = $2, updated_at = $2
		WHERE id = $1 AND migrating_at IS NULL
	`, repository.ID, now)
	if err != nil {
		return RepositoryMigration{}, Repository{}, LoreServer{},
			fmt.Errorf("mark repository as migrating: %w", err)
	}
	if result.RowsAffected() != 1 {
		return RepositoryMigration{}, Repository{}, LoreServer{}, ErrConflict
	}
	if err := insertAuditDetails(ctx, transaction, actor.ID, repository.OrganizationID, repository.ID,
		"repository.migration.started", "repository_migration", migration.ID, map[string]any{
			"fromServerId": migration.FromServerID,
			"toServerId":   migration.ToServerID,
			"state":        migration.State,
		}); err != nil {
		return RepositoryMigration{}, Repository{}, LoreServer{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return RepositoryMigration{}, Repository{}, LoreServer{},
			fmt.Errorf("commit repository migration start: %w", err)
	}
	repository.MigratingAt = &now
	repository.UpdatedAt = now
	return migration, repository, target, nil
}

func (store *Store) MarkRepositoryMigrationMirroring(ctx context.Context, migrationID string) error {
	return store.updateRepositoryMigrationState(ctx, migrationID,
		RepositoryMigrationPending, RepositoryMigrationMirroring)
}

func (store *Store) MarkRepositoryMigrationRepointing(ctx context.Context, migrationID string) error {
	return store.updateRepositoryMigrationState(ctx, migrationID,
		RepositoryMigrationMirroring, RepositoryMigrationRepointing)
}

func (store *Store) updateRepositoryMigrationState(
	ctx context.Context,
	migrationID string,
	fromState string,
	toState string,
) error {
	if _, err := uuid.Parse(strings.TrimSpace(migrationID)); err != nil {
		return ErrInvalidInput
	}
	result, err := store.pool.Exec(ctx, `
		UPDATE repository_migrations
		SET state = $2,
		    started_at = CASE WHEN $2 = 'mirroring' THEN COALESCE(started_at, now()) ELSE started_at END,
		    updated_at = now()
		WHERE id = $1 AND state = $3
	`, migrationID, toState, fromState)
	if err != nil {
		return fmt.Errorf("advance repository migration to %s: %w", toState, err)
	}
	if result.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

func (store *Store) CompleteRepositoryMigration(ctx context.Context, migrationID string) error {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin repository migration completion: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()

	var migration RepositoryMigration
	var targetPublicURL string
	err = transaction.QueryRow(ctx, `
		SELECT migration.id, migration.repository_id, migration.from_server_id,
		       migration.to_server_id, migration.state, migration.error_text,
		       migration.created_by, migration.created_at, migration.started_at,
		       migration.completed_at, migration.updated_at, server.public_url
		FROM repository_migrations migration
		JOIN lore_servers server
		  ON server.id = migration.to_server_id
		 AND server.status = 'active' AND server.revoked_at IS NULL
		WHERE migration.id = $1
		FOR UPDATE OF migration, server
	`, migrationID).Scan(
		&migration.ID, &migration.RepositoryID, &migration.FromServerID, &migration.ToServerID,
		&migration.State, &migration.ErrorText, &migration.CreatedBy, &migration.CreatedAt,
		&migration.StartedAt, &migration.CompletedAt, &migration.UpdatedAt, &targetPublicURL,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("find repository migration for completion: %w", err)
	}
	if migration.State != RepositoryMigrationRepointing {
		return ErrConflict
	}
	var loreRepositoryID, organizationID string
	err = transaction.QueryRow(ctx, `
		SELECT lore_repository_id, organization_id
		FROM repositories
		WHERE id = $1 AND migrating_at IS NOT NULL
		FOR UPDATE
	`, migration.RepositoryID).Scan(&loreRepositoryID, &organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return fmt.Errorf("lock repository for migration completion: %w", err)
	}
	targetURL, err := loreRepositoryURL(targetPublicURL, loreRepositoryID)
	if err != nil {
		return fmt.Errorf("build migrated Lore repository URL: %w", err)
	}
	result, err := transaction.Exec(ctx, `
		UPDATE repositories
		SET lore_url = $2, lore_server_id = $3, migrating_at = NULL, updated_at = now()
		WHERE id = $1 AND migrating_at IS NOT NULL
	`, migration.RepositoryID, targetURL, migration.ToServerID)
	if err != nil {
		return fmt.Errorf("repoint repository Lore server: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrConflict
	}
	completedAt := time.Now().UTC()
	_, err = transaction.Exec(ctx, `
		UPDATE repository_migrations
		SET state = $2, completed_at = $3, updated_at = $3
		WHERE id = $1 AND state = $4
	`, migration.ID, RepositoryMigrationCompleted, completedAt, RepositoryMigrationRepointing)
	if err != nil {
		return fmt.Errorf("complete repository migration record: %w", err)
	}
	if err := insertAuditDetails(ctx, transaction, migration.CreatedBy, organizationID, migration.RepositoryID,
		"repository.migration.completed", "repository_migration", migration.ID, map[string]any{
			"fromServerId": migration.FromServerID,
			"toServerId":   migration.ToServerID,
		}); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit repository migration completion: %w", err)
	}
	return nil
}

func (store *Store) FailRepositoryMigration(ctx context.Context, migrationID string, failure error) error {
	if failure == nil {
		return ErrInvalidInput
	}
	message := limitText(strings.TrimSpace(failure.Error()), 4096)
	if message == "" {
		message = "repository migration failed"
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin repository migration failure: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	var migration RepositoryMigration
	var organizationID string
	err = transaction.QueryRow(ctx, `
		SELECT migration.id, migration.repository_id, migration.from_server_id,
		       migration.to_server_id, migration.state, migration.created_by,
		       repository.organization_id
		FROM repository_migrations migration
		JOIN repositories repository ON repository.id = migration.repository_id
		WHERE migration.id = $1
		FOR UPDATE OF migration, repository
	`, migrationID).Scan(
		&migration.ID, &migration.RepositoryID, &migration.FromServerID, &migration.ToServerID,
		&migration.State, &migration.CreatedBy, &organizationID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("find repository migration for failure: %w", err)
	}
	if migration.State == RepositoryMigrationCompleted || migration.State == RepositoryMigrationFailed {
		return ErrConflict
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE repositories
		SET migrating_at = NULL, updated_at = now()
		WHERE id = $1
	`, migration.RepositoryID); err != nil {
		return fmt.Errorf("lift repository migration read-only state: %w", err)
	}
	failedAt := time.Now().UTC()
	if _, err := transaction.Exec(ctx, `
		UPDATE repository_migrations
		SET state = $2, error_text = $3, completed_at = $4, updated_at = $4
		WHERE id = $1
	`, migration.ID, RepositoryMigrationFailed, message, failedAt); err != nil {
		return fmt.Errorf("record repository migration failure: %w", err)
	}
	if err := insertAuditDetails(ctx, transaction, migration.CreatedBy, organizationID, migration.RepositoryID,
		"repository.migration.failed", "repository_migration", migration.ID, map[string]any{
			"fromServerId": migration.FromServerID,
			"toServerId":   migration.ToServerID,
			"error":        message,
		}); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit repository migration failure: %w", err)
	}
	return nil
}

func (store *Store) ListRepositoryMigrations(
	ctx context.Context,
	owner string,
	slug string,
) ([]RepositoryMigration, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT migration.id, migration.repository_id, migration.from_server_id,
		       migration.to_server_id, migration.state, migration.error_text,
		       migration.created_by, migration.created_at, migration.started_at,
		       migration.completed_at, migration.updated_at
		FROM repository_migrations migration
		JOIN repositories repository ON repository.id = migration.repository_id
		JOIN organizations organization ON organization.id = repository.organization_id
		WHERE organization.slug = $1 AND repository.slug = $2
		ORDER BY migration.created_at DESC, migration.id DESC
		LIMIT 100
	`, owner, slug)
	if err != nil {
		return nil, fmt.Errorf("list repository migrations: %w", err)
	}
	defer rows.Close()
	migrations := make([]RepositoryMigration, 0)
	for rows.Next() {
		var migration RepositoryMigration
		if err := rows.Scan(
			&migration.ID, &migration.RepositoryID, &migration.FromServerID, &migration.ToServerID,
			&migration.State, &migration.ErrorText, &migration.CreatedBy, &migration.CreatedAt,
			&migration.StartedAt, &migration.CompletedAt, &migration.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan repository migration: %w", err)
		}
		migrations = append(migrations, migration)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repository migrations: %w", err)
	}
	return migrations, nil
}

func repositoryForMigration(
	ctx context.Context,
	query interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	owner string,
	slug string,
) (Repository, error) {
	var repository Repository
	err := query.QueryRow(ctx, `
		SELECT repository.id, repository.organization_id, organization.slug, repository.slug,
		       repository.display_name, repository.description, repository.visibility,
		       repository.lore_repository_id, repository.lore_url,
		       COALESCE(repository.lore_server_id::text, ''), repository.default_branch,
		       repository.archived_at, repository.migrating_at, repository.lifecycle_state,
		       repository.updated_at
		FROM repositories repository
		JOIN organizations organization
		  ON organization.id = repository.organization_id AND organization.active
		WHERE organization.slug = $1 AND repository.slug = $2
		FOR UPDATE OF repository
	`, owner, slug).Scan(
		&repository.ID, &repository.OrganizationID, &repository.Owner, &repository.Slug,
		&repository.DisplayName, &repository.Description, &repository.Visibility,
		&repository.LoreRepositoryID, &repository.LoreURL, &repository.LoreServerID,
		&repository.DefaultBranch, &repository.ArchivedAt, &repository.MigratingAt,
		&repository.LifecycleState, &repository.UpdatedAt,
	)
	return repository, err
}
