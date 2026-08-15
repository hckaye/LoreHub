package platform

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func (store *Store) enforceOrganizationLimit(ctx context.Context, tx pgx.Tx, userID string) error {
	if store.maxOrganizationsPerUser <= 0 {
		return nil
	}
	var lockedUserID string
	err := tx.QueryRow(ctx, `
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
	if count >= store.maxOrganizationsPerUser {
		return ErrOrganizationLimit
	}
	return nil
}

func (store *Store) lockOrganizationAndEnforceRepositoryLimit(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
) error {
	if store.maxRepositoriesPerOrganization <= 0 {
		return nil
	}
	var lockedOrganizationID string
	if err := tx.QueryRow(ctx, `
		SELECT id FROM organizations WHERE id = $1 FOR UPDATE
	`, organizationID).Scan(&lockedOrganizationID); err != nil {
		return fmt.Errorf("lock organization repository quota: %w", err)
	}
	return store.rejectIfRepositoryLimitReached(ctx, tx, organizationID)
}

func (store *Store) rejectIfRepositoryLimitReached(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
) error {
	if store.maxRepositoriesPerOrganization <= 0 {
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
	if count >= store.maxRepositoriesPerOrganization {
		return ErrRepositoryLimit
	}
	return nil
}
