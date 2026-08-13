package platform

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (store *Store) RevokeRepositoryInvitation(
	ctx context.Context,
	actor User,
	owner string,
	repository string,
	invitationID string,
) error {
	if _, err := uuid.Parse(invitationID); err != nil {
		return ErrInvalidInput
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin repository invitation revocation: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	boundary, err := repositoryInvitationAdminBoundary(ctx, transaction, actor.ID, owner, repository)
	if err != nil {
		return err
	}
	var status string
	var unexpired bool
	err = transaction.QueryRow(ctx, `
		SELECT status, expires_at > now()
		FROM repository_invitations
		WHERE id = $1 AND repository_id = $2 AND organization_id = $3
		FOR UPDATE
	`, invitationID, boundary.RepositoryID, boundary.OrganizationID).Scan(&status, &unexpired)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock repository invitation: %w", err)
	}
	if status != "pending" {
		return fmt.Errorf("%w: repository invitation is not pending", ErrConflict)
	}
	if !unexpired {
		if err := store.transitionRepositoryInvitation(
			ctx,
			transaction,
			actor.ID,
			boundary,
			invitationID,
			"expired",
			true,
		); err != nil {
			return err
		}
		if err := transaction.Commit(ctx); err != nil {
			return fmt.Errorf("commit expired repository invitation: %w", err)
		}
		return fmt.Errorf("%w: repository invitation has expired", ErrConflict)
	}
	if err := store.transitionRepositoryInvitation(
		ctx,
		transaction,
		actor.ID,
		boundary,
		invitationID,
		"revoked",
		true,
	); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit repository invitation revocation: %w", err)
	}
	return nil
}

