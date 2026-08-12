package platform

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/authz"
)

type authorizationRepository struct {
	ID                string
	OrganizationID    string
	LoreRepositoryID  string
	Visibility        string
	LifecycleState    string
	Archived          bool
	AllowLinks        bool
	ObliterateEnabled bool
}

func (store *Store) authorizationRepository(
	ctx context.Context,
	resourceID string,
) (authorizationRepository, error) {
	if !authz.ValidResourceID(resourceID) {
		return authorizationRepository{}, authz.ErrInvalidResource
	}
	loreID := strings.TrimPrefix(resourceID, "urc-")
	var repository authorizationRepository
	err := store.pool.QueryRow(ctx, `
		SELECT r.id, r.organization_id, r.lore_repository_id, r.visibility,
		       r.lifecycle_state, r.archived_at IS NOT NULL,
		       p.allow_cross_repository_links, p.obliterate_enabled
		FROM repositories r
		JOIN organizations o ON o.id = r.organization_id AND o.active
		JOIN repository_policies p ON p.repository_id = r.id
		WHERE r.lore_repository_id = $1
	`, loreID).Scan(
		&repository.ID,
		&repository.OrganizationID,
		&repository.LoreRepositoryID,
		&repository.Visibility,
		&repository.LifecycleState,
		&repository.Archived,
		&repository.AllowLinks,
		&repository.ObliterateEnabled,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return authorizationRepository{}, ErrNotFound
	}
	if err != nil {
		return authorizationRepository{}, fmt.Errorf("find Lore repository boundary: %w", err)
	}
	return repository, nil
}

