package collab

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func revisionCommentPermission(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repository Repository,
	mutation bool,
) (Permission, error) {
	if actorID == "" {
		if mutation {
			return PermNone, platform.ErrForbidden
		}
		var visible bool
		err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM repositories repository
				JOIN organizations organization
				  ON organization.id = repository.organization_id AND organization.active
				WHERE repository.id = $1 AND repository.organization_id = $2
				  AND repository.lifecycle_state = 'active' AND repository.visibility = 'public'
			)
		`, repository.ID, repository.OrganizationID).Scan(&visible)
		if err != nil {
			return PermNone, fmt.Errorf("authorize anonymous revision comment read: %w", err)
		}
		if !visible {
			return PermNone, platform.ErrNotFound
		}
		return PermRead, nil
	}
	var permission int
	err := tx.QueryRow(ctx, `
		SELECT GREATEST(
			CASE
				WHEN repository.visibility = 'public' THEN 1
				WHEN repository.visibility = 'internal' AND organization_member.user_id IS NOT NULL THEN 1
				ELSE 0
			END,
			CASE
				WHEN organization_member.role = 'owner' THEN 4
				WHEN organization_member.role IN ('maintainer', 'member')
				 AND repository.visibility IN ('public', 'internal') THEN 1
				ELSE 0
			END,
			CASE direct_access.role
				WHEN 'admin' THEN 4 WHEN 'maintain' THEN 3 WHEN 'write' THEN 3
				WHEN 'triage' THEN 2 WHEN 'read' THEN 1 ELSE 0
			END,
			COALESCE((
				SELECT MAX(CASE team_role.role
					WHEN 'admin' THEN 4 WHEN 'maintain' THEN 3 WHEN 'write' THEN 3
					WHEN 'triage' THEN 2 WHEN 'read' THEN 1 ELSE 0 END)
				FROM team_repository_roles team_role
				JOIN teams team
				  ON team.id = team_role.team_id
				 AND team.organization_id = repository.organization_id AND team.active
				JOIN team_memberships team_member
				  ON team_member.team_id = team.id AND team_member.user_id = actor.id
				 AND team_member.active
				WHERE team_role.repository_id = repository.id AND team_role.active
				  AND organization_member.user_id IS NOT NULL
			), 0)
		)
		FROM repositories repository
		JOIN organizations organization
		  ON organization.id = repository.organization_id AND organization.active
		JOIN users actor ON actor.id = $3 AND actor.status = 'active'
		LEFT JOIN organization_memberships organization_member
		  ON organization_member.organization_id = repository.organization_id
		 AND organization_member.user_id = actor.id AND organization_member.active
		LEFT JOIN repository_memberships direct_access
		  ON direct_access.repository_id = repository.id
		 AND direct_access.user_id = actor.id AND direct_access.active
		WHERE repository.id = $1 AND repository.organization_id = $2
		  AND repository.lifecycle_state = 'active'
		  AND ($4 = false OR repository.archived_at IS NULL)
	`, repository.ID, repository.OrganizationID, actorID, mutation).Scan(&permission)
	if errors.Is(err, pgx.ErrNoRows) {
		return PermNone, platform.ErrNotFound
	}
	if err != nil {
		return PermNone, fmt.Errorf("authorize revision comment: %w", err)
	}
	if permission < int(PermRead) {
		return PermNone, platform.ErrNotFound
	}
	return Permission(permission), nil
}
