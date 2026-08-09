package platform

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/authz"
)

func (store *Store) UserInfoForResource(
	ctx context.Context,
	resourceID string,
	userIDs []string,
) ([]authz.UserInfo, error) {
	repository, err := store.authorizationRepository(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT DISTINCT u.id, u.username, u.display_name
		FROM users u
		JOIN organization_memberships om
		  ON om.user_id = u.id AND om.organization_id = $1 AND om.active
		WHERE u.status = 'active'
		  AND (
			  EXISTS (
				  SELECT 1 FROM repositories internal_repository
				  WHERE internal_repository.id = $2
				    AND internal_repository.visibility = 'internal'
			  )
			  OR
			  EXISTS (SELECT 1 FROM repository_memberships rm
			          WHERE rm.repository_id = $2 AND rm.user_id = u.id AND rm.active)
			  OR EXISTS (SELECT 1 FROM team_memberships tm
			             JOIN team_repository_roles tr ON tr.team_id = tm.team_id
		             JOIN teams t ON t.id = tm.team_id AND t.organization_id = $1 AND t.active
		             WHERE tm.user_id = u.id AND tm.active AND tr.active AND tr.repository_id = $2)
			  OR om.role = 'owner'
		  )
		  AND ($3::uuid[] IS NULL OR u.id = ANY($3::uuid[]))
		ORDER BY u.display_name, u.id
		LIMIT 100
	`, repository.OrganizationID, repository.ID, nullableUUIDArray(userIDs))
	if err != nil {
		return nil, fmt.Errorf("list Lore users: %w", err)
	}
	defer rows.Close()
	result := make([]authz.UserInfo, 0)
	for rows.Next() {
		var information authz.UserInfo
		if err := rows.Scan(&information.ID, &information.Username, &information.DisplayName); err != nil {
			return nil, fmt.Errorf("scan Lore user: %w", err)
		}
		result = append(result, information)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Lore users: %w", err)
	}
	return result, nil
}

func nullableUUIDArray(values []string) any {
	if len(values) == 0 {
		return nil
	}
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		parsed, err := uuid.Parse(value)
		if err == nil {
			result = append(result, parsed)
		}
	}
	return result
}

func (store *Store) UserInfoByDisplayName(
	ctx context.Context,
	resourceID string,
	displayName string,
) (authz.UserInfo, error) {
	repository, err := store.authorizationRepository(ctx, resourceID)
	if err != nil {
		return authz.UserInfo{}, err
	}
	var information authz.UserInfo
	err = store.pool.QueryRow(ctx, `
		SELECT u.id, u.username, u.display_name
		FROM users u
		JOIN organization_memberships om
		  ON om.user_id = u.id AND om.organization_id = $1 AND om.active
		WHERE u.status = 'active' AND u.display_name = $2
		  AND (
			  EXISTS (
				  SELECT 1 FROM repositories internal_repository
				  WHERE internal_repository.id = $3
				    AND internal_repository.visibility = 'internal'
			  )
			  OR
			  EXISTS (SELECT 1 FROM repository_memberships rm
			          WHERE rm.repository_id = $3 AND rm.user_id = u.id AND rm.active)
			  OR EXISTS (SELECT 1 FROM team_memberships tm
			             JOIN team_repository_roles tr ON tr.team_id = tm.team_id
		             JOIN teams t ON t.id = tm.team_id AND t.organization_id = $1 AND t.active
		             WHERE tm.user_id = u.id AND tm.active AND tr.active AND tr.repository_id = $3)
			  OR om.role = 'owner'
		  )
		LIMIT 1
	`, repository.OrganizationID, displayName, repository.ID).Scan(
		&information.ID,
		&information.Username,
		&information.DisplayName,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return authz.UserInfo{}, ErrNotFound
	}
	if err != nil {
		return authz.UserInfo{}, fmt.Errorf("find Lore user by display name: %w", err)
	}
	return information, nil
}
