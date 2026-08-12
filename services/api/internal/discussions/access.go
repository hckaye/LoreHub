package discussions

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type mutationPermission int

const (
	permissionParticipate mutationPermission = iota
	permissionModerate
	permissionAdmin
)

func discussionPermissionAllowed(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	repository RepositoryRef,
	permission mutationPermission,
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
			 AND organization_member.user_id = actor.id
			 AND organization_member.active
			WHERE repository.id = $1 AND repository.organization_id = $2
			  AND repository.lifecycle_state = 'active' AND repository.archived_at IS NULL
			  AND CASE $4::int
			    WHEN 0 THEN (
			      repository.visibility = 'public'
			      OR (
			        repository.visibility = 'internal' AND organization_member.user_id IS NOT NULL
			      )
			      OR organization_member.role = 'owner'
			      OR EXISTS (
			        SELECT 1 FROM repository_memberships direct_access
			        WHERE direct_access.repository_id = repository.id
			          AND direct_access.user_id = actor.id AND direct_access.active
			      )
			      OR EXISTS (
			        SELECT 1
			        FROM team_repository_roles role
						JOIN teams team
						  ON team.id = role.team_id
						 AND team.organization_id = repository.organization_id
						 AND team.active
			        JOIN team_memberships team_member
			          ON team_member.team_id = team.id
			         AND team_member.user_id = actor.id
			         AND team_member.active
			        WHERE role.repository_id = repository.id AND role.active
			          AND organization_member.user_id IS NOT NULL
			      )
			    )
			    WHEN 1 THEN (
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
						 AND team.organization_id = repository.organization_id
						 AND team.active
			        JOIN team_memberships team_member
			          ON team_member.team_id = team.id
			         AND team_member.user_id = actor.id
			         AND team_member.active
			        WHERE role.repository_id = repository.id AND role.active
			          AND role.role IN ('write', 'maintain', 'admin')
			          AND organization_member.user_id IS NOT NULL
			      )
			    )
			    ELSE (
			      organization_member.role = 'owner'
			      OR EXISTS (
			        SELECT 1 FROM repository_memberships direct_access
			        WHERE direct_access.repository_id = repository.id
			          AND direct_access.user_id = actor.id AND direct_access.active
			          AND direct_access.role = 'admin'
			      )
			      OR EXISTS (
			        SELECT 1
			        FROM team_repository_roles role
			        JOIN teams team ON team.id = role.team_id AND team.active
			        JOIN team_memberships team_member
			          ON team_member.team_id = team.id
			         AND team_member.user_id = actor.id
			         AND team_member.active
			        WHERE role.repository_id = repository.id AND role.active
			          AND role.role = 'admin' AND organization_member.user_id IS NOT NULL
			      )
			    )
			  END
		)
	`, repository.ID, repository.OrganizationID, actorID, int(permission)).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("authorize discussion mutation: %w", err)
	}
	return allowed, nil
}