func (store *Store) EffectivePermissions(
	ctx context.Context,
	userID string,
	resourceID string,
) (authz.ResourcePermissions, error) {
	repository, err := store.authorizationRepository(ctx, resourceID)
	if err != nil {
		return authz.ResourcePermissions{}, err
	}
	var serviceKind, serviceVisibility string
	var serviceObliterateEnabled bool
	var servicePermissions []string
	err = store.pool.QueryRow(ctx, `
		SELECT principal.kind, granted_repository.visibility, policy.obliterate_enabled,
		       spg.permissions
		FROM service_principals principal
		JOIN service_principal_repository_grants spg
		  ON spg.principal_id = principal.id AND spg.active
		JOIN repositories granted_repository
		  ON granted_repository.id = spg.repository_id
		JOIN organizations granted_organization
		  ON granted_organization.id = granted_repository.organization_id
		 AND (granted_organization.active OR principal.kind = 'lifecycle')
		JOIN repository_policies policy ON policy.repository_id = granted_repository.id
		WHERE principal.id = $1 AND principal.active AND spg.repository_id = $2
		  AND (
			granted_repository.lifecycle_state = 'active'
			OR (
				principal.kind = 'provisioner'
				AND granted_repository.lifecycle_state IN ('pending', 'failed')
				AND EXISTS (
					SELECT 1 FROM repository_provisioning provisioning
					WHERE provisioning.repository_id = granted_repository.id
					  AND provisioning.state IN ('pending', 'failed')
				)
			)
			OR (
				principal.kind = 'lifecycle'
				AND granted_repository.lifecycle_state IN ('deleting', 'purging')
			)
		  )
	`, userID, repository.ID).Scan(&serviceKind, &serviceVisibility,
		&serviceObliterateEnabled, &servicePermissions)
	if err == nil {
		permissions := policyServicePermissions(servicePermissions, serviceKind, serviceVisibility,
			serviceObliterateEnabled)
		if repository.Archived && serviceKind != "lifecycle" {
			permissions = archivedPermissionList(permissions)
		}
		return authz.ResourcePermissions{
			ResourceID:  resourceID,
			Permissions: permissions,
		}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return authz.ResourcePermissions{}, fmt.Errorf("find service principal permissions: %w", err)
	}
	var status string
	err = store.pool.QueryRow(ctx, `
		SELECT status
		FROM users
		WHERE id = $1
	`, userID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return authz.ResourcePermissions{ResourceID: resourceID}, nil
	}
	if err != nil {
		return authz.ResourcePermissions{}, fmt.Errorf("find Lore user status: %w", err)
	}
	if status != "active" {
		return authz.ResourcePermissions{ResourceID: resourceID}, nil
	}
	var organizationRole string
	err = store.pool.QueryRow(ctx, `
		SELECT role
		FROM organization_memberships
		WHERE organization_id = $1 AND user_id = $2 AND active
	`, repository.OrganizationID, userID).Scan(&organizationRole)
	if errors.Is(err, pgx.ErrNoRows) {
		organizationRole = ""
	} else if err != nil {
		return authz.ResourcePermissions{}, fmt.Errorf("find active organization membership: %w", err)
	}
	permissions := make(map[string]bool)
	if repository.LifecycleState == "active" && repository.Visibility == "public" {
		permissions[authz.PermissionRead] = true
	}
	if repository.LifecycleState == "active" && repository.Visibility == "internal" && organizationRole != "" {
		permissions[authz.PermissionRead] = true
	}
	if repository.LifecycleState == "active" && organizationRole == "owner" {
		mergePermissions(permissions, authz.PermissionsForRole(organizationRole))
	}
	var provisioningActor bool
	err = store.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM repository_provisioning provisioning
			JOIN repositories pending ON pending.id = provisioning.repository_id
			WHERE provisioning.repository_id = $1
			  AND provisioning.requested_by = $2
			  AND provisioning.state IN ('pending', 'failed')
			  AND pending.lifecycle_state IN ('pending', 'failed')
		)
	`, repository.ID, userID).Scan(&provisioningActor)
	if err != nil {
		return authz.ResourcePermissions{}, fmt.Errorf("find repository provisioning actor: %w", err)
	}
	if provisioningActor && (organizationRole == "owner" || organizationRole == "maintainer") {
		permissions[authz.PermissionAdmin] = true
	}
	var role string
	err = store.pool.QueryRow(ctx, `
		SELECT role
		FROM repository_memberships
		WHERE repository_id = $1 AND user_id = $2 AND active
	`, repository.ID, userID).Scan(&role)
	if err == nil && repository.LifecycleState == "active" {
		mergePermissions(permissions, authz.PermissionsForRole(role))
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return authz.ResourcePermissions{}, fmt.Errorf("find direct repository role: %w", err)
	}
	if repository.LifecycleState != "active" {
		return authz.ResourcePermissions{ResourceID: resourceID, Permissions: authz.PermissionList(permissions)}, nil
	}
	rows, err := store.pool.Query(ctx, `
		SELECT tr.role
		FROM team_repository_roles tr
		JOIN team_memberships tm
		  ON tm.team_id = tr.team_id AND tm.user_id = $2 AND tm.active
		JOIN teams t ON t.id = tr.team_id AND t.organization_id = $3 AND t.active
		JOIN organization_memberships om
		  ON om.organization_id = t.organization_id AND om.user_id = $2 AND om.active
		WHERE tr.repository_id = $1 AND tr.active
	`, repository.ID, userID, repository.OrganizationID)
	if err != nil {
		return authz.ResourcePermissions{}, fmt.Errorf("find team repository roles: %w", err)
	}
	for rows.Next() {
		if err := rows.Scan(&role); err != nil {
			rows.Close()
			return authz.ResourcePermissions{}, fmt.Errorf("scan team repository role: %w", err)
		}
		mergePermissions(permissions, authz.PermissionsForRole(role))
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return authz.ResourcePermissions{}, fmt.Errorf("iterate team repository roles: %w", err)
	}
	rows.Close()
	if repository.Archived {
		return authz.ResourcePermissions{
			ResourceID: resourceID, Permissions: archivedPermissionMap(permissions),
		}, nil
	}
	if repository.ObliterateEnabled {
		var granted bool
		err = store.pool.QueryRow(ctx, `
			SELECT active
			FROM repository_obliterate_grants
			WHERE repository_id = $1 AND user_id = $2
		`, repository.ID, userID).Scan(&granted)
		if err == nil && granted {
			permissions[authz.PermissionObliterate] = true
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return authz.ResourcePermissions{}, fmt.Errorf("find obliterate grant: %w", err)
		}
	}
	return authz.ResourcePermissions{
		ResourceID:  resourceID,
		Permissions: authz.PermissionList(permissions),
	}, nil
}

func archivedPermissionList(permissions []string) []string {
	for _, permission := range permissions {
		if permission == authz.PermissionRead {
			return []string{authz.PermissionRead}
		}
	}
	return []string{}
}

func archivedPermissionMap(permissions map[string]bool) []string {
	if permissions[authz.PermissionRead] {
		return []string{authz.PermissionRead}
	}
	return []string{}
}

func mergePermissions(destination map[string]bool, source map[string]bool) {
	for permission, enabled := range source {
		if enabled {
			destination[permission] = true
		}
	}
}

func (store *Store) ListResourcePermissions(
	ctx context.Context,
	userID string,
	resourceFilter string,
	pageSize int,
	pageToken string,
) ([]authz.ResourcePermissions, string, error) {
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	offset := 0
	if pageToken != "" {
		parsed, err := strconv.Atoi(pageToken)
		if err != nil || parsed < 0 {
			return nil, "", errors.New("invalid permission page token")
		}
		offset = parsed
	}
	filter := strings.TrimSpace(resourceFilter)
	if filter != "" && filter != "urc" && filter != "urc-*" && !authz.ValidResourceID(filter) {
		return nil, "", authz.ErrInvalidResource
	}
	where := ""
	args := []any{userID}
	if authz.ValidResourceID(filter) {
		where = " AND r.lore_repository_id = $2"
		args = append(args, strings.TrimPrefix(filter, "urc-"))
	}
	args = append(args, pageSize+1, offset)
	rows, err := store.pool.Query(ctx, `
		SELECT DISTINCT r.lore_repository_id
		FROM repositories r
		JOIN organizations o ON o.id = r.organization_id AND o.active
		JOIN users u ON u.id = $1 AND u.status = 'active'
		WHERE r.lifecycle_state = 'active'
		  AND (
				 r.visibility = 'public'
			  OR (
				  r.visibility = 'internal'
				  AND EXISTS (
					  SELECT 1 FROM organization_memberships om
					  WHERE om.organization_id = r.organization_id
						AND om.user_id = $1 AND om.active
				  )
			  )
			  OR (
				  r.visibility = 'private'
				  AND (
					  EXISTS (
						  SELECT 1 FROM organization_memberships om
						  WHERE om.organization_id = r.organization_id
							AND om.user_id = $1 AND om.role = 'owner' AND om.active
					  )
					  OR EXISTS (
						  SELECT 1 FROM repository_memberships rm
						  WHERE rm.repository_id = r.id AND rm.user_id = $1 AND rm.active
					  )
					  OR EXISTS (
						  SELECT 1
						  FROM team_repository_roles tr
						  JOIN teams t
							ON t.id = tr.team_id AND t.organization_id = r.organization_id AND t.active
						  JOIN team_memberships tm
							ON tm.team_id = t.id AND tm.user_id = $1 AND tm.active
						  JOIN organization_memberships om
							ON om.organization_id = r.organization_id AND om.user_id = $1 AND om.active
						  WHERE tr.repository_id = r.id AND tr.active
					  )
				  )
			  )
		  )`+where+`
		ORDER BY r.lore_repository_id
		LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args))+`
	`, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list authorized Lore resources: %w", err)
	}
	defer rows.Close()
	resourceIDs := make([]string, 0, pageSize+1)
	for rows.Next() {
		var loreID string
		if err := rows.Scan(&loreID); err != nil {
			return nil, "", fmt.Errorf("scan authorized Lore resource: %w", err)
		}
		resourceIDs = append(resourceIDs, "urc-"+loreID)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate authorized Lore resources: %w", err)
	}
	next := ""
	if len(resourceIDs) > pageSize {
		resourceIDs = resourceIDs[:pageSize]
		next = strconv.Itoa(offset + pageSize)
	}
	result := make([]authz.ResourcePermissions, 0, len(resourceIDs))
	for _, resourceID := range resourceIDs {
		permissions, err := store.EffectivePermissions(ctx, userID, resourceID)
		if err != nil {
			return nil, "", err
		}
		if len(permissions.Permissions) > 0 {
			result = append(result, permissions)
		}
	}
	return result, next, nil
}

