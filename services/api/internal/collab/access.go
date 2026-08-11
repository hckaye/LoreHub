package collab

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

// Permission is the effective access level an actor has on a repository.
type Permission int

const (
	PermNone Permission = iota
	PermRead
	PermTriage
	PermWrite
	PermAdmin
)

// Access describes the actor's relationship to a repository. Permission is the
// effective level (max of repository role and visibility-derived read).
// OrgOwner and OrgMaintainer are reported separately for organization settings.
type Access struct {
	Permission       Permission
	RepositoryRole   string
	OrganizationRole string
	OrgOwner         bool
	OrgMaintainer    bool
}

// AtLeast reports whether the effective permission meets the required level.
func (a Access) AtLeast(level Permission) bool {
	return a.Permission >= level
}

// CanManageBranchRules reports whether the actor may create or modify branch
// protection rules: an exact repository admin or organization owner.
func (a Access) CanManageBranchRules() bool {
	return a.Permission >= PermAdmin || a.OrgOwner
}

// lookupRepository returns the repository if it is visible to the actor.
// Anonymous actors (nil user) can only see public repositories. Private and
// internal repositories that the actor cannot read are reported as ErrNotFound
// so that existence does not leak to unauthorized users.
func lookupRepository(
	ctx context.Context,
	pool *pgxpool.Pool,
	actor *platform.User,
	owner string,
	slug string,
) (Repository, error) {
	if actor == nil {
		return lookupPublicRepository(ctx, pool, owner, slug)
	}
	row := pool.QueryRow(ctx, `
		SELECT r.id, r.organization_id, o.slug, r.slug, r.display_name, r.description,
		       r.visibility, r.lore_repository_id, r.lore_url, r.default_branch,
		       (SELECT COUNT(*) FROM issues i WHERE i.repository_id = r.id),
		       (SELECT COUNT(*) FROM merge_requests mr WHERE mr.repository_id = r.id),
		       (
		           SELECT COUNT(*) FROM repository_stars star
		           JOIN users stargazer ON stargazer.id = star.user_id AND stargazer.status = 'active'
		           WHERE star.repository_id = r.id
		       ),
		       (
		           SELECT COUNT(*) FROM repository_watches watch
		           JOIN users watcher ON watcher.id = watch.user_id AND watcher.status = 'active'
		           WHERE watch.repository_id = r.id
		       ),
		       EXISTS (
		           SELECT 1 FROM repository_stars star
		           WHERE star.repository_id = r.id AND star.user_id = $3
		       ),
		       EXISTS (
		           SELECT 1 FROM repository_watches watch
		           WHERE watch.repository_id = r.id AND watch.user_id = $3
		       ),
		       r.updated_at
		FROM repositories r
		JOIN organizations o ON o.id = r.organization_id AND o.active
		JOIN users actor_user ON actor_user.id = $3 AND actor_user.status = 'active'
		WHERE o.slug = $1 AND r.slug = $2 AND r.archived_at IS NULL AND r.lifecycle_state = 'active'
		  AND (
		      r.visibility = 'public'
		      OR (
		          r.visibility = 'internal'
		          AND EXISTS (
		              SELECT 1 FROM organization_memberships iom
		              WHERE iom.organization_id = o.id AND iom.user_id = $3 AND iom.active
		          )
		      )
		      OR EXISTS (
		          SELECT 1 FROM organization_memberships oom
		          WHERE oom.organization_id = o.id AND oom.user_id = $3
		            AND oom.active AND oom.role = 'owner'
		      )
		      OR EXISTS (
		          SELECT 1 FROM repository_memberships rm
		          WHERE rm.repository_id = r.id AND rm.user_id = $3 AND rm.active
		      )
		      OR EXISTS (
		          SELECT 1 FROM team_repository_roles tr
		          JOIN teams t ON t.id = tr.team_id AND t.organization_id = o.id AND t.active
		          JOIN team_memberships tm ON tm.team_id = t.id AND tm.user_id = $3 AND tm.active
		          JOIN organization_memberships tom
		            ON tom.organization_id = o.id AND tom.user_id = $3 AND tom.active
		          WHERE tr.repository_id = r.id AND tr.active
		      )
		  )
	`, owner, slug, actor.ID)
	repo, err := scanRepositoryRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Repository{}, platform.ErrNotFound
	}
	if err != nil {
		return Repository{}, fmt.Errorf("lookup repository: %w", err)
	}
	return repo, nil
}

func lookupPublicRepository(
	ctx context.Context,
	pool *pgxpool.Pool,
	owner string,
	slug string,
) (Repository, error) {
	row := pool.QueryRow(ctx, `
		SELECT r.id, r.organization_id, o.slug, r.slug, r.display_name, r.description,
		       r.visibility, r.lore_repository_id, r.lore_url, r.default_branch,
		       (SELECT COUNT(*) FROM issues i WHERE i.repository_id = r.id),
		       (SELECT COUNT(*) FROM merge_requests mr WHERE mr.repository_id = r.id),
		       (
		           SELECT COUNT(*) FROM repository_stars star
		           JOIN users stargazer ON stargazer.id = star.user_id AND stargazer.status = 'active'
		           WHERE star.repository_id = r.id
		       ),
		       (
		           SELECT COUNT(*) FROM repository_watches watch
		           JOIN users watcher ON watcher.id = watch.user_id AND watcher.status = 'active'
		           WHERE watch.repository_id = r.id
		       ),
		       false, false, r.updated_at
		FROM repositories r
		JOIN organizations o ON o.id = r.organization_id AND o.active
		WHERE o.slug = $1 AND r.slug = $2 AND r.archived_at IS NULL AND r.lifecycle_state = 'active'
		  AND r.visibility = 'public'
	`, owner, slug)
	repo, err := scanRepositoryRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Repository{}, platform.ErrNotFound
	}
	if err != nil {
		return Repository{}, fmt.Errorf("lookup public repository: %w", err)
	}
	return repo, nil
}

