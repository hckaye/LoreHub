package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const repositoryInvitationLifetime = 7 * 24 * time.Hour

type repositoryInvitationBoundary struct {
	RepositoryID          string
	OrganizationID        string
	Owner                 string
	Repository            string
	RepositoryDisplayName string
}

func (store *Store) ListRepositoryInvitations(
	ctx context.Context,
	actor User,
	owner string,
	repository string,
	page int,
	perPage int,
) (RepositoryInvitationPage, error) {
	if !validInvitationPage(page, perPage) {
		return RepositoryInvitationPage{}, ErrInvalidInput
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return RepositoryInvitationPage{}, fmt.Errorf("begin repository invitation list: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	boundary, err := repositoryInvitationAdminBoundary(ctx, transaction, actor.ID, owner, repository)
	if err != nil {
		return RepositoryInvitationPage{}, err
	}
	result, err := listRepositoryInvitations(
		ctx,
		transaction,
		"invitation.repository_id = $1",
		boundary.RepositoryID,
		page,
		perPage,
	)
	if err != nil {
		return RepositoryInvitationPage{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return RepositoryInvitationPage{}, fmt.Errorf("commit repository invitation list: %w", err)
	}
	return result, nil
}

func (store *Store) ListRepositoryInvitationsForUser(
	ctx context.Context,
	actor User,
	page int,
	perPage int,
) (RepositoryInvitationPage, error) {
	if !validInvitationPage(page, perPage) {
		return RepositoryInvitationPage{}, ErrInvalidInput
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return RepositoryInvitationPage{}, fmt.Errorf("begin account invitation list: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	var active bool
	if err := transaction.QueryRow(ctx, `
		SELECT status = 'active' FROM users WHERE id = $1
	`, actor.ID).Scan(&active); errors.Is(err, pgx.ErrNoRows) || !active {
		return RepositoryInvitationPage{}, ErrForbidden
	} else if err != nil {
		return RepositoryInvitationPage{}, fmt.Errorf("find repository invitation user: %w", err)
	}
	result, err := listRepositoryInvitations(
		ctx,
		transaction,
		"invitation.invitee_user_id = $1",
		actor.ID,
		page,
		perPage,
	)
	if err != nil {
		return RepositoryInvitationPage{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return RepositoryInvitationPage{}, fmt.Errorf("commit account invitation list: %w", err)
	}
	return result, nil
}

func listRepositoryInvitations(
	ctx context.Context,
	transaction pgx.Tx,
	filter string,
	filterValue string,
	page int,
	perPage int,
) (RepositoryInvitationPage, error) {
	var total int64
	if err := transaction.QueryRow(ctx, `
		SELECT count(*) FROM repository_invitations invitation WHERE `+filter,
		filterValue,
	).Scan(&total); err != nil {
		return RepositoryInvitationPage{}, fmt.Errorf("count repository invitations: %w", err)
	}
	offset := (page - 1) * perPage
	rows, err := transaction.Query(ctx, repositoryInvitationSelect+`
		WHERE `+filter+`
		ORDER BY
			CASE WHEN invitation.status = 'pending' AND invitation.expires_at > now() THEN 0 ELSE 1 END,
			invitation.created_at DESC,
			invitation.id DESC
		LIMIT $2 OFFSET $3
	`, filterValue, perPage, offset)
	if err != nil {
		return RepositoryInvitationPage{}, fmt.Errorf("list repository invitations: %w", err)
	}
	defer rows.Close()
	invitations := make([]RepositoryInvitation, 0)
	for rows.Next() {
		invitation, err := scanRepositoryInvitation(rows)
		if err != nil {
			return RepositoryInvitationPage{}, err
		}
		invitations = append(invitations, invitation)
	}
	if err := rows.Err(); err != nil {
		return RepositoryInvitationPage{}, fmt.Errorf("iterate repository invitations: %w", err)
	}
	return RepositoryInvitationPage{
		Invitations: invitations,
		Total:       total,
		Page:        page,
		PerPage:     perPage,
	}, nil
}

func (store *Store) CreateRepositoryInvitation(
	ctx context.Context,
	actor User,
	owner string,
	repository string,
	input CreateRepositoryInvitationInput,
) (RepositoryInvitation, error) {
	username := strings.ToLower(strings.TrimSpace(input.Username))
	if !validInvitationUsername(username) || !validRepositoryRole(input.Role) {
		return RepositoryInvitation{}, ErrInvalidInput
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return RepositoryInvitation{}, fmt.Errorf("begin repository invitation: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	boundary, err := repositoryInvitationAdminBoundary(ctx, transaction, actor.ID, owner, repository)
	if err != nil {
		return RepositoryInvitation{}, err
	}
	var inviteeID string
	if err := transaction.QueryRow(ctx, `
		SELECT id::text FROM users WHERE username = $1 AND status = 'active' FOR SHARE
	`, username).Scan(&inviteeID); errors.Is(err, pgx.ErrNoRows) {
		return RepositoryInvitation{}, ErrNotFound
	} else if err != nil {
		return RepositoryInvitation{}, fmt.Errorf("find repository invitee: %w", err)
	}
	if inviteeID == actor.ID {
		return RepositoryInvitation{}, ErrInvalidInput
	}
	if err := store.expirePendingRepositoryInvitation(
		ctx,
		transaction,
		actor.ID,
		boundary,
		inviteeID,
	); err != nil {
		return RepositoryInvitation{}, err
	}
	var alreadyCollaborator bool
	if err := transaction.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM repository_memberships
			WHERE repository_id = $1 AND user_id = $2 AND active
		)
	`, boundary.RepositoryID, inviteeID).Scan(&alreadyCollaborator); err != nil {
		return RepositoryInvitation{}, fmt.Errorf("check repository collaborator: %w", err)
	}
	if alreadyCollaborator {
		return RepositoryInvitation{}, fmt.Errorf("%w: user already has direct access", ErrConflict)
	}
	invitationID := uuid.NewString()
	_, err = transaction.Exec(ctx, `
		INSERT INTO repository_invitations (
			id, organization_id, repository_id, invitee_user_id, invited_by, role, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, now() + $7::interval)
	`, invitationID, boundary.OrganizationID, boundary.RepositoryID, inviteeID, actor.ID,
		input.Role, repositoryInvitationLifetime.String())
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return RepositoryInvitation{}, fmt.Errorf("%w: a pending invitation already exists", ErrConflict)
		}
		return RepositoryInvitation{}, fmt.Errorf("create repository invitation: %w", err)
	}
	if err := insertAuditDetails(
		ctx,
		transaction,
		actor.ID,
		boundary.OrganizationID,
		boundary.RepositoryID,
		"repository.invitation.created",
		"repository_invitation",
		invitationID,
		map[string]any{"inviteeUserId": inviteeID, "role": input.Role},
	); err != nil {
		return RepositoryInvitation{}, err
	}
	if err := insertRepositoryInvitationNotification(
		ctx,
		transaction,
		actor,
		boundary,
		invitationID,
		inviteeID,
		input.Role,
	); err != nil {
		return RepositoryInvitation{}, err
	}
	invitation, err := readRepositoryInvitation(ctx, transaction, invitationID)
	if err != nil {
		return RepositoryInvitation{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return RepositoryInvitation{}, fmt.Errorf("commit repository invitation: %w", err)
	}
	return invitation, nil
}

func insertRepositoryInvitationNotification(
	ctx context.Context,
	transaction pgx.Tx,
	actor User,
	boundary repositoryInvitationBoundary,
	invitationID string,
	inviteeID string,
	role string,
) error {
	name := boundary.Owner + "/" + boundary.Repository
	payload := map[string]any{
		"invitationId":  invitationID,
		"inviteeUserId": inviteeID,
		"titleEn":       "Invitation to " + name,
		"titleJa":       name + "への招待",
		"bodyEn":        actor.Username + " invited you with the " + role + " role.",
		"bodyJa":        actor.Username + "さんから" + role + "権限で招待されました。",
	}
	if err := insertOutbox(
		ctx,
		transaction,
		"repository.invitation.created",
		invitationID+":"+uuid.NewString(),
		payload,
	); err != nil {
		return fmt.Errorf("record repository invitation notification: %w", err)
	}
	return nil
}

func repositoryInvitationAdminBoundary(
	ctx context.Context,
	transaction pgx.Tx,
	actorID string,
	owner string,
	repository string,
) (repositoryInvitationBoundary, error) {
	var boundary repositoryInvitationBoundary
	err := transaction.QueryRow(ctx, `
		SELECT r.id::text, o.id::text, o.slug, r.slug, r.display_name
		FROM repositories r
		JOIN organizations o ON o.id = r.organization_id AND o.active
		JOIN users actor ON actor.id = $3 AND actor.status = 'active'
		WHERE o.slug = $1 AND r.slug = $2
		  AND r.lifecycle_state = 'active'
		  AND r.archived_at IS NULL AND r.migrating_at IS NULL
		  AND (
			EXISTS (
				SELECT 1 FROM organization_memberships membership
				WHERE membership.organization_id = o.id
				  AND membership.user_id = actor.id
				  AND membership.active AND membership.role = 'owner'
			)
			OR EXISTS (
				SELECT 1 FROM repository_memberships membership
				WHERE membership.repository_id = r.id
				  AND membership.user_id = actor.id
				  AND membership.active AND membership.role = 'admin'
			)
			OR EXISTS (
				SELECT 1
				FROM team_repository_roles team_role
				JOIN teams team ON team.id = team_role.team_id
				  AND team.organization_id = o.id AND team.active
				JOIN team_memberships team_member ON team_member.team_id = team.id
				  AND team_member.user_id = actor.id AND team_member.active
				JOIN organization_memberships organization_member
				  ON organization_member.organization_id = o.id
				 AND organization_member.user_id = actor.id AND organization_member.active
				WHERE team_role.repository_id = r.id
				  AND team_role.active AND team_role.role = 'admin'
			)
		  )
		FOR SHARE OF r, o, actor
	`, owner, repository, actorID).Scan(
		&boundary.RepositoryID,
		&boundary.OrganizationID,
		&boundary.Owner,
		&boundary.Repository,
		&boundary.RepositoryDisplayName,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return repositoryInvitationBoundary{}, ErrNotFound
	}
	if err != nil {
		return repositoryInvitationBoundary{}, fmt.Errorf("authorize repository invitation: %w", err)
	}
	return boundary, nil
}

func readRepositoryInvitation(
	ctx context.Context,
	transaction pgx.Tx,
	invitationID string,
) (RepositoryInvitation, error) {
	invitation, err := scanRepositoryInvitation(transaction.QueryRow(
		ctx,
		repositoryInvitationSelect+" WHERE invitation.id = $1",
		invitationID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return RepositoryInvitation{}, ErrNotFound
	}
	return invitation, err
}

const repositoryInvitationSelect = `
	SELECT invitation.id::text, invitation.organization_id::text, invitation.repository_id::text,
	       organization.slug, repository.slug, repository.display_name,
	       invitee.id::text, invitee.username, invitee.display_name,
	       inviter.id::text, inviter.username, inviter.display_name,
	       invitation.role,
	       CASE
	           WHEN invitation.status = 'pending' AND invitation.expires_at <= now() THEN 'expired'
	           ELSE invitation.status
	       END,
	       invitation.expires_at, invitation.responded_at,
	       invitation.created_at, invitation.updated_at
	FROM repository_invitations invitation
	JOIN organizations organization ON organization.id = invitation.organization_id
	JOIN repositories repository ON repository.id = invitation.repository_id
	  AND repository.organization_id = invitation.organization_id
	JOIN users invitee ON invitee.id = invitation.invitee_user_id
	JOIN users inviter ON inviter.id = invitation.invited_by
`

type invitationRow interface {
	Scan(...any) error
}

func scanRepositoryInvitation(row invitationRow) (RepositoryInvitation, error) {
	var invitation RepositoryInvitation
	err := row.Scan(
		&invitation.ID,
		&invitation.OrganizationID,
		&invitation.RepositoryID,
		&invitation.Owner,
		&invitation.Repository,
		&invitation.RepositoryDisplayName,
		&invitation.InviteeUserID,
		&invitation.InviteeUsername,
		&invitation.InviteeDisplayName,
		&invitation.InvitedByUserID,
		&invitation.InvitedByUsername,
		&invitation.InvitedByDisplayName,
		&invitation.Role,
		&invitation.Status,
		&invitation.ExpiresAt,
		&invitation.RespondedAt,
		&invitation.CreatedAt,
		&invitation.UpdatedAt,
	)
	if err != nil {
		return RepositoryInvitation{}, err
	}
	return invitation, nil
}

func validInvitationUsername(value string) bool {
	if value == "" || len([]rune(value)) > 64 {
		return false
	}
	return !strings.ContainsFunc(value, unicode.IsControl)
}

func validInvitationPage(page int, perPage int) bool {
	return page >= 1 && page <= 100_000 && perPage >= 1 && perPage <= 50
}