func (store *Store) UserInfo(ctx context.Context, userID string) (authz.UserInfo, error) {
	var information authz.UserInfo
	err := store.pool.QueryRow(ctx, `
		SELECT id, username, display_name
		FROM users
		WHERE id = $1 AND status = 'active'
	`, userID).Scan(&information.ID, &information.Username, &information.DisplayName)
	if errors.Is(err, pgx.ErrNoRows) {
		return authz.UserInfo{}, ErrNotFound
	}
	if err != nil {
		return authz.UserInfo{}, fmt.Errorf("find Lore user: %w", err)
	}
	return information, nil
}

func (store *Store) ProviderSubject(ctx context.Context, userID string) (string, error) {
	var subject string
	err := store.pool.QueryRow(ctx, `
		SELECT subject
		FROM user_identities
		WHERE user_id = $1
		ORDER BY last_seen_at DESC
		LIMIT 1
	`, userID).Scan(&subject)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("find provider subject: %w", err)
	}
	return subject, nil
}

func (store *Store) CheckPolicy(
	ctx context.Context,
	check authz.PolicyCheck,
) (authz.PolicyDecision, error) {
	permissions, err := store.EffectivePermissions(ctx, check.UserID, check.ResourceID)
	if err != nil {
		return authz.PolicyDecision{}, err
	}
	permissionSet := make(map[string]bool, len(permissions.Permissions))
	for _, permission := range permissions.Permissions {
		permissionSet[permission] = true
	}
	if !authz.RequirePermission(check.Operation, permissionSet) {
		return authz.PolicyDecision{}, nil
	}
	if check.Operation != authz.OperationBranchPush && check.Operation != authz.OperationBranchCreate &&
		check.Operation != authz.OperationBranchDelete && check.Operation != authz.OperationMerge {
		return authz.PolicyDecision{Allowed: true}, nil
	}
	repository, err := store.authorizationRepository(ctx, check.ResourceID)
	if err != nil {
		return authz.PolicyDecision{}, err
	}
	if check.Operation == authz.OperationBranchCreate {
		if check.BranchName == "" {
			return authz.PolicyDecision{}, nil
		}
		policy, err := store.branchPolicy(ctx, repository.ID, check.BranchName)
		if err != nil {
			return authz.PolicyDecision{}, err
		}
		return authz.PolicyDecision{Allowed: !policy.BlockDirectPush}, nil
	}
	if check.BranchID == "" ||
		(check.Operation == authz.OperationBranchPush && check.ProposedRevision == "") {
		return authz.PolicyDecision{}, nil
	}
	branchID, branchName, currentRevision, err := store.resolveBranchPolicyInput(ctx, repository.ID, check.BranchID)
	if err != nil {
		return authz.PolicyDecision{}, err
	}
	if branchID == "" || branchName == "" || currentRevision == "" {
		return authz.PolicyDecision{}, nil
	}
	policy, err := store.branchPolicy(ctx, repository.ID, branchName)
	if err != nil {
		return authz.PolicyDecision{}, err
	}
	if !policy.BlockDirectPush {
		if check.Operation == authz.OperationBranchPush && len(policy.RequiredStatusChecks) > 0 {
			success, err := store.requiredRevisionStatusesSuccessful(
				ctx,
				repository.ID,
				check.ProposedRevision,
				policy.RequiredStatusChecks,
			)
			if err != nil {
				return authz.PolicyDecision{}, err
			}
			return authz.PolicyDecision{Allowed: success}, nil
		}
		return authz.PolicyDecision{Allowed: true}, nil
	}
	if check.Operation == authz.OperationBranchDelete {
		return authz.PolicyDecision{}, nil
	}
	if check.Operation != authz.OperationMerge && check.Operation != authz.OperationBranchPush {
		return authz.PolicyDecision{}, nil
	}
	if check.ProposedRevision == "" {
		return authz.PolicyDecision{}, nil
	}
	consumed, err := store.consumePreparedMergeAuthorization(ctx, repository.ID, check.UserID, branchID,
		branchName, currentRevision, check.ProposedRevision)
	if err != nil {
		return authz.PolicyDecision{}, err
	}
	return authz.PolicyDecision{Allowed: consumed}, nil
}

