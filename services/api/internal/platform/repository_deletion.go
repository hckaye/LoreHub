package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/authz"
)

const (
	minimumRepositoryDeletionRetention = time.Hour
	maximumRepositoryDeletionRetention = 365 * 24 * time.Hour
	maximumRepositoryDeletionLease     = 15 * time.Minute
)

type DeletedRepository struct {
	ID               string     `json:"id"`
	OrganizationID   string     `json:"organizationId"`
	Owner            string     `json:"owner"`
	Slug             string     `json:"slug"`
	DisplayName      string     `json:"displayName"`
	LoreRepositoryID string     `json:"-"`
	LoreURL          string     `json:"-"`
	RequestedBy      string     `json:"requestedBy"`
	RequestedAt      time.Time  `json:"requestedAt"`
	PurgeAfter       time.Time  `json:"purgeAfter"`
	Purging          bool       `json:"purging"`
	Attempts         int        `json:"-"`
	LastError        string     `json:"-"`
	NextAttemptAt    time.Time  `json:"-"`
	LeaseExpiresAt   *time.Time `json:"-"`
}

type RepositoryDeletionClaim struct {
	RepositoryID     string
	OrganizationID   string
	Owner            string
	Slug             string
	LoreRepositoryID string
	LoreURL          string
	Attempt          int
}

