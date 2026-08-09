package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (store *Store) ListRepositoryCollaborators(
	ctx context.Context,
	actor User,
	owner string,
	repositorySlug string,
) ([]Collaborator, error) {
	repositoryID, organizationID, err := store.repositoryAdminAccess(ctx, actor.ID, owner, repositorySlug)
	if err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT u.id, u.username, u.display_name, rm.role, rm.active, 'direct'
		FROM repository_memberships rm
		JOIN users u ON u.id = rm.user_id
		JOIN organization_memberships om
		  ON om.organization_id = $2 AND om.user_id = rm.user_id AND om.active
		WHERE rm.repository_id = $1 AND u.status = 'active'
		UNION ALL
		SELECT u.id, u.username, u.display_name, tr.role, tm.active, 'team:' || t.slug
		FROM team_repository_roles tr
		JOIN teams t ON t.id = tr.team_id AND t.organization_id = $2 AND t.active
		JOIN team_memberships tm ON tm.team_id = t.id AND tm.active
		JOIN users u ON u.id = tm.user_id
		JOIN organization_memberships om
		  ON om.organization_id = $2 AND om.user_id = tm.user_id AND om.active
		WHERE tr.active AND u.status = 'active'
		ORDER BY username, source
	`, repositoryID, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list repository collaborators: %w", err)
	}
	defer rows.Close()
	collaborators := make([]Collaborator, 0)
	for rows.Next() {
		var collaborator Collaborator
		if err := rows.Scan(&collaborator.UserID, &collaborator.Username, &collaborator.DisplayName,
			&collaborator.Role, &collaborator.Active, &collaborator.Source); err != nil {
			return nil, fmt.Errorf("scan repository collaborator: %w", err)
		}
		collaborators = append(collaborators, collaborator)
	}
	return collaborators, rows.Err()
}

func (store *Store) SetRepositoryCollaborator(
	ctx context.Context,
	actor User,
	owner string,
	repositorySlug string,
	input SetCollaboratorInput,
) (Collaborator, error) {
	repositoryID, organizationID, err := store.repositoryAdminAccess(ctx, actor.ID, owner, repositorySlug)
	if err != nil {
		return Collaborator{}, err
	}
	if !validRepositoryRole(input.Role) {
		return Collaborator{}, errors.New("invalid repository role")
	}
	var userID string
	err = store.pool.QueryRow(ctx, `
		SELECT u.id
		FROM users u
		JOIN organization_memberships om
		  ON om.user_id = u.id AND om.organization_id = $2 AND om.active
		WHERE u.username = $1 AND u.status = 'active'
	`, strings.ToLower(strings.TrimSpace(input.Username)), organizationID).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Collaborator{}, ErrNotFound
	}
	if err != nil {
		return Collaborator{}, fmt.Errorf("find repository collaborator: %w", err)
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Collaborator{}, fmt.Errorf("begin repository collaborator transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	_, err = transaction.Exec(ctx, `
		INSERT INTO repository_memberships (repository_id, user_id, role, active)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (repository_id, user_id) DO UPDATE
		SET role = EXCLUDED.role, active = EXCLUDED.active
	`, repositoryID, userID, input.Role, input.Active)
	if err != nil {
		return Collaborator{}, fmt.Errorf("set repository collaborator: %w", err)
	}
	if err := insertAuditDetails(ctx, transaction, actor.ID, organizationID, repositoryID,
		"repository.collaborator.set", "user", userID,
		map[string]any{"role": input.Role, "active": input.Active}); err != nil {
		return Collaborator{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return Collaborator{}, fmt.Errorf("commit repository collaborator transaction: %w", err)
	}
	return Collaborator{UserID: userID, Username: strings.ToLower(strings.TrimSpace(input.Username)),
		Role: input.Role, Active: input.Active, Source: "direct"}, nil
}