// repositoryPermission computes the actor's effective access on a repository.
// Anonymous actors receive PermNone; visibility-based read for public repos is
// handled by lookupRepository, not by this function.
func repositoryPermission(
	ctx context.Context,
	pool *pgxpool.Pool,
	actor platform.User,
	repo Repository,
) (Access, error) {
	var repoRole, orgRole *string
	err := pool.QueryRow(ctx, `
		SELECT rm.role, om.role
		FROM repositories r
		JOIN organizations o ON o.id = r.organization_id AND o.active
		JOIN users actor_user ON actor_user.id = $3 AND actor_user.status = 'active'
		LEFT JOIN repository_memberships rm
		    ON rm.repository_id = r.id AND rm.user_id = $3 AND rm.active
		LEFT JOIN organization_memberships om
		    ON om.organization_id = o.id AND om.user_id = $3 AND om.active
		WHERE r.id = $1 AND o.id = $2 AND r.archived_at IS NULL AND r.lifecycle_state = 'active'
	`, repo.ID, repo.OrganizationID, actor.ID).Scan(&repoRole, &orgRole)
	if err != nil {
		return Access{}, fmt.Errorf("compute repository permission: %w", err)
	}
	access := Access{}
	if repoRole != nil {
		access.RepositoryRole = *repoRole
	}
	if orgRole != nil {
		access.OrganizationRole = *orgRole
		switch *orgRole {
		case "owner":
			access.OrgOwner = true
		case "maintainer":
			access.OrgMaintainer = true
		}
	}
	var teamRole *string
	if err := pool.QueryRow(ctx, `
		SELECT tr.role
		FROM team_repository_roles tr
		JOIN teams t ON t.id = tr.team_id AND t.organization_id = $2 AND t.active
		JOIN team_memberships tm ON tm.team_id = t.id AND tm.user_id = $3 AND tm.active
		JOIN organization_memberships om
		  ON om.organization_id = $2 AND om.user_id = $3 AND om.active
		WHERE tr.repository_id = $1 AND tr.active
		ORDER BY CASE tr.role
			WHEN 'admin' THEN 5
			WHEN 'maintain' THEN 4
			WHEN 'write' THEN 3
			WHEN 'triage' THEN 2
			ELSE 1 END DESC
		LIMIT 1
	`, repo.ID, repo.OrganizationID, actor.ID).Scan(&teamRole); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Access{}, fmt.Errorf("find team repository permission: %w", err)
	}
	if teamRole != nil && (repoRole == nil || rolePermission(teamRole) > rolePermission(repoRole)) {
		access.RepositoryRole = *teamRole
	}
	access.Permission = combineRoles(repoRole, orgRole, repo.Visibility)
	if repo.Visibility == "public" && access.Permission < PermRead {
		access.Permission = PermRead
	}
	if team := rolePermission(teamRole); team > access.Permission {
		access.Permission = team
	}
	return access, nil
}

func combineRoles(repoRole *string, orgRole *string, visibility ...string) Permission {
	repo := rolePermission(repoRole)
	org := orgRolePermission(orgRole, visibility...)
	if repo > org {
		return repo
	}
	return org
}

func rolePermission(role *string) Permission {
	if role == nil {
		return PermNone
	}
	switch *role {
	case "admin":
		return PermAdmin
	case "write":
		return PermWrite
	case "maintain":
		return PermWrite
	case "triage":
		return PermTriage
	case "read":
		return PermRead
	default:
		return PermNone
	}
}

func orgRolePermission(role *string, visibility ...string) Permission {
	if role == nil {
		return PermNone
	}
	switch *role {
	case "owner":
		return PermAdmin
	case "maintainer", "member":
		repositoryVisibility := "private"
		if len(visibility) > 0 {
			repositoryVisibility = visibility[0]
		}
		if repositoryVisibility == "internal" || repositoryVisibility == "public" {
			return PermRead
		}
		return PermNone
	default:
		return PermNone
	}
}

func scanRepositoryRow(row pgx.Row) (Repository, error) {
	var repo Repository
	err := row.Scan(
		&repo.ID,
		&repo.OrganizationID,
		&repo.Owner,
		&repo.Slug,
		&repo.DisplayName,
		&repo.Description,
		&repo.Visibility,
		&repo.LoreRepositoryID,
		&repo.LoreURL,
		&repo.DefaultBranch,
		&repo.IssueCount,
		&repo.MergeRequestCount,
		&repo.StarCount,
		&repo.WatcherCount,
		&repo.ViewerHasStarred,
		&repo.ViewerIsWatching,
		&repo.UpdatedAt,
	)
	return repo, err
}
