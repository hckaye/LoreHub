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
						OR EXISTS (
							SELECT 1
							FROM organization_memberships viewer_owner
							JOIN users viewer_user ON viewer_user.id = viewer_owner.user_id
							WHERE viewer_owner.organization_id = %s.organization_id
							  AND viewer_owner.user_id = NULLIF(%s::text, '')::uuid
							  AND viewer_owner.role = 'owner'
							  AND viewer_user.status = 'active'
						)
					)
				)
			)
		)
	)`, repositoryAlias, viewerParam, repositoryAlias, repositoryAlias, viewerParam,
		repositoryAlias, repositoryAlias, viewerParam, repositoryAlias, viewerParam,
		repositoryAlias, viewerParam)
}

func notificationCurrentAccessClause(notificationAlias, viewerParam string) string {
	return fmt.Sprintf(`(
		EXISTS (
			SELECT 1
			FROM users active_viewer
			WHERE active_viewer.id = NULLIF(%[2]s::text, '')::uuid
			  AND active_viewer.status = 'active'
		)
		AND (
			(
				%[1]s.scope_repository_id IS NOT NULL
				AND (
					(
						%[1]s.scope_visibility = 'public'
						AND (
							EXISTS (
								SELECT 1
								FROM repository_memberships repository_grant
								JOIN users grant_user ON grant_user.id = repository_grant.user_id
								WHERE repository_grant.repository_id = %[1]s.scope_repository_id
								  AND repository_grant.user_id = NULLIF(%[2]s::text, '')::uuid
								  AND grant_user.status = 'active'
							)
							OR EXISTS (
								SELECT 1
								FROM team_memberships team_member
								JOIN team_repository_memberships team_grant
								  ON team_grant.team_id = team_member.team_id
								JOIN users grant_user ON grant_user.id = team_member.user_id
								WHERE team_grant.repository_id = %[1]s.scope_repository_id
								  AND team_grant.active
								  AND team_member.user_id = NULLIF(%[2]s::text, '')::uuid
								  AND grant_user.status = 'active'
							)
						)
					)
					OR (
						%[1]s.scope_visibility = 'internal'
						AND EXISTS (
							SELECT 1
							FROM organization_memberships organization_member
							JOIN users member_user ON member_user.id = organization_member.user_id
							WHERE organization_member.organization_id = %[1]s.scope_organization_id
							  AND organization_member.user_id = NULLIF(%[2]s::text, '')::uuid
							  AND member_user.status = 'active'
						)
					)
					OR (
						%[1]s.scope_visibility = 'private'
						AND (
							EXISTS (
								SELECT 1
								FROM repository_memberships repository_grant
								JOIN users grant_user ON grant_user.id = repository_grant.user_id
								WHERE repository_grant.repository_id = %[1]s.scope_repository_id
								  AND repository_grant.user_id = NULLIF(%[2]s::text, '')::uuid
								  AND grant_user.status = 'active'
							)
							OR EXISTS (
								SELECT 1
								FROM team_memberships team_member
								JOIN team_repository_memberships team_grant
								  ON team_grant.team_id = team_member.team_id
								JOIN users grant_user ON grant_user.id = team_member.user_id
								WHERE team_grant.repository_id = %[1]s.scope_repository_id
								  AND team_grant.active
								  AND team_member.user_id = NULLIF(%[2]s::text, '')::uuid
								  AND grant_user.status = 'active'
							)
							OR EXISTS (
								SELECT 1
								FROM organization_memberships organization_owner
								JOIN users owner_user ON owner_user.id = organization_owner.user_id
								WHERE organization_owner.organization_id = %[1]s.scope_organization_id
								  AND organization_owner.user_id = NULLIF(%[2]s::text, '')::uuid
								  AND organization_owner.role = 'owner'
								  AND owner_user.status = 'active'
							)
						)
					)
				)
			)
			OR (
				%[1]s.scope_repository_id IS NULL
				AND %[1]s.scope_team_id IS NOT NULL
				AND EXISTS (
					SELECT 1
					FROM team_memberships team_member
					JOIN users member_user ON member_user.id = team_member.user_id
					WHERE team_member.team_id = %[1]s.scope_team_id
					  AND team_member.user_id = NULLIF(%[2]s::text, '')::uuid
					  AND member_user.status = 'active'
				)
			)
			OR (
				%[1]s.scope_repository_id IS NULL
				AND %[1]s.scope_team_id IS NULL
				AND %[1]s.scope_organization_id IS NOT NULL
				AND EXISTS (
					SELECT 1
					FROM organization_memberships organization_member
					JOIN users member_user ON member_user.id = organization_member.user_id
					WHERE organization_member.organization_id = %[1]s.scope_organization_id
					  AND organization_member.user_id = NULLIF(%[2]s::text, '')::uuid
					  AND member_user.status = 'active'
				)
			)
		)
	)`, notificationAlias, viewerParam)
}
