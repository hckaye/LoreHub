package platform

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/authz"
)

// PrepareMergeAuthorization is called by the trusted merge worker after it has
// calculated the exact proposed revision. It stores no client-held credential.
func (store *Store) PrepareMergeAuthorization(
	ctx context.Context,
	userID string,
	input MergeAuthorizationInput,
) error {
	if input.Lifetime <= 0 || input.Lifetime > 5*time.Minute {
		input.Lifetime = 2 * time.Minute
	}
	resourceID := "urc-" + input.RepositoryID
	repository, err := store.authorizationRepository(ctx, resourceID)
	if err != nil {
		return err
	}
	permissions, err := store.EffectivePermissions(ctx, userID, resourceID)
	if err != nil {
		return err
	}
	permissionSet := make(map[string]bool, len(permissions.Permissions))
	for _, permission := range permissions.Permissions {
		permissionSet[permission] = true
	}
	if !authz.ExpandPermissions(permissionSet)[authz.PermissionWrite] {
		return ErrForbidden
	}
	if input.BranchID == "" || input.BranchName == "" || input.ExpectedBase == "" ||
		input.ExpectedHead == "" || input.SourceRevision == "" {
		return errors.New("merge authorization fields are required")
	}
	expiresAt := time.Now().UTC().Add(input.Lifetime)
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin merge authorization transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	_, err = transaction.Exec(ctx, `
		INSERT INTO lore_merge_authorizations (
			id, repository_id, user_id, target_branch_id, target_branch_name,
			expected_current_revision, proposed_revision, source_revision, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, uuid.New(), repository.ID, userID, input.BranchID, input.BranchName,
		input.ExpectedBase, input.ExpectedHead, input.SourceRevision, expiresAt)
	if err != nil {
		return fmt.Errorf("store merge authorization: %w", err)
	}
	if err := insertAuditDetails(ctx, transaction, userID, repository.OrganizationID, repository.ID,
		"repository.merge_authorization.issue", "repository", repository.ID, map[string]any{
			"branchId": input.BranchID, "branchName": input.BranchName,
			"expectedBaseRevision": input.ExpectedBase, "expectedHeadRevision": input.ExpectedHead,
			"sourceRevision": input.SourceRevision, "expiresAt": expiresAt,
		}); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit merge authorization transaction: %w", err)
	}
	return nil
}

func (store *Store) consumePreparedMergeAuthorization(
	ctx context.Context,
	repositoryID string,
	userID string,
	branchID string,
	branchName string,
	currentRevision string,
	proposedRevision string,
) (bool, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, fmt.Errorf("begin merge authorization consumption: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	var authorizationID, organizationID, sourceRevision string
	err = transaction.QueryRow(ctx, `
		WITH consumed AS (
			UPDATE lore_merge_authorizations
			SET consumed_at = now()
			WHERE repository_id = $1 AND user_id = $2 AND target_branch_id = $3
			  AND target_branch_name = $4 AND expected_current_revision = $5
			  AND proposed_revision = $6 AND source_revision <> ''
			  AND expires_at > now() AND consumed_at IS NULL
			RETURNING id, repository_id, source_revision
		)
		SELECT consumed.id, repository.organization_id, consumed.source_revision
		FROM consumed
		JOIN repositories repository ON repository.id = consumed.repository_id
	`, repositoryID, userID, branchID, branchName, currentRevision, proposedRevision).
		Scan(&authorizationID, &organizationID, &sourceRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("consume merge authorization: %w", err)
	}
	if err := insertAuditDetails(ctx, transaction, userID, organizationID, repositoryID,
		"repository.merge_authorization.consume", "merge_authorization", authorizationID,
		map[string]any{"branchId": branchID, "branchName": branchName,
			"expectedCurrentRevision": currentRevision, "proposedRevision": proposedRevision,
			"sourceRevision": sourceRevision}); err != nil {
		return false, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit merge authorization consumption: %w", err)
	}
	return true, nil
}
