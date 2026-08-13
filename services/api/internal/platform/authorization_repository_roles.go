package platform

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func (store *Store) SetTeamRepositoryRole(
	ctx context.Context,
	actor User,
	organizationSlug string,
	teamSlug string,
	owner string,
	repositorySlug string,
	input SetTeamRepositoryRoleInput,
) (TeamRepositoryRole, error) {
	organizationID, _, err := store.organizationAccess(ctx, actor.ID, organizationSlug)
	if err != nil {
		return TeamRepositoryRole{}, err
	}
	repositoryID, repositoryOrganizationID, err := store.repositoryAdminAccess(ctx, actor.ID, owner, repositorySlug)
	if err != nil {
		return TeamRepositoryRole{}, err
	}
	if repositoryOrganizationID != organizationID || !validRepositoryRole(input.Role) {
		return TeamRepositoryRole{}, ErrForbidden
	}
	var result TeamRepositoryRole
	err = store.pool.QueryRow(ctx, `
		SELECT t.id, r.id, o.slug, r.slug
		FROM teams t
		JOIN repositories r ON r.organization_id = t.organization_id
		JOIN organizations o ON o.id = r.organization_id
		WHERE t.organization_id = $1 AND t.slug = $2 AND t.active
		  AND o.slug = $3 AND o.active AND r.slug = $4 AND r.lifecycle_state = 'active'
		  AND r.archived_at IS NULL AND r.migrating_at IS NULL
	`, organizationID, teamSlug, owner, repositorySlug).Scan(&result.TeamID, &result.RepositoryID,
		&result.Owner, &result.Repository)
	if errors.Is(err, pgx.ErrNoRows) {
		return TeamRepositoryRole{}, ErrNotFound
	}
	if err != nil {
		return TeamRepositoryRole{}, fmt.Errorf("find team repository: %w", err)
	}
	if result.RepositoryID != repositoryID {
		return TeamRepositoryRole{}, ErrNotFound
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TeamRepositoryRole{}, fmt.Errorf("begin team repository role transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	_, err = transaction.Exec(ctx, `
		INSERT INTO team_repository_roles (team_id, repository_id, role, created_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (team_id, repository_id) DO UPDATE
		SET role = EXCLUDED.role, updated_at = now()
	`, result.TeamID, result.RepositoryID, input.Role, actor.ID)
	if err != nil {
		return TeamRepositoryRole{}, fmt.Errorf("set team repository role: %w", err)
	}
	result.Role = input.Role
	result.CreatedAt = time.Now().UTC()
	result.UpdatedAt = result.CreatedAt
	if err := insertAuditDetails(ctx, transaction, actor.ID, organizationID, result.RepositoryID,
		"team.repository_role.set", "team", result.TeamID,
		map[string]any{"repositoryId": result.RepositoryID, "role": input.Role}); err != nil {
		return TeamRepositoryRole{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return TeamRepositoryRole{}, fmt.Errorf("commit team repository role transaction: %w", err)
	}
	return result, nil
}

func (store *Store) DeleteTeamRepositoryRole(
	ctx context.Context,
	actor User,
	organizationSlug string,
	teamSlug string,
	owner string,
	repositorySlug string,
) error {
	organizationID, _, err := store.organizationAccess(ctx, actor.ID, organizationSlug)
	if err != nil {
		return err
	}
	repositoryID, repositoryOrganizationID, err := store.repositoryAdminAccess(ctx, actor.ID, owner, repositorySlug)
	if err != nil {
		return err
	}
	if repositoryOrganizationID != organizationID {
		return ErrForbidden
	}
	var teamID, foundRepositoryID string
	err = store.pool.QueryRow(ctx, `
		SELECT t.id, r.id
		FROM teams t
		JOIN repositories r ON r.organization_id = t.organization_id
		JOIN organizations o ON o.id = r.organization_id
		WHERE t.organization_id = $1 AND t.slug = $2 AND t.active
		  AND o.slug = $3 AND o.active AND r.slug = $4 AND r.lifecycle_state = 'active'
		  AND r.archived_at IS NULL AND r.migrating_at IS NULL
	`, organizationID, teamSlug, owner, repositorySlug).Scan(&teamID, &foundRepositoryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("find team repository role: %w", err)
	}
	if foundRepositoryID != repositoryID {
		return ErrNotFound
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin team repository role delete transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	command, err := transaction.Exec(ctx, `
		DELETE FROM team_repository_roles
		WHERE team_id = $1 AND repository_id = $2
	`, teamID, repositoryID)
	if err != nil {
		return fmt.Errorf("delete team repository role: %w", err)
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := insertAuditDetails(ctx, transaction, actor.ID, organizationID, repositoryID,
		"team.repository_role.delete", "team", teamID,
		map[string]any{"repositoryId": repositoryID}); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit team repository role delete transaction: %w", err)
	}
	return nil
}
