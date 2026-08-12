package platform

import "fmt"

// repositoryAccessClause is shared by every repository projection and search query.
func repositoryAccessClause(repositoryAlias, viewerParam string) string {
	return fmt.Sprintf(`(
		%s.lifecycle_state = 'active'
		AND EXISTS (
			SELECT 1
			FROM organizations repository_org
			WHERE repository_org.id = %s.organization_id
			  AND repository_org.active
		)
		AND (
			NULLIF(%s::text, '') IS NULL
			OR EXISTS (
				SELECT 1
				FROM users active_viewer
				WHERE active_viewer.id = NULLIF(%s::text, '')::uuid
				  AND active_viewer.status = 'active'
			)
		)
		AND (
			%s.visibility = 'public'
			OR (
				NULLIF(%s::text, '') IS NOT NULL
				AND EXISTS (
					SELECT 1
					FROM users active_viewer
					WHERE active_viewer.id = NULLIF(%s::text, '')::uuid
					  AND active_viewer.status = 'active'
				)
				AND (
					(
						%s.visibility = 'internal'
						AND EXISTS (
							SELECT 1
							FROM organization_memberships viewer_org
							JOIN organizations viewer_organization
							  ON viewer_organization.id = viewer_org.organization_id
							 AND viewer_organization.active
							WHERE viewer_org.organization_id = %s.organization_id
							  AND viewer_org.user_id = NULLIF(%s::text, '')::uuid
							  AND viewer_org.active
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
								  AND viewer_repo.active
								  AND viewer_user.status = 'active'
							)
							OR EXISTS (
								SELECT 1
								FROM team_repository_roles team_repo
								JOIN teams viewer_team
								  ON viewer_team.id = team_repo.team_id
								 AND viewer_team.organization_id = %s.organization_id
								 AND viewer_team.active
								JOIN team_memberships viewer_membership
								  ON viewer_membership.team_id = viewer_team.id
								 AND viewer_membership.user_id = NULLIF(%s::text, '')::uuid
								 AND viewer_membership.active
								JOIN organization_memberships viewer_org
								  ON viewer_org.organization_id = viewer_team.organization_id
								 AND viewer_org.user_id = viewer_membership.user_id
								 AND viewer_org.active
								JOIN users viewer_user ON viewer_user.id = viewer_membership.user_id
								WHERE team_repo.repository_id = %s.id
							  AND team_repo.active
							  AND viewer_user.status = 'active'
							)
							OR EXISTS (
								SELECT 1
								FROM organization_memberships viewer_owner
								JOIN organizations owner_organization
								  ON owner_organization.id = viewer_owner.organization_id
								 AND owner_organization.active
								JOIN users viewer_user ON viewer_user.id = viewer_owner.user_id
								WHERE viewer_owner.organization_id = %s.organization_id
							  AND viewer_owner.user_id = NULLIF(%s::text, '')::uuid
							  AND viewer_owner.role = 'owner'
							  AND viewer_owner.active
							  AND viewer_user.status = 'active'
							)
						)
					)
				)
			)
		)
	)`, repositoryAlias, repositoryAlias, viewerParam, viewerParam, repositoryAlias,
		viewerParam, viewerParam, repositoryAlias, repositoryAlias, viewerParam, repositoryAlias,
		repositoryAlias, viewerParam, repositoryAlias, viewerParam, repositoryAlias, repositoryAlias,
		viewerParam)
}

// notificationCurrentAccessClause applies current authorization to materialized notification rows.
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
				(%[1]s.scope_kind = 'user' OR %[1]s.scope_kind IS NULL)
				AND %[1]s.scope_organization_id IS NULL
				AND %[1]s.scope_repository_id IS NULL
				AND %[1]s.scope_team_id IS NULL
			)
			OR (
				%[1]s.scope_kind = 'repository'
				AND %[1]s.scope_organization_id IS NOT NULL
				AND %[1]s.scope_team_id IS NULL
				AND EXISTS (
					SELECT 1
					FROM repositories current_repository
					JOIN organizations current_organization
					  ON current_organization.id = current_repository.organization_id
					 AND current_organization.active
					WHERE current_repository.id = %[1]s.scope_repository_id
					  AND %[1]s.scope_organization_id = current_repository.organization_id
					  AND current_repository.lifecycle_state = 'active'
						AND (
							current_repository.visibility = 'public'
						OR (
							current_repository.visibility = 'internal'
							AND EXISTS (
								SELECT 1
								FROM organization_memberships organization_member
								WHERE organization_member.organization_id = current_repository.organization_id
								  AND organization_member.user_id = NULLIF(%[2]s::text, '')::uuid
								  AND organization_member.active
							)
						)
						OR (
							current_repository.visibility = 'private'
							AND (
								EXISTS (
									SELECT 1
									FROM repository_memberships repository_grant
									WHERE repository_grant.repository_id = current_repository.id
									  AND repository_grant.user_id = NULLIF(%[2]s::text, '')::uuid
									  AND repository_grant.active
								)
								OR EXISTS (
									SELECT 1
									FROM team_repository_roles team_grant
									JOIN teams current_team ON current_team.id = team_grant.team_id
									 AND current_team.organization_id = current_repository.organization_id
									 AND current_team.active
								JOIN team_memberships team_member ON team_member.team_id = current_team.id
								 AND team_member.user_id = NULLIF(%[2]s::text, '')::uuid
								 AND team_member.active
								JOIN organization_memberships team_org_member
								 ON team_org_member.organization_id = current_team.organization_id
								AND team_org_member.user_id = team_member.user_id
								AND team_org_member.active
								WHERE team_grant.repository_id = current_repository.id
									  AND team_grant.active
								)
								OR EXISTS (
									SELECT 1
									FROM organization_memberships owner_membership
									WHERE owner_membership.organization_id = current_repository.organization_id
									  AND owner_membership.user_id = NULLIF(%[2]s::text, '')::uuid
									  AND owner_membership.role = 'owner'
									  AND owner_membership.active
								)
							)
						)
					)
				)
			)
			OR (
				%[1]s.scope_kind = 'team'
				AND %[1]s.scope_organization_id IS NOT NULL
				AND %[1]s.scope_repository_id IS NULL
				AND EXISTS (
					SELECT 1
					FROM teams current_team
					JOIN organizations current_organization
					  ON current_organization.id = current_team.organization_id
					 AND current_organization.active
					JOIN team_memberships team_member
					  ON team_member.team_id = current_team.id
					 AND team_member.user_id = NULLIF(%[2]s::text, '')::uuid
					 AND team_member.active
					JOIN organization_memberships team_org_member
					  ON team_org_member.organization_id = current_team.organization_id
					 AND team_org_member.user_id = team_member.user_id
					 AND team_org_member.active
					WHERE current_team.id = %[1]s.scope_team_id
					  AND %[1]s.scope_organization_id = current_team.organization_id
				  AND current_team.active
				)
			)
			OR (
				%[1]s.scope_kind = 'organization'
				AND %[1]s.scope_repository_id IS NULL
				AND %[1]s.scope_team_id IS NULL
				AND EXISTS (
					SELECT 1
					FROM organizations current_organization
					JOIN organization_memberships organization_member
					  ON organization_member.organization_id = current_organization.id
					 AND organization_member.user_id = NULLIF(%[2]s::text, '')::uuid
					 AND organization_member.active
					WHERE current_organization.id = %[1]s.scope_organization_id
					  AND current_organization.active
				)
			)
		)
	)`, notificationAlias, viewerParam)
}
