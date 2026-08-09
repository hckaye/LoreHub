package platform

import "fmt"

// repositoryAccessClause is shared by every repository projection and search query.
func repositoryAccessClause(repositoryAlias, viewerParam string) string {
	return fmt.Sprintf(`(
		%s.visibility = 'public'
		OR (
			NULLIF(%s::text, '') IS NOT NULL AND (
				(
					%s.visibility = 'internal'
					AND EXISTS (
						SELECT 1
						FROM organization_memberships viewer_org
						JOIN users viewer_user ON viewer_user.id = viewer_org.user_id
						WHERE viewer_org.organization_id = %s.organization_id
						  AND viewer_org.user_id = NULLIF(%s::text, '')::uuid
						  AND viewer_user.status = 'active'
					)
				)
				OR (
					%s.visibility = 'private'
					AND (
						EXISTS (
							SELECT 1
							FROM repository_memberships viewer_repo
							JOIN users viewer_user ON viewer_user.id = viewer_repo.user_id
							WHERE viewer_repo.repository_id = %s.id
							  AND viewer_repo.user_id = NULLIF(%s::text, '')::uuid
							  AND viewer_user.status = 'active'
						)
						OR EXISTS (
							SELECT 1
							FROM team_memberships viewer_team
							JOIN team_repository_memberships team_repo
							  ON team_repo.team_id = viewer_team.team_id
							JOIN users viewer_user ON viewer_user.id = viewer_team.user_id
							WHERE team_repo.repository_id = %s.id
							  AND team_repo.active
							  AND viewer_team.user_id = NULLIF(%s::text, '')::uuid
							  AND viewer_user.status = 'active'
						)
					)
				)
			)
		)
	)`, repositoryAlias, viewerParam, repositoryAlias, repositoryAlias, viewerParam,
		repositoryAlias, repositoryAlias, viewerParam, repositoryAlias, viewerParam)
}