func (store *Store) ScheduleRepositoryDeletion(
	ctx context.Context,
	actor User,
	owner string,
	slug string,
	confirmation string,
	retention time.Duration,
) (DeletedRepository, error) {
	if strings.TrimSpace(confirmation) != owner+"/"+slug ||
		retention < minimumRepositoryDeletionRetention || retention > maximumRepositoryDeletionRetention {
		return DeletedRepository{}, ErrInvalidInput
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return DeletedRepository{}, fmt.Errorf("begin repository deletion: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var deleted DeletedRepository
	err = tx.QueryRow(ctx, repositoryDeletionOwnerQuery, owner, slug, actor.ID).Scan(
		&deleted.ID,
		&deleted.OrganizationID,
		&deleted.Owner,
		&deleted.Slug,
		&deleted.DisplayName,
		&deleted.LoreRepositoryID,
		&deleted.LoreURL,
		&deleted.RequestedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeletedRepository{}, store.repositoryDeletionAccessError(ctx, tx, actor.ID, owner, slug)
	}
	if err != nil {
		return DeletedRepository{}, fmt.Errorf("authorize repository deletion: %w", err)
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO repository_deletions (
			repository_id, requested_by, purge_after, next_attempt_at
		) VALUES ($1, $2, now() + $3::interval, now() + $3::interval)
		RETURNING requested_at, purge_after, attempts, COALESCE(last_error, ''), next_attempt_at
	`, deleted.ID, actor.ID, retention.String()).Scan(
		&deleted.RequestedAt,
		&deleted.PurgeAfter,
		&deleted.Attempts,
		&deleted.LastError,
		&deleted.NextAttemptAt,
	)
	if err != nil {
		return DeletedRepository{}, fmt.Errorf("schedule repository deletion: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE repositories SET lifecycle_state = 'deleting', updated_at = now() WHERE id = $1
	`, deleted.ID); err != nil {
		return DeletedRepository{}, fmt.Errorf("hide repository pending deletion: %w", err)
	}
	if err := addRepositoryLifecycleGrant(ctx, tx, actor.ID, deleted.OrganizationID, deleted.ID); err != nil {
		return DeletedRepository{}, err
	}
	if err := cancelRepositoryRuns(ctx, tx, deleted.ID); err != nil {
		return DeletedRepository{}, err
	}
	details := map[string]any{
		"owner": deleted.Owner, "repository": deleted.Slug, "purgeAfter": deleted.PurgeAfter,
	}
	if err := insertAuditDetails(ctx, tx, actor.ID, deleted.OrganizationID, deleted.ID,
		"repository.deletion.schedule", "repository", deleted.ID, details); err != nil {
		return DeletedRepository{}, err
	}
	if err := insertOutbox(ctx, tx, "repository.deletion_scheduled", deleted.ID+":"+uuid.NewString(),
		map[string]any{
			"repositoryId": deleted.ID, "owner": deleted.Owner, "repository": deleted.Slug,
			"purgeAfter": deleted.PurgeAfter,
		}); err != nil {
		return DeletedRepository{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return DeletedRepository{}, fmt.Errorf("commit repository deletion: %w", err)
	}
	return deleted, nil
}

const repositoryDeletionOwnerQuery = `
	SELECT repository.id, repository.organization_id, organization.slug, repository.slug,
	       repository.display_name, repository.lore_repository_id, repository.lore_url,
	       actor_user.username
	FROM repositories repository
	JOIN organizations organization
	  ON organization.id = repository.organization_id AND organization.active
	JOIN users actor_user ON actor_user.id = $3 AND actor_user.status = 'active'
	JOIN organization_memberships membership
	  ON membership.organization_id = organization.id
	 AND membership.user_id = actor_user.id AND membership.role = 'owner' AND membership.active
	WHERE organization.slug = $1 AND repository.slug = $2
	  AND repository.lifecycle_state = 'active'
	FOR UPDATE OF repository
`

func (store *Store) ListDeletedRepositories(
	ctx context.Context,
	actor User,
	owner string,
) ([]DeletedRepository, error) {
	organizationID, err := store.repositoryDeletionOrganization(ctx, actor.ID, owner)
	if err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, repositoryDeletionSelect+`
		WHERE repository.organization_id = $1
		  AND repository.lifecycle_state IN ('deleting', 'purging')
		ORDER BY deletion.purge_after, repository.slug
	`, organizationID)
	if err != nil {
		return nil, fmt.Errorf("list deleted repositories: %w", err)
	}
	defer rows.Close()
	deleted := make([]DeletedRepository, 0)
	for rows.Next() {
		item, scanErr := scanDeletedRepository(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan deleted repository: %w", scanErr)
		}
		deleted = append(deleted, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate deleted repositories: %w", err)
	}
	return deleted, nil
}

func (store *Store) RestoreRepository(
	ctx context.Context,
	actor User,
	owner string,
	slug string,
) (Repository, error) {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Repository{}, fmt.Errorf("begin repository restoration: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var repositoryID, organizationID string
	err = tx.QueryRow(ctx, `
		SELECT repository.id, repository.organization_id
		FROM repositories repository
		JOIN organizations organization
		  ON organization.id = repository.organization_id AND organization.active
		JOIN users actor_user ON actor_user.id = $3 AND actor_user.status = 'active'
		JOIN organization_memberships membership
		  ON membership.organization_id = organization.id
		 AND membership.user_id = actor_user.id AND membership.role = 'owner' AND membership.active
		JOIN repository_deletions deletion ON deletion.repository_id = repository.id
		WHERE organization.slug = $1 AND repository.slug = $2
		  AND repository.lifecycle_state = 'deleting' AND deletion.lease_owner IS NULL
		FOR UPDATE OF repository, deletion
	`, owner, slug, actor.ID).Scan(&repositoryID, &organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Repository{}, store.repositoryRestoreAccessError(ctx, tx, actor.ID, owner, slug)
	}
	if err != nil {
		return Repository{}, fmt.Errorf("authorize repository restoration: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE repositories SET lifecycle_state = 'active', updated_at = now() WHERE id = $1
	`, repositoryID); err != nil {
		return Repository{}, fmt.Errorf("restore repository: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM repository_deletions WHERE repository_id = $1`, repositoryID); err != nil {
		return Repository{}, fmt.Errorf("remove repository deletion schedule: %w", err)
	}
	grantResult, err := tx.Exec(ctx, `
		UPDATE service_principal_repository_grants
		SET active = false, updated_at = now()
		WHERE principal_id = $2 AND repository_id = $1
	`, repositoryID, repositoryLifecyclePrincipalID)
	if err != nil {
		return Repository{}, fmt.Errorf("revoke repository lifecycle grant: %w", err)
	}
	if grantResult.RowsAffected() != 1 {
		return Repository{}, ErrConflict
	}
	if err := insertAuditDetails(ctx, tx, actor.ID, organizationID, repositoryID,
		"service_principal.grant.repository_lifecycle.revoke", "service_principal",
		repositoryLifecyclePrincipalID, map[string]any{
			"permissions": []string{authz.PermissionObliterate}, "active": false,
		}); err != nil {
		return Repository{}, err
	}
	if err := insertAudit(ctx, tx, actor.ID, organizationID, repositoryID,
		"repository.deletion.restore", "repository", repositoryID); err != nil {
		return Repository{}, err
	}
	if err := insertOutbox(ctx, tx, "repository.restored", repositoryID+":"+uuid.NewString(),
		map[string]any{"repositoryId": repositoryID, "owner": owner, "repository": slug}); err != nil {
		return Repository{}, err
	}
	repository, err := scanRepository(tx.QueryRow(ctx, repositorySelect+`
		WHERE r.id = $1
		GROUP BY r.id, o.slug
	`, repositoryID))
	if err != nil {
		return Repository{}, fmt.Errorf("read restored repository: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Repository{}, fmt.Errorf("commit repository restoration: %w", err)
	}
	return repository, nil
}

func (store *Store) ClaimRepositoryDeletion(
	ctx context.Context,
	workerID string,
	lease time.Duration,
) (*RepositoryDeletionClaim, error) {
	if strings.TrimSpace(workerID) != workerID || workerID == "" || len(workerID) > 128 ||
		lease <= 0 || lease > maximumRepositoryDeletionLease {
		return nil, ErrInvalidInput
	}
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin repository deletion claim: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var claim RepositoryDeletionClaim
	err = tx.QueryRow(ctx, `
		SELECT repository.id, repository.organization_id, organization.slug, repository.slug,
		       repository.lore_repository_id, repository.lore_url, deletion.attempts + 1
		FROM repository_deletions deletion
		JOIN repositories repository ON repository.id = deletion.repository_id
		JOIN organizations organization
		  ON organization.id = repository.organization_id
		WHERE deletion.purge_after <= now() AND deletion.next_attempt_at <= now()
		  AND (
		      (repository.lifecycle_state = 'deleting' AND deletion.lease_owner IS NULL)
		      OR (
		          repository.lifecycle_state = 'purging'
		          AND (deletion.lease_owner IS NULL OR deletion.lease_expires_at < now())
		      )
		  )
		ORDER BY deletion.next_attempt_at, deletion.purge_after, repository.id
		LIMIT 1
		FOR UPDATE OF deletion, repository SKIP LOCKED
	`).Scan(
		&claim.RepositoryID,
		&claim.OrganizationID,
		&claim.Owner,
		&claim.Slug,
		&claim.LoreRepositoryID,
		&claim.LoreURL,
		&claim.Attempt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit empty repository deletion claim: %w", err)
		}
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim repository deletion: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE repository_deletions
		SET attempts = attempts + 1, lease_owner = $2,
		    lease_expires_at = now() + $3::interval, last_error = NULL
		WHERE repository_id = $1
	`, claim.RepositoryID, workerID, lease.String()); err != nil {
		return nil, fmt.Errorf("lease repository deletion: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE repositories SET lifecycle_state = 'purging', updated_at = now() WHERE id = $1
	`, claim.RepositoryID); err != nil {
		return nil, fmt.Errorf("mark repository deletion started: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit repository deletion claim: %w", err)
	}
	return &claim, nil
}

func (store *Store) FailRepositoryDeletion(
	ctx context.Context,
	workerID string,
	claim RepositoryDeletionClaim,
	retryAfter time.Duration,
	failure error,
) error {
	if failure == nil || retryAfter <= 0 || retryAfter > 24*time.Hour {
		return ErrInvalidInput
	}
	message := limitText(strings.TrimSpace(failure.Error()), 1000)
	if message == "" {
		message = "repository deletion failed"
	}
	result, err := store.pool.Exec(ctx, `
		UPDATE repository_deletions deletion
		SET lease_owner = NULL, lease_expires_at = NULL,
		    next_attempt_at = now() + $4::interval, last_error = $3
		FROM repositories repository
		WHERE deletion.repository_id = $1 AND deletion.repository_id = repository.id
		  AND deletion.lease_owner = $2 AND repository.lifecycle_state = 'purging'
	`, claim.RepositoryID, workerID, message, retryAfter.String())
	if err != nil {
		return fmt.Errorf("record repository deletion failure: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}

func (store *Store) CompleteRepositoryDeletion(
	ctx context.Context,
	workerID string,
	claim RepositoryDeletionClaim,
) error {
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return fmt.Errorf("begin repository deletion completion: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	var actorID, organizationID, owner, slug, loreRepositoryID string
	var attempt int
	err = tx.QueryRow(ctx, `
		SELECT deletion.requested_by, repository.organization_id, organization.slug,
		       repository.slug, repository.lore_repository_id, deletion.attempts
		FROM repository_deletions deletion
		JOIN repositories repository ON repository.id = deletion.repository_id
		JOIN organizations organization ON organization.id = repository.organization_id
		WHERE repository.id = $1 AND repository.lifecycle_state = 'purging'
		  AND deletion.lease_owner = $2
		FOR UPDATE OF deletion, repository
	`, claim.RepositoryID, workerID).Scan(
		&actorID, &organizationID, &owner, &slug, &loreRepositoryID, &attempt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return fmt.Errorf("lock repository deletion completion: %w", err)
	}
	if organizationID != claim.OrganizationID || owner != claim.Owner || slug != claim.Slug ||
		loreRepositoryID != claim.LoreRepositoryID || attempt != claim.Attempt {
		return ErrConflict
	}
	details := map[string]any{
		"owner": owner, "repository": slug, "loreRepositoryId": loreRepositoryID,
		"attempt": claim.Attempt,
	}
	if err := insertAuditDetails(ctx, tx, actorID, organizationID, claim.RepositoryID,
		"repository.deletion.complete", "repository", claim.RepositoryID, details); err != nil {
		return err
	}
	if err := insertOutbox(ctx, tx, "repository.deleted", claim.RepositoryID+":"+uuid.NewString(),
		map[string]any{
			"repositoryId": claim.RepositoryID, "owner": owner, "repository": slug,
			"loreRepositoryId": loreRepositoryID,
		}); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `DELETE FROM repositories WHERE id = $1`, claim.RepositoryID)
	if err != nil {
		return fmt.Errorf("delete repository control-plane records: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit repository deletion completion: %w", err)
	}
	return nil
}

const repositoryDeletionSelect = `
	SELECT repository.id, repository.organization_id, organization.slug, repository.slug,
	       repository.display_name, repository.lore_repository_id, repository.lore_url,
	       requester.username, deletion.requested_at, deletion.purge_after,
	       repository.lifecycle_state = 'purging', deletion.attempts,
	       COALESCE(deletion.last_error, ''), deletion.next_attempt_at, deletion.lease_expires_at
	FROM repository_deletions deletion
	JOIN repositories repository ON repository.id = deletion.repository_id
	JOIN organizations organization ON organization.id = repository.organization_id AND organization.active
	JOIN users requester ON requester.id = deletion.requested_by
`

func scanDeletedRepository(row rowScanner) (DeletedRepository, error) {
	var deleted DeletedRepository
	err := row.Scan(
		&deleted.ID,
		&deleted.OrganizationID,
		&deleted.Owner,
		&deleted.Slug,
		&deleted.DisplayName,
		&deleted.LoreRepositoryID,
		&deleted.LoreURL,
		&deleted.RequestedBy,
		&deleted.RequestedAt,
		&deleted.PurgeAfter,
		&deleted.Purging,
		&deleted.Attempts,
		&deleted.LastError,
		&deleted.NextAttemptAt,
		&deleted.LeaseExpiresAt,
	)
	return deleted, err
}

func (store *Store) repositoryDeletionOrganization(
	ctx context.Context,
	actorID string,
	owner string,
) (string, error) {
	var organizationID string
	err := store.pool.QueryRow(ctx, `
		SELECT organization.id
		FROM organizations organization
		JOIN users actor_user ON actor_user.id = $2 AND actor_user.status = 'active'
		JOIN organization_memberships membership
		  ON membership.organization_id = organization.id
		 AND membership.user_id = actor_user.id AND membership.role = 'owner' AND membership.active
		WHERE organization.slug = $1 AND organization.active
	`, owner, actorID).Scan(&organizationID)
	if err == nil {
		return organizationID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("authorize deleted repository access: %w", err)
	}
	var exists bool
	if err := store.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM organizations WHERE slug = $1 AND active)
	`, owner).Scan(&exists); err != nil {
		return "", fmt.Errorf("check deletion organization: %w", err)
	}
	if exists {
		return "", ErrForbidden
	}
	return "", ErrNotFound
}

func (store *Store) repositoryDeletionAccessError(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	owner string,
	slug string,
) error {
	var visible bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM repositories repository
			JOIN organizations organization ON organization.id = repository.organization_id
			WHERE organization.slug = $1 AND repository.slug = $2
			  AND `+repositoryAccessClause("repository", "$3")+`
		)
	`, owner, slug, actorID).Scan(&visible)
	if err != nil {
		return fmt.Errorf("check repository deletion visibility: %w", err)
	}
	if visible {
		return ErrForbidden
	}
	return ErrNotFound
}

func (store *Store) repositoryRestoreAccessError(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	owner string,
	slug string,
) error {
	var state string
	err := tx.QueryRow(ctx, `
		SELECT repository.lifecycle_state
		FROM repositories repository
		JOIN organizations organization
		  ON organization.id = repository.organization_id AND organization.active
		JOIN users actor_user ON actor_user.id = $3 AND actor_user.status = 'active'
		JOIN organization_memberships membership
		  ON membership.organization_id = organization.id
		 AND membership.user_id = actor_user.id AND membership.role = 'owner' AND membership.active
		JOIN repository_deletions deletion ON deletion.repository_id = repository.id
		WHERE organization.slug = $1 AND repository.slug = $2
	`, owner, slug, actorID).Scan(&state)
	if err == nil && state == "purging" {
		return ErrConflict
	}
	if err == nil {
		return ErrNotFound
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("check repository restoration state: %w", err)
	}
	if accessErr := store.authorizeRepositoryDeletionOrganizationTx(ctx, tx, actorID, owner); accessErr != nil {
		return accessErr
	}
	return ErrNotFound
}

func (store *Store) authorizeRepositoryDeletionOrganizationTx(
	ctx context.Context,
	tx pgx.Tx,
	actorID string,
	owner string,
) error {
	var organizationID string
	err := tx.QueryRow(ctx, `
		SELECT organization.id
		FROM organizations organization
		JOIN users actor_user ON actor_user.id = $2 AND actor_user.status = 'active'
		JOIN organization_memberships membership
		  ON membership.organization_id = organization.id
		 AND membership.user_id = actor_user.id AND membership.role = 'owner' AND membership.active
		WHERE organization.slug = $1 AND organization.active
	`, owner, actorID).Scan(&organizationID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("authorize repository restoration: %w", err)
	}
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM organizations WHERE slug = $1 AND active)
	`, owner).Scan(&exists); err != nil {
		return fmt.Errorf("check repository restoration organization: %w", err)
	}
	if exists {
		return ErrForbidden
	}
	return ErrNotFound
}
