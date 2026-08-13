package platform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (store *Store) BeginRepositoryProvisioning(
	ctx context.Context,
	actor User,
	organizationSlug string,
	input ProvisionRepositoryInput,
	explicitServerID string,
) (Repository, error) {
	if err := validateSlug(input.Slug); err != nil {
		return Repository{}, err
	}
	if input.DisplayName == "" || len(input.DisplayName) > 200 || len(input.Description) > 10_000 {
		return Repository{}, errors.New("repository fields are invalid")
	}
	if input.Visibility != "public" && input.Visibility != "internal" && input.Visibility != "private" {
		return Repository{}, errors.New("repository visibility is invalid")
	}
	if input.DefaultBranch == "" {
		input.DefaultBranch = "main"
	}
	organizationID, err := store.organizationManager(ctx, actor.ID, organizationSlug)
	if err != nil {
		return Repository{}, err
	}
	loreRepositoryID, err := newLorePartitionID()
	if err != nil {
		return Repository{}, fmt.Errorf("create canonical Lore repository ID: %w", err)
	}
	now := time.Now().UTC()
	repository := Repository{
		ID:               uuid.NewString(),
		OrganizationID:   organizationID,
		Owner:            organizationSlug,
		Slug:             input.Slug,
		DisplayName:      input.DisplayName,
		Description:      input.Description,
		Visibility:       input.Visibility,
		LoreRepositoryID: loreRepositoryID,
		DefaultBranch:    input.DefaultBranch,
		LifecycleState:   "pending",
		UpdatedAt:        now,
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Repository{}, fmt.Errorf("begin repository provisioning transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	var lockedOrganizationID string
	if err := transaction.QueryRow(ctx, `
		SELECT id FROM organizations WHERE id = $1 FOR UPDATE
	`, organizationID).Scan(&lockedOrganizationID); err != nil {
		return Repository{}, fmt.Errorf("lock organization provisioning state: %w", err)
	}
	var existingID string
	err = transaction.QueryRow(ctx, `
		SELECT id
		FROM repositories
		WHERE organization_id = $1 AND slug = $2 AND lifecycle_state IN ('pending', 'failed')
	`, organizationID, input.Slug).Scan(&existingID)
	if err == nil {
		_ = transaction.Rollback(context.WithoutCancel(ctx))
		return store.RepositoryForProvisioning(ctx, actor, organizationSlug, input.Slug)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Repository{}, fmt.Errorf("find existing repository provisioning state: %w", err)
	}
	server, err := resolveServerForNewRepository(ctx, transaction, organizationID, strings.TrimSpace(explicitServerID))
	if err != nil {
		return Repository{}, err
	}
	publicLoreURL, err := loreRepositoryURL(server.PublicURL, loreRepositoryID)
	if err != nil {
		return Repository{}, err
	}
	repository.LoreURL = publicLoreURL
	repository.LoreServerID = server.ID
	_, err = transaction.Exec(ctx, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, description, visibility,
			lore_repository_id, lore_url, lore_server_id, default_branch, created_by,
			lifecycle_state, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 'pending', $12, $12)
	`, repository.ID, repository.OrganizationID, repository.Slug, repository.DisplayName,
		repository.Description, repository.Visibility, repository.LoreRepositoryID, repository.LoreURL,
		repository.LoreServerID, repository.DefaultBranch, actor.ID, now)
	if err != nil {
		return Repository{}, translateConstraintError("begin repository provisioning", err)
	}
	if _, err := transaction.Exec(ctx, `
		INSERT INTO repository_counters (repository_id) VALUES ($1)
	`, repository.ID); err != nil {
		return Repository{}, fmt.Errorf("create repository counters: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		INSERT INTO repository_policies (repository_id) VALUES ($1)
	`, repository.ID); err != nil {
		return Repository{}, fmt.Errorf("create repository policy: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		INSERT INTO repository_provisioning (repository_id, requested_by, public_lore_url)
		VALUES ($1, $2, $3)
	`, repository.ID, actor.ID, publicLoreURL); err != nil {
		return Repository{}, fmt.Errorf("create repository provisioning record: %w", err)
	}
	if err := addProvisionerGrant(ctx, transaction, actor.ID, organizationID, repository.ID); err != nil {
		return Repository{}, err
	}
	if input.Visibility == "public" {
		if err := addAnonymousReaderGrant(ctx, transaction, actor.ID, organizationID, repository.ID); err != nil {
			return Repository{}, err
		}
	}
	if err := addCIReadGrant(ctx, transaction, actor.ID, organizationID, repository.ID); err != nil {
		return Repository{}, err
	}
	if err := addObserverReadGrant(ctx, transaction, actor.ID, organizationID, repository.ID); err != nil {
		return Repository{}, err
	}
	if err := insertAuditDetails(ctx, transaction, actor.ID, organizationID, repository.ID,
		"repository.provisioning.started", "repository", repository.ID, map[string]any{
			"loreRepositoryId": repository.LoreRepositoryID,
			"publicLoreUrl":    publicLoreURL,
		}); err != nil {
		return Repository{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return Repository{}, fmt.Errorf("commit repository provisioning transaction: %w", err)
	}
	return repository, nil
}

func (store *Store) RepositoryForProvisioning(
	ctx context.Context,
	actor User,
	owner string,
	slug string,
) (Repository, error) {
	row := store.pool.QueryRow(ctx, repositorySelect+`
		JOIN users actor_user ON actor_user.id = $3 AND actor_user.status = 'active'
		JOIN organization_memberships actor_membership
		  ON actor_membership.organization_id = r.organization_id
		 AND actor_membership.user_id = $3 AND actor_membership.active
		WHERE o.slug = $1 AND r.slug = $2 AND r.archived_at IS NULL
		  AND r.lifecycle_state IN ('pending', 'failed')
		  AND actor_membership.role IN ('owner', 'maintainer')
		GROUP BY r.id, o.slug, actor_membership.role
	`, owner, slug, actor.ID)
	repository, err := scanRepository(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Repository{}, ErrNotFound
	}
	if err != nil {
		return Repository{}, fmt.Errorf("find repository provisioning record: %w", err)
	}
	return repository, nil
}

func (store *Store) MarkRepositoryProvisioned(
	ctx context.Context,
	actor User,
	repositoryID string,
) error {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin repository provisioning completion: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	var organizationID string
	command, err := transaction.Exec(ctx, `
		UPDATE repositories r
		SET lifecycle_state = 'active', provisioning_error = NULL, updated_at = now()
		WHERE r.id = $1 AND r.lifecycle_state IN ('pending', 'failed')
		  AND EXISTS (
			SELECT 1
			FROM repository_provisioning p
			JOIN users u ON u.id = $2 AND u.status = 'active'
			JOIN organization_memberships om
			  ON om.organization_id = r.organization_id
			 AND om.user_id = u.id AND om.active
			WHERE p.repository_id = r.id AND p.state IN ('pending', 'failed')
			  AND om.role IN ('owner', 'maintainer')
		  )
	`, repositoryID, actor.ID)
	if err != nil {
		return fmt.Errorf("activate provisioned repository: %w", err)
	}
	if command.RowsAffected() != 1 {
		var lifecycleState, provisioningState string
		checkErr := transaction.QueryRow(ctx, `
			SELECT r.lifecycle_state, p.state
			FROM repositories r
			JOIN repository_provisioning p ON p.repository_id = r.id
			JOIN users u ON u.id = $2 AND u.status = 'active'
			JOIN organization_memberships om
			  ON om.organization_id = r.organization_id
			 AND om.user_id = u.id AND om.active
			WHERE r.id = $1 AND om.role IN ('owner', 'maintainer')
		`, repositoryID, actor.ID).Scan(&lifecycleState, &provisioningState)
		if checkErr == nil && lifecycleState == "active" && provisioningState == "active" {
			_ = transaction.Rollback(context.WithoutCancel(ctx))
			return nil
		}
		if checkErr != nil && !errors.Is(checkErr, pgx.ErrNoRows) {
			return fmt.Errorf("check repository provisioning completion: %w", checkErr)
		}
		return ErrConflict
	}
	if err := transaction.QueryRow(ctx, `
		SELECT organization_id FROM repositories WHERE id = $1
	`, repositoryID).Scan(&organizationID); err != nil {
		return fmt.Errorf("find provisioned repository organization: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE repository_provisioning
		SET state = 'active', completed_at = now(), updated_at = now(), last_error = NULL
		WHERE repository_id = $1
	`, repositoryID); err != nil {
		return fmt.Errorf("complete repository provisioning record: %w", err)
	}
	grant, err := transaction.Exec(ctx, `
		UPDATE service_principal_repository_grants
		SET active = false, updated_at = now()
		WHERE principal_id = $1 AND repository_id = $2 AND active
	`, provisionerPrincipalID, repositoryID)
	if err != nil {
		return fmt.Errorf("revoke repository provisioning principal: %w", err)
	}
	if grant.RowsAffected() == 1 {
		if err := insertAuditDetails(ctx, transaction, actor.ID, organizationID, repositoryID,
			"service_principal.grant.provisioner.revoke", "service_principal", provisionerPrincipalID,
			map[string]any{"active": false}); err != nil {
			return err
		}
	}
	if err := insertAuditDetails(ctx, transaction, actor.ID, organizationID, repositoryID,
		"repository.provisioning.completed", "repository", repositoryID, nil); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit repository provisioning completion: %w", err)
	}
	return nil
}

func (store *Store) MarkRepositoryProvisioningFailed(
	ctx context.Context,
	actor User,
	repositoryID string,
	message string,
) error {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "Lore repository provisioning failed"
	}
	if len(message) > 500 {
		message = message[:500]
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin repository provisioning failure: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	var organizationID string
	command, err := transaction.Exec(ctx, `
		UPDATE repositories r
		SET lifecycle_state = 'failed', provisioning_error = $3, updated_at = now()
		WHERE r.id = $1 AND r.lifecycle_state IN ('pending', 'failed')
		  AND EXISTS (
			SELECT 1
			FROM repository_provisioning p
			JOIN users u ON u.id = $2 AND u.status = 'active'
			JOIN organization_memberships om
			  ON om.organization_id = r.organization_id
			 AND om.user_id = u.id AND om.active
			WHERE p.repository_id = r.id AND p.state IN ('pending', 'failed')
			  AND om.role IN ('owner', 'maintainer')
		  )
	`, repositoryID, actor.ID, message)
	if err != nil {
		return fmt.Errorf("record repository provisioning failure: %w", err)
	}
	if command.RowsAffected() != 1 {
		var lifecycleState, provisioningState string
		checkErr := transaction.QueryRow(ctx, `
			SELECT r.lifecycle_state, p.state
			FROM repositories r
			JOIN repository_provisioning p ON p.repository_id = r.id
			JOIN users u ON u.id = $2 AND u.status = 'active'
			JOIN organization_memberships om
			  ON om.organization_id = r.organization_id
			 AND om.user_id = u.id AND om.active
			WHERE r.id = $1 AND om.role IN ('owner', 'maintainer')
		`, repositoryID, actor.ID).Scan(&lifecycleState, &provisioningState)
		if checkErr == nil && lifecycleState == "active" && provisioningState == "active" {
			_ = transaction.Rollback(context.WithoutCancel(ctx))
			return nil
		}
		if checkErr != nil && !errors.Is(checkErr, pgx.ErrNoRows) {
			return fmt.Errorf("check repository provisioning failure state: %w", checkErr)
		}
		return ErrNotFound
	}
	if err := transaction.QueryRow(ctx, `
		SELECT organization_id FROM repositories WHERE id = $1
	`, repositoryID).Scan(&organizationID); err != nil {
		return fmt.Errorf("find failed repository organization: %w", err)
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE repository_provisioning
		SET state = 'failed', attempts = attempts + 1, last_error = $2, updated_at = now()
		WHERE repository_id = $1
	`, repositoryID, message); err != nil {
		return fmt.Errorf("record repository provisioning attempt: %w", err)
	}
	if err := insertAuditDetails(ctx, transaction, actor.ID, organizationID, repositoryID,
		"repository.provisioning.failed", "repository", repositoryID, map[string]any{
			"error": message,
		}); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit repository provisioning failure: %w", err)
	}
	return nil
}

func (store *Store) organizationManager(ctx context.Context, userID, slug string) (string, error) {
	var organizationID, role string
	err := store.pool.QueryRow(ctx, `
		SELECT o.id, om.role
		FROM organizations o
		JOIN organization_memberships om
		  ON om.organization_id = o.id AND om.user_id = $2 AND om.active
		JOIN users u ON u.id = om.user_id AND u.status = 'active'
		WHERE o.slug = $1 AND o.active
	`, slug, userID).Scan(&organizationID, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrForbidden
	}
	if err != nil {
		return "", fmt.Errorf("find organization manager: %w", err)
	}
	if role != "owner" && role != "maintainer" {
		return "", ErrForbidden
	}
	return organizationID, nil
}

func newLorePartitionID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func validatePublicLoreURL(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "lores" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && !isLorePartitionID(strings.TrimPrefix(parsed.Path, "/"))) {
		return errors.New("the public Lore URL must be a fixed lores:// endpoint")
	}
	return nil
}

func loreRepositoryURL(base string, partition string) (string, error) {
	if err := validatePublicLoreURL(base); err != nil {
		return "", err
	}
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return "", errors.New("the public Lore URL is invalid")
	}
	if parsed.Path != "" && parsed.Path != "/"+partition {
		return "", errors.New("the public Lore URL partition does not match the canonical ID")
	}
	parsed.Path = "/" + partition
	return parsed.String(), nil
}
