package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (store *Store) ListTeams(
	ctx context.Context,
	actor User,
	organizationSlug string,
) ([]Team, error) {
	organizationID, _, err := store.organizationAccess(ctx, actor.ID, organizationSlug)
	if err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT id, organization_id, slug, display_name, description, created_at, updated_at
		FROM teams
		WHERE organization_id = $1 AND active
		ORDER BY slug
	`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	defer rows.Close()
	teams := make([]Team, 0)
	for rows.Next() {
		var team Team
		if err := rows.Scan(&team.ID, &team.OrganizationID, &team.Slug, &team.DisplayName,
			&team.Description, &team.CreatedAt, &team.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan team: %w", err)
		}
		team.Organization = organizationSlug
		teams = append(teams, team)
	}
	return teams, rows.Err()
}

func (store *Store) CreateTeam(
	ctx context.Context,
	actor User,
	organizationSlug string,
	input SetTeamInput,
) (Team, error) {
	organizationID, role, err := store.organizationAccess(ctx, actor.ID, organizationSlug)
	if err != nil {
		return Team{}, err
	}
	if !canManageOrganization(role) {
		return Team{}, ErrForbidden
	}
	if err := validateSlug(input.Slug); err != nil {
		return Team{}, err
	}
	team := Team{ID: uuid.NewString(), OrganizationID: organizationID, Organization: organizationSlug,
		OrganizationSlug: organizationSlug, ViewerRole: "maintainer", MemberCount: 1,
		Slug: input.Slug, DisplayName: limitText(strings.TrimSpace(input.DisplayName), 160),
		Description: limitText(input.Description, 10_000), CreatedAt: time.Now().UTC()}
	team.UpdatedAt = team.CreatedAt
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Team{}, fmt.Errorf("begin team transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	_, err = transaction.Exec(ctx, `
		INSERT INTO teams (id, organization_id, slug, display_name, description, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
	`, team.ID, team.OrganizationID, team.Slug, team.DisplayName, team.Description, actor.ID, team.CreatedAt)
	if err != nil {
		return Team{}, translateConstraintError("create team", err)
	}
	if _, err := transaction.Exec(ctx, `
		INSERT INTO team_memberships (team_id, user_id, role, active)
		VALUES ($1, $2, 'maintainer', true)
	`, team.ID, actor.ID); err != nil {
		return Team{}, fmt.Errorf("add team creator: %w", err)
	}
	if err := insertAuditDetails(ctx, transaction, actor.ID, organizationID, "", "team.create",
		"team", team.ID, map[string]any{"slug": team.Slug}); err != nil {
		return Team{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return Team{}, fmt.Errorf("commit team transaction: %w", err)
	}
	return team, nil
}

func (store *Store) UpdateTeam(
	ctx context.Context,
	actor User,
	organizationSlug string,
	teamSlug string,
	input SetTeamInput,
) (Team, error) {
	organizationID, role, err := store.organizationAccess(ctx, actor.ID, organizationSlug)
	if err != nil {
		return Team{}, err
	}
	if !canManageOrganization(role) {
		return Team{}, ErrForbidden
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Team{}, fmt.Errorf("begin team update transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	var team Team
	err = transaction.QueryRow(ctx, `
		UPDATE teams
		SET display_name = $3, description = $4, updated_at = now()
		WHERE organization_id = $1 AND slug = $2 AND active
		RETURNING id, organization_id, slug, display_name, description, created_at, updated_at
	`, organizationID, teamSlug, limitText(strings.TrimSpace(input.DisplayName), 160),
		limitText(input.Description, 10_000)).Scan(&team.ID, &team.OrganizationID, &team.Slug,
		&team.DisplayName, &team.Description, &team.CreatedAt, &team.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Team{}, ErrNotFound
	}
	if err != nil {
		return Team{}, fmt.Errorf("update team: %w", err)
	}
	team.Organization = organizationSlug
	team.OrganizationSlug = organizationSlug
	if err := insertAuditDetails(ctx, transaction, actor.ID, organizationID, "", "team.update", "team", team.ID,
		map[string]any{"displayName": team.DisplayName}); err != nil {
		return Team{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return Team{}, fmt.Errorf("commit team update transaction: %w", err)
	}
	return team, nil
}

func (store *Store) DeleteTeam(
	ctx context.Context,
	actor User,
	organizationSlug string,
	teamSlug string,
) error {
	organizationID, role, err := store.organizationAccess(ctx, actor.ID, organizationSlug)
	if err != nil {
		return err
	}
	if !canManageOrganization(role) {
		return ErrForbidden
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin team delete transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	var teamID string
	err = transaction.QueryRow(ctx, `
		DELETE FROM teams
		WHERE organization_id = $1 AND slug = $2 AND active
		RETURNING id
	`, organizationID, teamSlug).Scan(&teamID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("delete team: %w", err)
	}
	if err := insertAuditDetails(ctx, transaction, actor.ID, organizationID, "", "team.delete", "team", teamID,
		map[string]any{"slug": teamSlug}); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit team delete transaction: %w", err)
	}
	return nil
}

func (store *Store) ListTeamMembers(
	ctx context.Context,
	actor User,
	organizationSlug string,
	teamSlug string,
) ([]TeamMember, error) {
	organizationID, _, err := store.organizationAccess(ctx, actor.ID, organizationSlug)
	if err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT t.id, u.id, u.username, u.display_name, tm.role, tm.active, tm.created_at
		FROM teams t
		JOIN team_memberships tm ON tm.team_id = t.id
		JOIN users u ON u.id = tm.user_id
		WHERE t.organization_id = $1 AND t.slug = $2 AND t.active
		ORDER BY u.username
	`, organizationID, teamSlug)
	if err != nil {
		return nil, fmt.Errorf("list team members: %w", err)
	}
	defer rows.Close()
	members := make([]TeamMember, 0)
	for rows.Next() {
		var member TeamMember
		if err := rows.Scan(&member.TeamID, &member.UserID, &member.Username, &member.DisplayName,
			&member.Role, &member.Active, &member.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan team member: %w", err)
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func (store *Store) SetTeamMember(
	ctx context.Context,
	actor User,
	organizationSlug string,
	teamSlug string,
	input SetTeamMemberInput,
) (TeamMember, error) {
	organizationID, organizationRole, err := store.organizationAccess(ctx, actor.ID, organizationSlug)
	if err != nil {
		return TeamMember{}, err
	}
	if !canManageOrganization(organizationRole) || !validTeamRole(input.Role) {
		return TeamMember{}, ErrForbidden
	}
	var teamID, userID string
	err = store.pool.QueryRow(ctx, `
		SELECT t.id, u.id
		FROM teams t
		JOIN organizations o ON o.id = t.organization_id
		JOIN organization_memberships om ON om.organization_id = o.id AND om.active
		JOIN users u ON u.id = om.user_id AND u.username = $3 AND u.status = 'active'
		WHERE t.organization_id = $1 AND t.slug = $2 AND t.active AND o.active
	`, organizationID, teamSlug, strings.ToLower(strings.TrimSpace(input.Username))).Scan(&teamID, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return TeamMember{}, ErrNotFound
	}
	if err != nil {
		return TeamMember{}, fmt.Errorf("find team member: %w", err)
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TeamMember{}, fmt.Errorf("begin team member transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	_, err = transaction.Exec(ctx, `
		INSERT INTO team_memberships (team_id, user_id, role, active)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (team_id, user_id) DO UPDATE
		SET role = EXCLUDED.role, active = EXCLUDED.active
	`, teamID, userID, input.Role, input.Active)
	if err != nil {
		return TeamMember{}, fmt.Errorf("set team member: %w", err)
	}
	if err := insertAuditDetails(ctx, transaction, actor.ID, organizationID, "", "team.member.set", "team", teamID,
		map[string]any{"userId": userID, "role": input.Role, "active": input.Active}); err != nil {
		return TeamMember{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return TeamMember{}, fmt.Errorf("commit team member transaction: %w", err)
	}
	return store.teamMember(ctx, teamID, userID)
}

func (store *Store) teamMember(ctx context.Context, teamID string, userID string) (TeamMember, error) {
	var member TeamMember
	err := store.pool.QueryRow(ctx, `
		SELECT tm.team_id, u.id, u.username, u.display_name, tm.role, tm.active, tm.created_at
		FROM team_memberships tm
		JOIN users u ON u.id = tm.user_id
		WHERE tm.team_id = $1 AND tm.user_id = $2
	`, teamID, userID).Scan(&member.TeamID, &member.UserID, &member.Username, &member.DisplayName,
		&member.Role, &member.Active, &member.CreatedAt)
	if err != nil {
		return TeamMember{}, fmt.Errorf("read team member: %w", err)
	}
	return member, nil
}
