package platform

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (store *Store) enforceOrganizationLimit(ctx context.Context, tx pgx.Tx, userID string) error {
	limit, err := store.organizationsPerUserLimit(ctx, tx)
	if err != nil {
		return err
	}
	if limit <= 0 {
		return nil
	}
	var lockedUserID string
	err = tx.QueryRow(ctx, `
		SELECT id FROM users WHERE id = $1 AND status = 'active' FOR UPDATE
	`, userID).Scan(&lockedUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return fmt.Errorf("lock user organization quota: %w", err)
	}
	var count int
	err = tx.QueryRow(ctx, `
		SELECT count(*) FROM organizations WHERE created_by = $1 AND active
	`, userID).Scan(&count)
	if err != nil {
		return fmt.Errorf("count user organizations: %w", err)
	}
	if int64(count) >= limit {
		return ErrOrganizationLimit
	}
	return nil
}

func (store *Store) lockOrganizationAndEnforceRepositoryLimit(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
) error {
	limit, err := store.repositoriesPerOrganizationLimit(ctx, tx)
	if err != nil {
		return err
	}
	if limit <= 0 {
		return nil
	}
	var lockedOrganizationID string
	if err := tx.QueryRow(ctx, `
		SELECT id FROM organizations WHERE id = $1 FOR UPDATE
	`, organizationID).Scan(&lockedOrganizationID); err != nil {
		return fmt.Errorf("lock organization repository quota: %w", err)
	}
	return store.rejectIfRepositoryCountReached(ctx, tx, organizationID, limit)
}

func (store *Store) rejectIfRepositoryLimitReached(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
) error {
	limit, err := store.repositoriesPerOrganizationLimit(ctx, tx)
	if err != nil {
		return err
	}
	return store.rejectIfRepositoryCountReached(ctx, tx, organizationID, limit)
}

func (store *Store) rejectIfRepositoryCountReached(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	limit int64,
) error {
	if limit <= 0 {
		return nil
	}
	var count int
	err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM repositories
		WHERE organization_id = $1
		  AND lifecycle_state IN ('pending', 'active', 'failed')
	`, organizationID).Scan(&count)
	if err != nil {
		return fmt.Errorf("count organization repositories: %w", err)
	}
	if int64(count) >= limit {
		return ErrRepositoryLimit
	}
	return nil
}

func (store *Store) organizationsPerUserLimit(ctx context.Context, query instanceSettingsQuery) (int64, error) {
	override, err := readMaxOrganizationsPerUserOverride(ctx, query)
	if err != nil {
		return 0, err
	}
	return effectiveInt64Override(override, int64(store.maxOrganizationsPerUser)), nil
}

func (store *Store) repositoriesPerOrganizationLimit(
	ctx context.Context,
	query instanceSettingsQuery,
) (int64, error) {
	override, err := readMaxRepositoriesPerOrganizationOverride(ctx, query)
	if err != nil {
		return 0, err
	}
	return effectiveInt64Override(override, int64(store.maxRepositoriesPerOrganization)), nil
}

func (store *Store) maxRepositorySizeBytesLimit(
	ctx context.Context,
	query instanceSettingsQuery,
) (int64, error) {
	override, err := readMaxRepositorySizeBytesOverride(ctx, query)
	if err != nil {
		return 0, err
	}
	return effectiveInt64Override(override, store.maxRepositorySizeBytes), nil
}
