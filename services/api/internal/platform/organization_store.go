package platform

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (store *Store) Organization(
	ctx context.Context,
	viewer *User,
	slug string,
) (OrganizationView, error) {
	viewerID := ""
	if viewer != nil {
		viewerID = viewer.ID
	}
	query := `
		SELECT o.id, o.slug, o.display_name, o.description, o.visibility,
		       o.website_url, o.contact_email, o.default_repository_visibility,
		       COALESCE(viewer.role, ''),
		       COUNT(DISTINCT member_org.user_id) FILTER (WHERE member_user.id IS NOT NULL),
		       COUNT(DISTINCT r.id) FILTER (WHERE ` + repositoryAccessClause("r", "$2") + `),
		       COUNT(DISTINCT t.id), o.created_at
		FROM organizations o
		LEFT JOIN organization_memberships viewer
		  ON viewer.organization_id = o.id AND viewer.user_id = NULLIF($2, '')::uuid
		 AND viewer.active
		 AND EXISTS (
		     SELECT 1 FROM users viewer_user
		     WHERE viewer_user.id = viewer.user_id AND viewer_user.status = 'active'
		 )
		LEFT JOIN organization_memberships members
		  ON members.organization_id = o.id AND members.active
		LEFT JOIN users member_user ON member_user.id = members.user_id AND member_user.status = 'active'
		LEFT JOIN organization_memberships member_org
		  ON member_org.organization_id = o.id AND member_org.user_id = members.user_id AND member_org.active
		LEFT JOIN repositories r ON r.organization_id = o.id
		  AND r.lifecycle_state = 'active' AND r.archived_at IS NULL
		LEFT JOIN teams t ON t.organization_id = o.id AND t.active
		WHERE o.slug = $1 AND o.active AND (o.visibility = 'public' OR viewer.user_id IS NOT NULL)
		GROUP BY o.id, viewer.role
	`
	row := store.pool.QueryRow(ctx, query, slug, viewerID)
	view, err := scanOrganizationView(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return OrganizationView{}, ErrNotFound
	}
	if err != nil {
		return OrganizationView{}, fmt.Errorf("get organization: %w", err)
	}
	return view, nil
}

func (store *Store) OrganizationRepositories(
	ctx context.Context,
	viewer *User,
	slug string,
) ([]Repository, error) {
	viewerID := ""
	if viewer != nil {
		viewerID = viewer.ID
	}
	rows, err := store.pool.Query(ctx, repositorySelect+`
		WHERE o.slug = $1 AND o.active AND r.lifecycle_state = 'active' AND r.archived_at IS NULL
		  AND (
		      o.visibility = 'public'
		      OR ($2 <> '' AND EXISTS (
		          SELECT 1 FROM organization_memberships om
		          JOIN users viewer_user ON viewer_user.id = om.user_id
		          WHERE om.organization_id = o.id AND om.user_id = NULLIF($2, '')::uuid
		            AND om.active AND o.active
		            AND viewer_user.status = 'active'
		      ))
		  )
		  AND `+repositoryAccessClause("r", "$2")+`
		GROUP BY r.id, o.slug
		ORDER BY r.updated_at DESC
		LIMIT 100
	`, slug, viewerID)
	if err != nil {
		return nil, fmt.Errorf("list organization repositories: %w", err)
	}
	defer rows.Close()
	return scanRepositories(rows)
}

