package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/authz"
)

const anonymousReaderPrincipalID = "00000000-0000-4000-8000-000000000001"

const ciRunnerPrincipalID = "00000000-0000-4000-8000-000000000002"

const observerPrincipalID = "00000000-0000-4000-8000-000000000003"

const provisionerPrincipalID = "00000000-0000-4000-8000-000000000004"

func addAnonymousReaderGrant(
	ctx context.Context,
	transaction pgx.Tx,
	actorID string,
	organizationID string,
	repositoryID string,
) error {
	_, err := transaction.Exec(ctx, `
		INSERT INTO service_principal_repository_grants (
			principal_id, repository_id, permissions, active, created_by
		) VALUES ($1, $2, ARRAY['read']::varchar[], true, $3)
		ON CONFLICT (principal_id, repository_id) DO UPDATE SET
			permissions = ARRAY['read']::varchar[], active = true,
			updated_at = now(), created_by = EXCLUDED.created_by
	`, anonymousReaderPrincipalID, repositoryID, actorID)
	if err != nil {
		return fmt.Errorf("grant public Lore reader: %w", err)
	}
	return insertAuditDetails(ctx, transaction, actorID, organizationID, repositoryID,
		"service_principal.grant.public_reader", "service_principal", anonymousReaderPrincipalID,
		map[string]any{"permissions": []string{authz.PermissionRead}, "active": true})
}

func addCIReadGrant(
	ctx context.Context,
	transaction pgx.Tx,
	actorID string,
	organizationID string,
	repositoryID string,
) error {
	return addServicePrincipalReadGrant(ctx, transaction, actorID, organizationID, repositoryID,
		ciRunnerPrincipalID, "service_principal.grant.ci_runner")
}

func addObserverReadGrant(
	ctx context.Context,
	transaction pgx.Tx,
	actorID string,
	organizationID string,
	repositoryID string,
) error {
	return addServicePrincipalReadGrant(ctx, transaction, actorID, organizationID, repositoryID,
		observerPrincipalID, "service_principal.grant.observer")
}

func addServicePrincipalReadGrant(
	ctx context.Context,
	transaction pgx.Tx,
	actorID string,
	organizationID string,
	repositoryID string,
	principalID string,
	action string,
) error {
	_, err := transaction.Exec(ctx, `
		INSERT INTO service_principal_repository_grants (
			principal_id, repository_id, permissions, active, created_by
		) VALUES ($1, $2, ARRAY['read']::varchar[], true, $3)
		ON CONFLICT (principal_id, repository_id) DO UPDATE SET
			permissions = ARRAY['read']::varchar[], active = true,
			updated_at = now(), created_by = EXCLUDED.created_by
	`, principalID, repositoryID, actorID)
	if err != nil {
		return fmt.Errorf("grant scoped Lore service principal: %w", err)
	}
	return insertAuditDetails(ctx, transaction, actorID, organizationID, repositoryID, action,
		"service_principal", principalID, map[string]any{
			"permissions": []string{authz.PermissionRead}, "active": true,
		})
}

func addProvisionerGrant(
	ctx context.Context,
	transaction pgx.Tx,
	actorID string,
	organizationID string,
	repositoryID string,
) error {
	_, err := transaction.Exec(ctx, `
		INSERT INTO service_principal_repository_grants (
			principal_id, repository_id, permissions, active, created_by
		) VALUES ($1, $2, ARRAY['read', 'write', 'admin']::varchar[], true, $3)
		ON CONFLICT (principal_id, repository_id) DO UPDATE SET
			permissions = ARRAY['read', 'write', 'admin']::varchar[], active = true,
			updated_at = now(), created_by = EXCLUDED.created_by
	`, provisionerPrincipalID, repositoryID, actorID)
	if err != nil {
		return fmt.Errorf("grant repository provisioning principal: %w", err)
	}
	return insertAuditDetails(ctx, transaction, actorID, organizationID, repositoryID,
		"service_principal.grant.provisioner", "service_principal", provisionerPrincipalID,
		map[string]any{"permissions": []string{authz.PermissionRead, authz.PermissionWrite,
			authz.PermissionAdmin}, "active": true})
}

