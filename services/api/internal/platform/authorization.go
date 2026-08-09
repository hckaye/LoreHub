package platform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/authz"
)

type authorizationRepository struct {
	ID                string
	OrganizationID    string
	LoreRepositoryID  string
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
		SELECT r.id, r.organization_id, r.lore_repository_id,
		       p.allow_cross_repository_links, p.obliterate_enabled
		FROM repositories r
		JOIN repository_policies p ON p.repository_id = r.id
		WHERE r.lore_repository_id = $1 AND r.archived_at IS NULL
	`, loreID).Scan(
		&repository.ID,
		&repository.OrganizationID,
		&repository.LoreRepositoryID,
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
	var status string
	var organizationRole string
	err = store.pool.QueryRow(ctx, `
		SELECT u.status, om.role
		FROM users u
		JOIN organization_memberships om
		  ON om.user_id = u.id AND om.organization_id = $2 AND om.active
		WHERE u.id = $1
	`, userID, repository.OrganizationID).Scan(&status, &organizationRole)
	if errors.Is(err, pgx.ErrNoRows) {
		return authz.ResourcePermissions{ResourceID: resourceID}, nil
	}
	if err != nil {
		return authz.ResourcePermissions{}, fmt.Errorf("find active organization membership: %w", err)
	}
	if status != "active" {
		return authz.ResourcePermissions{ResourceID: resourceID}, nil
	}
	permissions := make(map[string]bool)
	if organizationRole == "owner" || organizationRole == "maintainer" {
		mergePermissions(permissions, authz.PermissionsForRole(organizationRole))
	}
	var role string
	err = store.pool.QueryRow(ctx, `
		SELECT role
		FROM repository_memberships
		WHERE repository_id = $1 AND user_id = $2 AND active
	`, repository.ID, userID).Scan(&role)
	if err == nil {
		mergePermissions(permissions, authz.PermissionsForRole(role))
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return authz.ResourcePermissions{}, fmt.Errorf("find direct repository role: %w", err)
	}
	rows, err := store.pool.Query(ctx, `
		SELECT tr.role
		FROM team_repository_roles tr
		JOIN team_memberships tm
		  ON tm.team_id = tr.team_id AND tm.user_id = $2 AND tm.active
		JOIN teams t ON t.id = tr.team_id AND t.organization_id = $3
		WHERE tr.repository_id = $1
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
		JOIN organization_memberships om
		  ON om.organization_id = r.organization_id AND om.user_id = $1 AND om.active
		JOIN users u ON u.id = om.user_id AND u.status = 'active'
		WHERE r.archived_at IS NULL
		  AND (
			  om.role IN ('owner', 'maintainer')
			  OR EXISTS (
				  SELECT 1 FROM repository_memberships rm
				  WHERE rm.repository_id = r.id AND rm.user_id = $1 AND rm.active
			  )
			  OR EXISTS (
				  SELECT 1
				  FROM team_repository_roles tr
				  JOIN teams t ON t.id = tr.team_id AND t.organization_id = r.organization_id
				  JOIN team_memberships tm
				    ON tm.team_id = t.id AND tm.user_id = $1 AND tm.active
				  WHERE tr.repository_id = r.id
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
	if (check.Operation == authz.OperationBranchPush || check.Operation == authz.OperationMerge) &&
		check.ProposedRevision == "" {
		return authz.PolicyDecision{}, nil
	}
	branchID, branchName, currentRevision, proposedRevision, err := store.resolveBranchPolicyInput(
		ctx,
		repository.ID,
		check,
	)
	if err != nil {
		return authz.PolicyDecision{}, err
	}
	if branchID == "" || branchName == "" ||
		((check.Operation == authz.OperationBranchPush || check.Operation == authz.OperationMerge) &&
			(currentRevision == "" || proposedRevision == "")) {
		return authz.PolicyDecision{}, nil
	}
	rows, err := store.pool.Query(ctx, `
		SELECT pattern, block_direct_push
		FROM branch_rules
		WHERE repository_id = $1
	`, repository.ID)
	if err != nil {
		return authz.PolicyDecision{}, fmt.Errorf("find branch policy: %w", err)
	}
	protected := false
	for rows.Next() {
		var pattern string
		var blockDirectPush bool
		if err := rows.Scan(&pattern, &blockDirectPush); err != nil {
			rows.Close()
			return authz.PolicyDecision{}, fmt.Errorf("scan branch policy: %w", err)
		}
		if blockDirectPush && branchMatches(pattern, branchName) {
			protected = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return authz.PolicyDecision{}, fmt.Errorf("iterate branch policy: %w", err)
	}
	rows.Close()
	if !protected {
		return authz.PolicyDecision{Allowed: true}, nil
	}
	if check.Operation != authz.OperationMerge && check.Operation != authz.OperationBranchPush {
		return authz.PolicyDecision{}, nil
	}
	var consumed bool
	if check.OperationAuthorization != "" {
		consumed, err = store.consumeMergeAuthorization(
			ctx,
			check.OperationAuthorization,
			repository.ID,
			check.UserID,
			branchID,
			branchName,
			currentRevision,
			proposedRevision,
		)
	} else if check.Operation == authz.OperationBranchPush {
		// Lore 0.8.6's pre-hook context carries the branch ID and proposed hash,
		// but has no merge-operation field. A pending authorization is therefore
		// matched on every immutable merge binding and consumed once atomically.
		consumed, err = store.consumePendingMergeAuthorization(
			ctx,
			repository.ID,
			check.UserID,
			branchID,
			branchName,
			currentRevision,
			proposedRevision,
		)
	} else {
		return authz.PolicyDecision{}, nil
	}
	if err != nil {
		return authz.PolicyDecision{}, err
	}
	return authz.PolicyDecision{Allowed: consumed}, nil
}

func (store *Store) resolveBranchPolicyInput(
	ctx context.Context,
	repositoryID string,
	check authz.PolicyCheck,
) (string, string, string, string, error) {
	branchID := check.BranchID
	branchName := check.BranchName
	currentRevision := check.CurrentRevision
	proposedRevision := check.ProposedRevision
	if branchID == "" || branchName == "" || currentRevision == "" {
		var observedID, observedName, observedRevision string
		err := store.pool.QueryRow(ctx, `
			SELECT branch_id, branch_name, latest_revision
			FROM repository_branch_states
			WHERE repository_id = $1
			  AND ($2 = '' OR branch_id = $2)
			  AND ($3 = '' OR branch_name = $3)
			ORDER BY observed_at DESC
			LIMIT 1
		`, repositoryID, branchID, branchName).Scan(&observedID, &observedName, &observedRevision)
		if err == nil {
			if branchID == "" {
				branchID = observedID
			}
			if branchName == "" {
				branchName = observedName
			}
			if currentRevision == "" {
				currentRevision = observedRevision
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return "", "", "", "", fmt.Errorf("resolve branch policy state: %w", err)
		}
	}
	if proposedRevision == "" {
		proposedRevision = currentRevision
	}
	return branchID, branchName, currentRevision, proposedRevision, nil
}

func branchMatches(pattern string, branchName string) bool {
	if branchName == "" {
		return false
	}
	matched, err := path.Match(pattern, branchName)
	return err == nil && matched
}

type MergeAuthorization struct {
	Authorization string    `json:"authorization"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

func (store *Store) IssueMergeAuthorization(
	ctx context.Context,
	actor User,
	input MergeAuthorizationInput,
) (MergeAuthorization, error) {
	if input.Lifetime <= 0 || input.Lifetime > 5*time.Minute {
		input.Lifetime = 2 * time.Minute
	}
	resourceID := "urc-" + input.RepositoryID
	repository, err := store.authorizationRepository(ctx, resourceID)
	if err != nil {
		return MergeAuthorization{}, err
	}
	permissions, err := store.EffectivePermissions(ctx, actor.ID, resourceID)
	if err != nil {
		return MergeAuthorization{}, err
	}
	permissionSet := make(map[string]bool, len(permissions.Permissions))
	for _, permission := range permissions.Permissions {
		permissionSet[permission] = true
	}
	if !permissionSet[authz.PermissionWrite] {
		return MergeAuthorization{}, ErrForbidden
	}
	if input.BranchID == "" || input.BranchName == "" || input.ExpectedBase == "" || input.ExpectedHead == "" {
		return MergeAuthorization{}, errors.New("merge authorization fields are required")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return MergeAuthorization{}, fmt.Errorf("generate merge authorization: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	expiresAt := time.Now().UTC().Add(input.Lifetime)
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return MergeAuthorization{}, fmt.Errorf("begin merge authorization transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	_, err = transaction.Exec(ctx, `
		INSERT INTO lore_operation_authorizations (
			id, token_digest, repository_id, user_id, operation, branch_id, branch_name,
			expected_base_revision, expected_head_revision, expires_at
		) VALUES ($1, $2, $3, $4, 'merge', $5, $6, $7, $8, $9)
	`, uuid.New(), digest[:], repository.ID, actor.ID, input.BranchID, input.BranchName,
		input.ExpectedBase, input.ExpectedHead, expiresAt)
	if err != nil {
		return MergeAuthorization{}, fmt.Errorf("store merge authorization: %w", err)
	}
	if err := insertAuditDetails(ctx, transaction, actor.ID, repository.OrganizationID, repository.ID,
		"repository.merge_authorization.issue", "repository", repository.ID, map[string]any{
			"branchId": input.BranchID, "branchName": input.BranchName,
			"expectedBaseRevision": input.ExpectedBase, "expectedHeadRevision": input.ExpectedHead,
			"expiresAt": expiresAt,
		}); err != nil {
		return MergeAuthorization{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return MergeAuthorization{}, fmt.Errorf("commit merge authorization transaction: %w", err)
	}
	return MergeAuthorization{Authorization: token, ExpiresAt: expiresAt}, nil
}

func (store *Store) consumeMergeAuthorization(
	ctx context.Context,
	token string,
	repositoryID string,
	userID string,
	branchID string,
	branchName string,
	baseRevision string,
	headRevision string,
) (bool, error) {
	digest := sha256.Sum256([]byte(token))
	command, err := store.pool.Exec(ctx, `
		UPDATE lore_operation_authorizations
		SET consumed_at = now()
		WHERE token_digest = $1 AND repository_id = $2 AND user_id = $3
		  AND operation = 'merge' AND branch_id = $4 AND branch_name = $5
		  AND expected_base_revision = $6 AND expected_head_revision = $7
		  AND expires_at > now() AND consumed_at IS NULL
	`, digest[:], repositoryID, userID, branchID, branchName, baseRevision, headRevision)
	if err != nil {
		return false, fmt.Errorf("consume merge authorization: %w", err)
	}
	return command.RowsAffected() == 1, nil
}

func (store *Store) consumePendingMergeAuthorization(
	ctx context.Context,
	repositoryID string,
	userID string,
	branchID string,
	branchName string,
	baseRevision string,
	headRevision string,
) (bool, error) {
	command, err := store.pool.Exec(ctx, `
		WITH candidate AS (
			SELECT id
			FROM lore_operation_authorizations
			WHERE repository_id = $1 AND user_id = $2 AND operation = 'merge'
			  AND branch_id = $3 AND branch_name = $4
			  AND expected_base_revision = $5 AND expected_head_revision = $6
			  AND expires_at > now() AND consumed_at IS NULL
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE lore_operation_authorizations pending
		SET consumed_at = now()
		FROM candidate
		WHERE pending.id = candidate.id
	`, repositoryID, userID, branchID, branchName, baseRevision, headRevision)
	if err != nil {
		return false, fmt.Errorf("consume pending merge authorization: %w", err)
	}
	return command.RowsAffected() == 1, nil
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
		WHERE o.slug = $1
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
		WHERE organization_id = $1
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
		WHERE organization_id = $1 AND slug = $2
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
		WHERE organization_id = $1 AND slug = $2
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
		WHERE t.organization_id = $1 AND t.slug = $2
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
		WHERE t.organization_id = $1 AND t.slug = $2
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

func (store *Store) SetTeamRepositoryRole(
	ctx context.Context,
	actor User,
	organizationSlug string,
	teamSlug string,
	owner string,
	repositorySlug string,
	input SetTeamRepositoryRoleInput,
) (TeamRepositoryRole, error) {
	organizationID, organizationRole, err := store.organizationAccess(ctx, actor.ID, organizationSlug)
	if err != nil {
		return TeamRepositoryRole{}, err
	}
	if !canManageOrganization(organizationRole) || !validRepositoryRole(input.Role) {
		return TeamRepositoryRole{}, ErrForbidden
	}
	var result TeamRepositoryRole
	err = store.pool.QueryRow(ctx, `
		SELECT t.id, r.id, o.slug, r.slug
		FROM teams t
		JOIN repositories r ON r.organization_id = t.organization_id
		JOIN organizations o ON o.id = r.organization_id
		WHERE t.organization_id = $1 AND t.slug = $2 AND o.slug = $3 AND r.slug = $4
	`, organizationID, teamSlug, owner, repositorySlug).Scan(&result.TeamID, &result.RepositoryID,
		&result.Owner, &result.Repository)
	if errors.Is(err, pgx.ErrNoRows) {
		return TeamRepositoryRole{}, ErrNotFound
	}
	if err != nil {
		return TeamRepositoryRole{}, fmt.Errorf("find team repository: %w", err)
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return TeamRepositoryRole{}, fmt.Errorf("begin team repository role transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	_, err = transaction.Exec(ctx, `
		INSERT INTO team_repository_roles (team_id, repository_id, role, created_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (team_id, repository_id) DO UPDATE
		SET role = EXCLUDED.role, updated_at = now()
	`, result.TeamID, result.RepositoryID, input.Role, actor.ID)
	if err != nil {
		return TeamRepositoryRole{}, fmt.Errorf("set team repository role: %w", err)
	}
	result.Role = input.Role
	result.CreatedAt = time.Now().UTC()
	result.UpdatedAt = result.CreatedAt
	if err := insertAuditDetails(ctx, transaction, actor.ID, organizationID, result.RepositoryID,
		"team.repository_role.set", "team", result.TeamID,
		map[string]any{"repositoryId": result.RepositoryID, "role": input.Role}); err != nil {
		return TeamRepositoryRole{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return TeamRepositoryRole{}, fmt.Errorf("commit team repository role transaction: %w", err)
	}
	return result, nil
}

func (store *Store) DeleteTeamRepositoryRole(
	ctx context.Context,
	actor User,
	organizationSlug string,
	teamSlug string,
	owner string,
	repositorySlug string,
) error {
	organizationID, organizationRole, err := store.organizationAccess(ctx, actor.ID, organizationSlug)
	if err != nil {
		return err
	}
	if !canManageOrganization(organizationRole) {
		return ErrForbidden
	}
	var teamID, repositoryID string
	err = store.pool.QueryRow(ctx, `
		SELECT t.id, r.id
		FROM teams t
		JOIN repositories r ON r.organization_id = t.organization_id
		JOIN organizations o ON o.id = r.organization_id
		WHERE t.organization_id = $1 AND t.slug = $2 AND o.slug = $3 AND r.slug = $4
	`, organizationID, teamSlug, owner, repositorySlug).Scan(&teamID, &repositoryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("find team repository role: %w", err)
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin team repository role delete transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	command, err := transaction.Exec(ctx, `
		DELETE FROM team_repository_roles
		WHERE team_id = $1 AND repository_id = $2
	`, teamID, repositoryID)
	if err != nil {
		return fmt.Errorf("delete team repository role: %w", err)
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := insertAuditDetails(ctx, transaction, actor.ID, organizationID, repositoryID,
		"team.repository_role.delete", "team", teamID,
		map[string]any{"repositoryId": repositoryID}); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit team repository role delete transaction: %w", err)
	}
	return nil
}