type branchPolicy struct {
	BlockDirectPush      bool
	RequiredStatusChecks []string
}

type branchPolicyQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func (store *Store) branchPolicy(
	ctx context.Context,
	repositoryID string,
	branchName string,
) (branchPolicy, error) {
	return branchPolicyForQueryer(ctx, store.pool, repositoryID, branchName)
}

func branchPolicyForQueryer(
	ctx context.Context,
	queryer branchPolicyQueryer,
	repositoryID string,
	branchName string,
) (branchPolicy, error) {
	rows, err := queryer.Query(ctx, `
		SELECT rule.pattern, rule.block_direct_push, rule.required_status_checks
		FROM branch_rules rule
		JOIN repositories repository
		  ON repository.id = rule.repository_id
		 AND repository.lifecycle_state = 'active'
		 AND repository.archived_at IS NULL
		JOIN organizations organization
		  ON organization.id = repository.organization_id AND organization.active
		WHERE rule.repository_id = $1
	`, repositoryID)
	if err != nil {
		return branchPolicy{}, fmt.Errorf("find branch policy: %w", err)
	}
	defer rows.Close()
	policy := branchPolicy{RequiredStatusChecks: []string{}}
	contextNames := make(map[string]string)
	for rows.Next() {
		var pattern string
		var blockDirectPush bool
		var requiredStatusChecks []string
		if err := rows.Scan(&pattern, &blockDirectPush, &requiredStatusChecks); err != nil {
			return branchPolicy{}, fmt.Errorf("scan branch policy: %w", err)
		}
		if !authz.MatchBranchPattern(pattern, branchName) {
			continue
		}
		policy.BlockDirectPush = policy.BlockDirectPush || blockDirectPush
		for _, contextName := range requiredStatusChecks {
			key := strings.ToLower(contextName)
			if _, found := contextNames[key]; !found {
				contextNames[key] = contextName
			}
		}
	}
	if err := rows.Err(); err != nil {
		return branchPolicy{}, fmt.Errorf("iterate branch policy: %w", err)
	}
	for _, contextName := range contextNames {
		policy.RequiredStatusChecks = append(policy.RequiredStatusChecks, contextName)
	}
	return policy, nil
}