func (store *Store) RespondRepositoryInvitation(
	ctx context.Context,
	actor User,
	invitationID string,
	accept bool,
) (RepositoryInvitation, error) {
	if _, err := uuid.Parse(invitationID); err != nil {
		return RepositoryInvitation{}, ErrInvalidInput
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return RepositoryInvitation{}, fmt.Errorf("begin repository invitation response: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	var boundary repositoryInvitationBoundary
	var inviteeID, status, repositoryState string
	var organizationActive, repositoryArchived, repositoryMigrating bool
	var unexpired bool
	err = transaction.QueryRow(ctx, `
		SELECT invitation.invitee_user_id::text, invitation.status, invitation.expires_at > now(),
		       invitation.repository_id::text, invitation.organization_id::text,
		       organization.slug, repository.slug, repository.display_name,
		       organization.active, repository.lifecycle_state,
		       repository.archived_at IS NOT NULL, repository.migrating_at IS NOT NULL
		FROM repository_invitations invitation
		JOIN organizations organization ON organization.id = invitation.organization_id
		JOIN repositories repository ON repository.id = invitation.repository_id
		  AND repository.organization_id = invitation.organization_id
		JOIN users invitee ON invitee.id = invitation.invitee_user_id
		WHERE invitation.id = $1 AND invitee.id = $2 AND invitee.status = 'active'
		FOR UPDATE OF invitation
		FOR SHARE OF invitee, organization, repository
	`, invitationID, actor.ID).Scan(
		&inviteeID,
		&status,
		&unexpired,
		&boundary.RepositoryID,
		&boundary.OrganizationID,
		&boundary.Owner,
		&boundary.Repository,
		&boundary.RepositoryDisplayName,
		&organizationActive,
		&repositoryState,
		&repositoryArchived,
		&repositoryMigrating,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return RepositoryInvitation{}, ErrNotFound
	}
	if err != nil {
		return RepositoryInvitation{}, fmt.Errorf("lock repository invitation response: %w", err)
	}
	if inviteeID != actor.ID {
		return RepositoryInvitation{}, ErrNotFound
	}
	if status != "pending" {
		return RepositoryInvitation{}, fmt.Errorf("%w: repository invitation is not pending", ErrConflict)
	}
	if !unexpired {
		if err := store.transitionRepositoryInvitation(
			ctx,
			transaction,
			actor.ID,
			boundary,
			invitationID,
			"expired",
			true,
		); err != nil {
			return RepositoryInvitation{}, err
		}
		if err := transaction.Commit(ctx); err != nil {
			return RepositoryInvitation{}, fmt.Errorf("commit expired repository invitation: %w", err)
		}
		return RepositoryInvitation{}, fmt.Errorf("%w: repository invitation has expired", ErrConflict)
	}
	if accept && (!organizationActive || repositoryState != "active" || repositoryArchived || repositoryMigrating) {
		if err := store.transitionRepositoryInvitation(
			ctx,
			transaction,
			actor.ID,
			boundary,
			invitationID,
			"revoked",
			true,
		); err != nil {
			return RepositoryInvitation{}, err
		}
		if err := transaction.Commit(ctx); err != nil {
			return RepositoryInvitation{}, fmt.Errorf("commit unavailable repository invitation: %w", err)
		}
		return RepositoryInvitation{}, fmt.Errorf("%w: repository is not available", ErrConflict)
	}
	response := "declined"
	if accept {
		response = "accepted"
		if _, err := transaction.Exec(ctx, `
			INSERT INTO repository_memberships (repository_id, user_id, role, active)
			SELECT repository_id, invitee_user_id, role, true
			FROM repository_invitations WHERE id = $1
			ON CONFLICT (repository_id, user_id) DO UPDATE
			SET role = EXCLUDED.role, active = true
		`, invitationID); err != nil {
			return RepositoryInvitation{}, fmt.Errorf("activate repository collaborator: %w", err)
		}
	}
	if err := store.transitionRepositoryInvitation(
		ctx,
		transaction,
		actor.ID,
		boundary,
		invitationID,
		response,
		!accept,
	); err != nil {
		return RepositoryInvitation{}, err
	}
	invitation, err := readRepositoryInvitation(ctx, transaction, invitationID)
	if err != nil {
		return RepositoryInvitation{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return RepositoryInvitation{}, fmt.Errorf("commit repository invitation response: %w", err)
	}
	return invitation, nil
}

func (store *Store) expirePendingRepositoryInvitation(
	ctx context.Context,
	transaction pgx.Tx,
	actorID string,
	boundary repositoryInvitationBoundary,
	inviteeID string,
) error {
	rows, err := transaction.Query(ctx, `
		UPDATE repository_invitations
		SET status = 'expired', responded_at = now(), updated_at = now()
		WHERE repository_id = $1 AND invitee_user_id = $2
		  AND status = 'pending' AND expires_at <= now()
		RETURNING id::text
	`, boundary.RepositoryID, inviteeID)
	if err != nil {
		return fmt.Errorf("expire repository invitation: %w", err)
	}
	invitationIDs := make([]string, 0, 1)
	for rows.Next() {
		var invitationID string
		if err := rows.Scan(&invitationID); err != nil {
			rows.Close()
			return fmt.Errorf("scan expired repository invitation: %w", err)
		}
		invitationIDs = append(invitationIDs, invitationID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate expired repository invitations: %w", err)
	}
	rows.Close()
	for _, invitationID := range invitationIDs {
		if err := store.recordRepositoryInvitationTransition(
			ctx,
			transaction,
			actorID,
			boundary,
			invitationID,
			"expired",
		); err != nil {
			return err
		}
		if err := cancelRepositoryInvitationNotification(ctx, transaction, invitationID, true); err != nil {
			return err
		}
	}
	return nil
}

func (store *Store) transitionRepositoryInvitation(
	ctx context.Context,
	transaction pgx.Tx,
	actorID string,
	boundary repositoryInvitationBoundary,
	invitationID string,
	status string,
	hideNotification bool,
) error {
	tag, err := transaction.Exec(ctx, `
		UPDATE repository_invitations
		SET status = $1, responded_at = now(), updated_at = now()
		WHERE id = $2 AND status = 'pending'
	`, status, invitationID)
	if err != nil {
		return fmt.Errorf("update repository invitation status: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("%w: repository invitation is not pending", ErrConflict)
	}
	if err := store.recordRepositoryInvitationTransition(
		ctx,
		transaction,
		actorID,
		boundary,
		invitationID,
		status,
	); err != nil {
		return err
	}
	return cancelRepositoryInvitationNotification(ctx, transaction, invitationID, hideNotification)
}

func (store *Store) recordRepositoryInvitationTransition(
	ctx context.Context,
	transaction pgx.Tx,
	actorID string,
	boundary repositoryInvitationBoundary,
	invitationID string,
	status string,
) error {
	return insertAuditDetails(
		ctx,
		transaction,
		actorID,
		boundary.OrganizationID,
		boundary.RepositoryID,
		"repository.invitation."+status,
		"repository_invitation",
		invitationID,
		map[string]any{"status": status},
	)
}

func cancelRepositoryInvitationNotification(
	ctx context.Context,
	transaction pgx.Tx,
	invitationID string,
	hideInApp bool,
) error {
	_, err := transaction.Exec(ctx, `
		WITH affected AS (
			UPDATE notifications notification
			SET in_app_enabled = CASE WHEN $2 THEN false ELSE notification.in_app_enabled END,
			    email_enabled = false,
			    read_at = CASE WHEN $2 THEN notification.read_at ELSE COALESCE(notification.read_at, now()) END
			FROM outbox_events event
			WHERE notification.source_event_id = event.id
			  AND event.topic = 'repository.invitation.created'
			  AND event.payload ->> 'invitationId' = $1
			RETURNING notification.id
		)
		UPDATE notification_email_deliveries delivery
		SET status = 'cancelled', lease_owner = NULL, lease_expires_at = NULL,
		    last_error = '', updated_at = now()
		WHERE delivery.notification_id IN (SELECT id FROM affected)
		  AND delivery.status IN ('queued', 'failed')
	`, invitationID, hideInApp)
	if err != nil {
		return fmt.Errorf("cancel repository invitation notification: %w", err)
	}
	return nil
}