func (store *Store) UpdateOrganization(
	ctx context.Context,
	actor User,
	slug string,
	input UpdateOrganizationInput,
) (OrganizationView, error) {
	organizationID, role, err := store.organizationRole(ctx, actor.ID, slug)
	if err != nil {
		return OrganizationView{}, err
	}
	if role != "owner" && role != "maintainer" {
		return OrganizationView{}, ErrForbidden
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return OrganizationView{}, fmt.Errorf("begin organization update: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	_, err = transaction.Exec(ctx, `
		UPDATE organizations
		SET display_name = COALESCE($2, display_name),
		    description = COALESCE($3, description),
		    visibility = COALESCE($4, visibility),
		    website_url = COALESCE($5, website_url),
		    contact_email = COALESCE($6, contact_email),
		    default_repository_visibility = COALESCE($7, default_repository_visibility),
		    updated_at = now()
		WHERE id = $1 AND active
	`, organizationID, input.DisplayName, input.Description, input.Visibility, input.WebsiteURL,
		input.ContactEmail, input.DefaultRepositoryVisibility)
	if err != nil {
		return OrganizationView{}, translateConstraintError("update organization", err)
	}
	if err := insertAudit(ctx, transaction, actor.ID, organizationID, "", "organization.update", "organization",
		organizationID); err != nil {
		return OrganizationView{}, err
	}
	if err := insertOutbox(ctx, transaction, "organization.updated",
		organizationID+":"+uuid.NewString(), input); err != nil {
		return OrganizationView{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return OrganizationView{}, fmt.Errorf("commit organization update: %w", err)
	}
	return store.Organization(ctx, &actor, slug)
}

func (store *Store) Teams(
	ctx context.Context,
	viewer *User,
	organizationSlug string,
) ([]Team, error) {
	viewerID := ""
	if viewer != nil {
		viewerID = viewer.ID
	}
	rows, err := store.pool.Query(ctx, `
		SELECT t.id, t.organization_id, o.slug, t.slug, t.display_name, t.description,
		       COALESCE(viewer.role, ''),
		       COUNT(DISTINCT member_org.user_id) FILTER (WHERE member_user.id IS NOT NULL),
		       t.created_at, t.updated_at
		FROM teams t
		JOIN organizations o ON o.id = t.organization_id AND o.active
		LEFT JOIN team_memberships viewer
		  ON viewer.team_id = t.id AND viewer.user_id = NULLIF($2, '')::uuid AND viewer.active
		 AND EXISTS (
		     SELECT 1 FROM organization_memberships active_org_member
		     WHERE active_org_member.organization_id = t.organization_id
		       AND active_org_member.user_id = viewer.user_id AND active_org_member.active
		 )
		LEFT JOIN team_memberships members ON members.team_id = t.id AND members.active
		LEFT JOIN users member_user ON member_user.id = members.user_id AND member_user.status = 'active'
		LEFT JOIN organization_memberships member_org
		  ON member_org.organization_id = o.id AND member_org.user_id = members.user_id AND member_org.active
		LEFT JOIN organization_memberships organization_viewer
		  ON organization_viewer.organization_id = o.id AND organization_viewer.user_id = NULLIF($2, '')::uuid
		 AND organization_viewer.active
		WHERE o.slug = $1 AND t.active
		  AND (o.visibility = 'public' OR organization_viewer.user_id IS NOT NULL)
		GROUP BY t.id, o.slug, viewer.role, organization_viewer.role
		ORDER BY t.display_name ASC
	`, organizationSlug, viewerID)
	if err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	defer rows.Close()
	return scanTeams(rows)
}

func (store *Store) Team(
	ctx context.Context,
	viewer *User,
	organizationSlug string,
	teamSlug string,
) (Team, error) {
	viewerID := ""
	if viewer != nil {
		viewerID = viewer.ID
	}
	row := store.pool.QueryRow(ctx, `
		SELECT t.id, t.organization_id, o.slug, t.slug, t.display_name, t.description,
		       COALESCE(viewer.role, organization_viewer.role, ''),
		       COUNT(DISTINCT member_org.user_id) FILTER (WHERE member_user.id IS NOT NULL),
		       t.created_at, t.updated_at
		FROM teams t
		JOIN organizations o ON o.id = t.organization_id AND o.active
		LEFT JOIN team_memberships viewer
		  ON viewer.team_id = t.id AND viewer.user_id = NULLIF($3, '')::uuid AND viewer.active
		 AND EXISTS (
		     SELECT 1 FROM organization_memberships active_org_member
		     WHERE active_org_member.organization_id = t.organization_id
		       AND active_org_member.user_id = viewer.user_id AND active_org_member.active
		 )
		LEFT JOIN team_memberships members ON members.team_id = t.id AND members.active
		LEFT JOIN users member_user ON member_user.id = members.user_id AND member_user.status = 'active'
		LEFT JOIN organization_memberships member_org
		  ON member_org.organization_id = o.id AND member_org.user_id = members.user_id AND member_org.active
		LEFT JOIN organization_memberships organization_viewer
		  ON organization_viewer.organization_id = o.id AND organization_viewer.user_id = NULLIF($3, '')::uuid
		 AND organization_viewer.active
		WHERE o.slug = $1 AND t.slug = $2
		  AND t.active
		  AND (o.visibility = 'public' OR organization_viewer.user_id IS NOT NULL)
		GROUP BY t.id, o.slug, viewer.role, organization_viewer.role
	`, organizationSlug, teamSlug, viewerID)
	team, err := scanTeam(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Team{}, ErrNotFound
	}
	if err != nil {
		return Team{}, fmt.Errorf("get team: %w", err)
	}
	return team, nil
}

func (store *Store) TeamMembers(
	ctx context.Context,
	viewer *User,
	organizationSlug string,
	teamSlug string,
) ([]TeamMember, error) {
	team, err := store.Team(ctx, viewer, organizationSlug, teamSlug)
	if err != nil {
		return nil, err
	}
	if team.ViewerRole == "" {
		return nil, ErrForbidden
	}
	rows, err := store.pool.Query(ctx, `
		SELECT u.id, u.username, u.display_name, tm.role, tm.created_at
		FROM team_memberships tm
		JOIN teams t ON t.id = tm.team_id AND t.active
		JOIN organizations o ON o.id = t.organization_id AND o.active
		JOIN organization_memberships om
		  ON om.organization_id = o.id AND om.user_id = tm.user_id AND om.active
		JOIN users u ON u.id = tm.user_id AND u.status = 'active'
		WHERE tm.team_id = $1 AND tm.active
		ORDER BY u.username ASC
	`, team.ID)
	if err != nil {
		return nil, fmt.Errorf("list team members: %w", err)
	}
	defer rows.Close()
	members := make([]TeamMember, 0)
	for rows.Next() {
		var member TeamMember
		if err := rows.Scan(
			&member.UserID, &member.Username, &member.DisplayName, &member.Role, &member.JoinedAt,
		); err != nil {
			return nil, fmt.Errorf("scan team member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate team members: %w", err)
	}
	return members, nil
}

func (store *Store) AddTeamMember(
	ctx context.Context,
	actor User,
	organizationSlug string,
	teamSlug string,
	username string,
	role string,
) (TeamMember, error) {
	team, err := store.Team(ctx, &actor, organizationSlug, teamSlug)
	if err != nil {
		return TeamMember{}, err
	}
	if !teamManager(team.ViewerRole) {
		return TeamMember{}, ErrForbidden
	}
	if role != "member" && role != "maintainer" {
		return TeamMember{}, errors.New("team member role is invalid")
	}
	var member TeamMember
	err = store.pool.QueryRow(ctx, `
		SELECT u.id, u.username, u.display_name, $3, now()
		FROM users u
		JOIN organization_memberships om ON om.user_id = u.id
		WHERE lower(u.username) = lower($1) AND om.organization_id = $2
		  AND om.active AND u.status = 'active'
	`, username, team.OrganizationID, role).Scan(
		&member.UserID, &member.Username, &member.DisplayName, &member.Role, &member.JoinedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return TeamMember{}, ErrNotFound
	}
	if err != nil {
		return TeamMember{}, fmt.Errorf("find team member: %w", err)
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TeamMember{}, fmt.Errorf("begin team membership: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	_, err = transaction.Exec(ctx, `
		INSERT INTO team_memberships (team_id, user_id, role) VALUES ($1, $2, $3)
	`, team.ID, member.UserID, role)
	if err != nil {
		return TeamMember{}, translateConstraintError("add team member", err)
	}
	if err := insertAudit(
		ctx, transaction, actor.ID, team.OrganizationID, "", "team_member.add", "team", team.ID,
	); err != nil {
		return TeamMember{}, err
	}
	if err := insertOutbox(ctx, transaction, "team.member_added", team.ID+":"+member.UserID, member); err != nil {
		return TeamMember{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return TeamMember{}, fmt.Errorf("commit team membership: %w", err)
	}
	return member, nil
}

func (store *Store) RemoveTeamMember(
	ctx context.Context,
	actor User,
	organizationSlug string,
	teamSlug string,
	username string,
) error {
	team, err := store.Team(ctx, &actor, organizationSlug, teamSlug)
	if err != nil {
		return err
	}
	if !teamManager(team.ViewerRole) {
		return ErrForbidden
	}
	var userID string
	err = store.pool.QueryRow(ctx, `
		SELECT u.id FROM users u WHERE lower(u.username) = lower($1) AND u.status = 'active'
	`, username).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("find team member to remove: %w", err)
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin team membership removal: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	tag, err := transaction.Exec(ctx, `
		DELETE FROM team_memberships WHERE team_id = $1 AND user_id = $2
	`, team.ID, userID)
	if err != nil {
		return fmt.Errorf("remove team member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := insertAudit(
		ctx, transaction, actor.ID, team.OrganizationID, "", "team_member.remove", "team", team.ID,
	); err != nil {
		return err
	}
	if err := insertOutbox(ctx, transaction, "team.member_removed", team.ID+":"+userID, map[string]string{
		"teamId": team.ID, "userId": userID,
	}); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit team membership removal: %w", err)
	}
	return nil
}

func (store *Store) UpdateRepositorySettings(
	ctx context.Context,
	actor User,
	owner string,
	slug string,
	input UpdateRepositorySettingsInput,
) (Repository, error) {
	repository, organizationID, role, orgRole, err := store.repositoryManager(ctx, actor.ID, owner, slug)
	if err != nil {
		return Repository{}, err
	}
	if role != "admin" && orgRole != "owner" {
		return Repository{}, ErrForbidden
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Repository{}, fmt.Errorf("begin repository settings update: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	updateTag, err := transaction.Exec(ctx, `
		UPDATE repositories
		SET display_name = COALESCE($2, display_name),
		    description = COALESCE($3, description),
		    visibility = COALESCE($4, visibility),
		    homepage_url = COALESCE($5, homepage_url),
		    updated_at = now()
		WHERE id = $1 AND lifecycle_state = 'active' AND archived_at IS NULL
		  AND EXISTS (SELECT 1 FROM organizations o WHERE o.id = repositories.organization_id AND o.active)
		  AND (
		      EXISTS (
		          SELECT 1 FROM repository_memberships rm
		          JOIN users repository_user ON repository_user.id = rm.user_id
		          WHERE rm.repository_id = repositories.id AND rm.user_id = $6
		            AND rm.role = 'admin' AND rm.active AND repository_user.status = 'active'
		      )
		      OR EXISTS (
		          SELECT 1 FROM organization_memberships om
		          JOIN users organization_user ON organization_user.id = om.user_id
		          WHERE om.organization_id = repositories.organization_id AND om.user_id = $6
		            AND om.role = 'owner' AND om.active AND organization_user.status = 'active'
		      )
		  )
	`, repository.ID, input.DisplayName, input.Description, input.Visibility, input.HomepageURL, actor.ID)
	if err != nil {
		return Repository{}, translateConstraintError("update repository settings", err)
	}
	if updateTag.RowsAffected() == 0 {
		return Repository{}, ErrForbidden
	}
	if err := insertAudit(ctx, transaction, actor.ID, organizationID, repository.ID, "repository.settings_update",
		"repository", repository.ID); err != nil {
		return Repository{}, err
	}
	if err := insertOutbox(ctx, transaction, "repository.settings_updated",
		repository.ID+":"+uuid.NewString(), input); err != nil {
		return Repository{}, err
	}
	updated, err := scanRepository(transaction.QueryRow(ctx, repositorySelect+`
		WHERE r.id = $1
		GROUP BY r.id, o.slug
	`, repository.ID))
	if err != nil {
		return Repository{}, fmt.Errorf("read updated repository settings: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return Repository{}, fmt.Errorf("commit repository settings update: %w", err)
	}
	return updated, nil
}

func (store *Store) repositoryManager(
	ctx context.Context,
	userID string,
	owner string,
	slug string,
) (Repository, string, string, string, error) {
	row := store.pool.QueryRow(ctx, repositorySelect+`
		WHERE o.slug = $1 AND r.slug = $2 AND o.active
		  AND r.lifecycle_state = 'active' AND r.archived_at IS NULL
		  AND (
		      EXISTS (
		          SELECT 1 FROM repository_memberships rm
		          JOIN users repository_user ON repository_user.id = rm.user_id
		          WHERE rm.repository_id = r.id AND rm.user_id = $3
		            AND rm.role = 'admin' AND rm.active AND repository_user.status = 'active'
		      )
		      OR EXISTS (
		          SELECT 1 FROM organization_memberships om
		          JOIN users organization_user ON organization_user.id = om.user_id
		          WHERE om.organization_id = o.id AND om.user_id = $3
		            AND om.role = 'owner' AND om.active AND organization_user.status = 'active'
		      )
		  )
		GROUP BY r.id, o.slug
	`, owner, slug, userID)
	repository, err := scanRepository(row)
	if errors.Is(err, pgx.ErrNoRows) {
		visible, visibilityErr := store.repositoryVisibleForActor(ctx, userID, owner, slug)
		if visibilityErr != nil {
			return Repository{}, "", "", "", visibilityErr
		}
		if !visible {
			return Repository{}, "", "", "", ErrNotFound
		}
		return Repository{}, "", "", "", ErrForbidden
	}
	if err != nil {
		return Repository{}, "", "", "", fmt.Errorf("find repository settings: %w", err)
	}
	var role, orgRole string
	err = store.pool.QueryRow(ctx, `
		SELECT COALESCE((SELECT rm.role FROM repository_memberships rm
		                WHERE rm.repository_id = $1 AND rm.user_id = $2 AND rm.active), ''),
		       COALESCE((SELECT om.role FROM organization_memberships om
		                 WHERE om.organization_id = $3 AND om.user_id = $2 AND om.active), '')
	`, repository.ID, userID, repository.OrganizationID).Scan(&role, &orgRole)
	if err != nil {
		return Repository{}, "", "", "", fmt.Errorf("read repository settings role: %w", err)
	}
	return repository, repository.OrganizationID, role, orgRole, nil
}

func (store *Store) organizationRole(ctx context.Context, userID string, slug string) (string, string, error) {
	var organizationID, role string
	err := store.pool.QueryRow(ctx, `
		SELECT o.id, m.role
		FROM organizations o
		JOIN organization_memberships m ON m.organization_id = o.id AND m.user_id = $2 AND m.active
		JOIN users u ON u.id = m.user_id AND u.status = 'active'
		WHERE o.slug = $1 AND o.active
	`, slug, userID).Scan(&organizationID, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrForbidden
	}
	if err != nil {
		return "", "", fmt.Errorf("check organization role: %w", err)
	}
	return organizationID, role, nil
}

func teamManager(role string) bool {
	return role == "maintainer" || role == "owner"
}

func scanOrganizationView(row rowScanner) (OrganizationView, error) {
	var view OrganizationView
	err := row.Scan(
		&view.ID, &view.Slug, &view.DisplayName, &view.Description, &view.Visibility,
		&view.WebsiteURL, &view.ContactEmail, &view.DefaultRepositoryVisibility,
		&view.Role, &view.MemberCount, &view.RepositoryCount, &view.TeamCount, &view.CreatedAt,
	)
	return view, err
}

func scanTeam(row rowScanner) (Team, error) {
	var team Team
	err := row.Scan(
		&team.ID, &team.OrganizationID, &team.OrganizationSlug, &team.Slug,
		&team.DisplayName, &team.Description, &team.ViewerRole, &team.MemberCount,
		&team.CreatedAt, &team.UpdatedAt,
	)
	return team, err
}

func scanTeams(rows pgx.Rows) ([]Team, error) {
	teams := make([]Team, 0)
	for rows.Next() {
		team, err := scanTeam(rows)
		if err != nil {
			return nil, fmt.Errorf("scan team: %w", err)
		}
		teams = append(teams, team)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate teams: %w", err)
	}
	return teams, nil
}