func (store *Store) requiredRevisionStatusesSuccessful(
	ctx context.Context,
	repositoryID string,
	revision string,
	required []string,
) (bool, error) {
	return requiredRevisionStatusesSuccessful(ctx, store.pool, repositoryID, revision, required)
}

type revisionStatusQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func requiredRevisionStatusesSuccessful(
	ctx context.Context,
	queryer revisionStatusQueryer,
	repositoryID string,
	revision string,
	required []string,
) (bool, error) {
	var successful bool
	err := queryer.QueryRow(ctx, `
		WITH required(context) AS (
			SELECT unnest($3::text[])
		), latest AS (
			SELECT required.context, (
				SELECT status.state
				FROM revision_statuses status
				JOIN repositories repository
				  ON repository.id = status.repository_id
				 AND repository.lifecycle_state = 'active'
				 AND repository.archived_at IS NULL
				JOIN organizations organization
				  ON organization.id = repository.organization_id AND organization.active
				WHERE status.repository_id = $1 AND status.revision = $2
				  AND lower(status.context) = lower(required.context)
				ORDER BY status.created_at DESC, status.id DESC
				LIMIT 1
			) AS state
			FROM required
		)
		SELECT NOT EXISTS (
			SELECT 1 FROM latest WHERE state IS DISTINCT FROM 'success'
		)
	`, repositoryID, revision, required).Scan(&successful)
	if err != nil {
		return false, fmt.Errorf("check branch push revision statuses: %w", err)
	}
	return successful, nil
}

func (store *Store) resolveBranchPolicyInput(
	ctx context.Context,
	repositoryID string,
	branchID string,
) (string, string, string, error) {
	var branchName, currentRevision string
	err := store.pool.QueryRow(ctx, `
		SELECT state.branch_name, state.latest_revision
		FROM repository_branch_states state
		JOIN repositories repository
		  ON repository.id = state.repository_id
		 AND repository.lifecycle_state = 'active'
		 AND repository.archived_at IS NULL
		JOIN organizations organization
		  ON organization.id = repository.organization_id AND organization.active
		WHERE state.repository_id = $1 AND state.branch_id = $2
		  AND state.observed_at > now() - interval '2 minutes'
	`, repositoryID, branchID).Scan(&branchName, &currentRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", nil
	}
	if err != nil {
		return "", "", "", fmt.Errorf("resolve branch policy state: %w", err)
	}
	return branchID, branchName, currentRevision, nil
}

func (store *Store) organizationAccess(
	ctx context.Context,
	actorID string,
	organizationSlug string,
) (string, string, error) {
	var organizationID, role string
	err := store.pool.QueryRow(ctx, `
		SELECT o.id, om.role
		FROM organizations o
		JOIN organization_memberships om
		  ON om.organization_id = o.id AND om.user_id = $2 AND om.active
		JOIN users u ON u.id = om.user_id AND u.status = 'active'
		WHERE o.slug = $1 AND o.active
	`, organizationSlug, actorID).Scan(&organizationID, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("find organization membership: %w", err)
	}
	return organizationID, role, nil
}

func canManageOrganization(role string) bool {
	return role == "owner" || role == "maintainer"
}

func validTeamRole(role string) bool {
	return role == "maintainer" || role == "member"
}

func validRepositoryRole(role string) bool {
	return role == "admin" || role == "maintain" || role == "write" ||
		role == "triage" || role == "read"
}