func (store *Store) ServicePrincipalResource(
	ctx context.Context,
	name string,
	resourceID string,
) (authz.UserInfo, []string, error) {
	if !authz.ValidResourceID(resourceID) || strings.TrimSpace(name) == "" {
		return authz.UserInfo{}, nil, authz.ErrInvalidResource
	}
	var information authz.UserInfo
	var principalKind, visibility string
	var obliterateEnabled bool
	var archived bool
	var permissions []string
	err := store.pool.QueryRow(ctx, `
		SELECT principal.id, principal.name, principal.kind, repository.visibility,
		       policy.obliterate_enabled, spg.permissions, repository.archived_at IS NOT NULL
		FROM service_principals principal
		JOIN service_principal_repository_grants spg
		  ON spg.principal_id = principal.id AND spg.active
		JOIN repositories repository
		  ON repository.id = spg.repository_id
		 AND repository.lore_repository_id = $2
		JOIN organizations organization
		  ON organization.id = repository.organization_id AND organization.active
		JOIN repository_policies policy ON policy.repository_id = repository.id
		WHERE principal.name = $1 AND principal.active
		  AND (
			repository.lifecycle_state = 'active'
			OR (
				principal.kind = 'provisioner'
				AND repository.lifecycle_state IN ('pending', 'failed')
				AND EXISTS (
					SELECT 1 FROM repository_provisioning provisioning
					WHERE provisioning.repository_id = repository.id
					  AND provisioning.state IN ('pending', 'failed')
				)
			)
		  )
	`, strings.TrimSpace(name), strings.TrimPrefix(resourceID, "urc-")).Scan(
		&information.ID, &information.Username, &principalKind, &visibility,
		&obliterateEnabled, &permissions, &archived,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return authz.UserInfo{}, nil, ErrForbidden
	}
	if err != nil {
		return authz.UserInfo{}, nil, fmt.Errorf("find service principal grant: %w", err)
	}
	information.DisplayName = information.Username
	information.ProviderSubject = "service:" + information.Username
	resolved := policyServicePermissions(permissions, principalKind, visibility, obliterateEnabled)
	if archived {
		resolved = archivedPermissionList(resolved)
	}
	return information, resolved, nil
}

func (store *Store) SetServicePrincipalGrant(
	ctx context.Context,
	actor User,
	name string,
	owner string,
	repositorySlug string,
	permissions []string,
	active bool,
) error {
	repositoryID, organizationID, err := store.repositoryAdminAccess(ctx, actor.ID, owner, repositorySlug)
	if err != nil {
		return err
	}
	var actorRole string
	if err := store.pool.QueryRow(ctx, `
		SELECT om.role
		FROM organization_memberships om
		JOIN repositories r ON r.organization_id = om.organization_id
		WHERE r.id = $1 AND om.user_id = $2 AND om.active
	`, repositoryID, actor.ID).Scan(&actorRole); err != nil {
		return ErrForbidden
	}
	if actorRole != "owner" {
		return ErrForbidden
	}
	permissions = normalizeServicePermissions(permissions)
	if len(permissions) == 0 {
		return errors.New("a service principal grant needs an explicit permission")
	}
	var principalID string
	err = store.pool.QueryRow(ctx, `
		SELECT id FROM service_principals WHERE name = $1
	`, strings.TrimSpace(name)).Scan(&principalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("find service principal: %w", err)
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin service principal grant transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	_, err = transaction.Exec(ctx, `
		INSERT INTO service_principal_repository_grants (
			principal_id, repository_id, permissions, active, created_by
		) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (principal_id, repository_id) DO UPDATE SET
			permissions = EXCLUDED.permissions, active = EXCLUDED.active,
			updated_at = now(), created_by = EXCLUDED.created_by
	`, principalID, repositoryID, permissions, active, actor.ID)
	if err != nil {
		return fmt.Errorf("set service principal grant: %w", err)
	}
	if err := insertAuditDetails(ctx, transaction, actor.ID, organizationID, repositoryID,
		"service_principal.grant.set", "service_principal", principalID, map[string]any{
			"name": name, "permissions": permissions, "active": active,
		}); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit service principal grant transaction: %w", err)
	}
	return nil
}

func normalizeServicePermissions(values []string) []string {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != authz.PermissionRead && value != authz.PermissionWrite &&
			value != authz.PermissionAdmin && value != authz.PermissionObliterate {
			return nil
		}
		if !seen[value] {
			seen[value] = true
		}
	}
	return authz.PermissionList(authz.ExpandPermissions(seen))
}

func policyServicePermissions(values []string, kind string, visibility string, obliterateEnabled bool) []string {
	if kind == "anonymous_reader" && visibility != "public" {
		return nil
	}
	permissions := make(map[string]bool)
	for _, permission := range normalizeServicePermissions(values) {
		permissions[permission] = true
	}
	if !obliterateEnabled {
		delete(permissions, authz.PermissionObliterate)
	}
	return authz.PermissionList(permissions)
}
