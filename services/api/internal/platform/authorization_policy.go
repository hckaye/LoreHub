package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/authz"
)

func (store *Store) GetRepositoryPolicy(
	ctx context.Context,
	actor User,
	owner string,
	repositorySlug string,
) (RepositoryPolicy, error) {
	repositoryID, _, err := store.repositoryAdminAccess(ctx, actor.ID, owner, repositorySlug)
	if err != nil {
		return RepositoryPolicy{}, err
	}
	var policy RepositoryPolicy
	err = store.pool.QueryRow(ctx, `
		SELECT allow_cross_repository_links, obliterate_enabled
		FROM repository_policies
		WHERE repository_id = $1
	`, repositoryID).Scan(&policy.AllowCrossRepositoryLinks, &policy.ObliterateEnabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return RepositoryPolicy{}, ErrNotFound
	}
	if err != nil {
		return RepositoryPolicy{}, fmt.Errorf("get repository policy: %w", err)
	}
	return policy, nil
}

func (store *Store) SetRepositoryPolicy(
	ctx context.Context,
	actor User,
	owner string,
	repositorySlug string,
	input SetRepositoryPolicyInput,
) error {
	repositoryID, organizationID, err := store.repositoryAdminAccess(ctx, actor.ID, owner, repositorySlug)
	if err != nil {
		return err
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin repository policy transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	_, err = transaction.Exec(ctx, `
		INSERT INTO repository_policies (repository_id, allow_cross_repository_links, obliterate_enabled, updated_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (repository_id) DO UPDATE SET
			allow_cross_repository_links = EXCLUDED.allow_cross_repository_links,
			obliterate_enabled = EXCLUDED.obliterate_enabled,
			updated_by = EXCLUDED.updated_by,
			updated_at = now()
	`, repositoryID, input.AllowCrossRepositoryLinks, input.ObliterateEnabled, actor.ID)
	if err != nil {
		return fmt.Errorf("set repository policy: %w", err)
	}
	if err := insertAuditDetails(ctx, transaction, actor.ID, organizationID, repositoryID, "repository.policy.set",
		"repository", repositoryID, map[string]any{
			"allowCrossRepositoryLinks": input.AllowCrossRepositoryLinks,
			"obliterateEnabled":         input.ObliterateEnabled,
		}); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit repository policy transaction: %w", err)
	}
	return nil
}

func (store *Store) SetObliterateGrant(
	ctx context.Context,
	actor User,
	owner string,
	repositorySlug string,
	username string,
	active bool,
) error {
	repositoryID, organizationID, err := store.repositoryAdminAccess(ctx, actor.ID, owner, repositorySlug)
	if err != nil {
		return err
	}
	var actorRole string
	err = store.pool.QueryRow(ctx, `
		SELECT om.role
		FROM repositories r
		JOIN organization_memberships om ON om.organization_id = r.organization_id
		WHERE r.id = $1 AND om.user_id = $2 AND om.active
	`, repositoryID, actor.ID).Scan(&actorRole)
	if err != nil || actorRole != "owner" {
		return ErrForbidden
	}
	var userID string
	err = store.pool.QueryRow(ctx, `
		SELECT u.id
		FROM users u
		JOIN organization_memberships om
		  ON om.user_id = u.id AND om.organization_id = $2 AND om.active
		WHERE u.username = $1 AND u.status = 'active'
	`, strings.ToLower(strings.TrimSpace(username)), organizationID).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("find obliterate grant user: %w", err)
	}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin obliterate grant transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	_, err = transaction.Exec(ctx, `
		INSERT INTO repository_obliterate_grants (repository_id, user_id, granted_by, active, revoked_at)
		VALUES ($1, $2, $3, $4, CASE WHEN $4 THEN NULL ELSE now() END)
		ON CONFLICT (repository_id, user_id) DO UPDATE SET
			granted_by = EXCLUDED.granted_by, active = EXCLUDED.active,
			revoked_at = EXCLUDED.revoked_at
	`, repositoryID, userID, actor.ID, active)
	if err != nil {
		return fmt.Errorf("set obliterate grant: %w", err)
	}
	if err := insertAuditDetails(ctx, transaction, actor.ID, organizationID, repositoryID,
		"repository.obliterate_grant.set", "user", userID, map[string]any{"active": active}); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit obliterate grant transaction: %w", err)
	}
	return nil
}

func (store *Store) DeclareRepositoryLink(
	ctx context.Context,
	actor User,
	sourceOwner string,
	sourceSlug string,
	targetOwner string,
	targetSlug string,
) (RepositoryLink, error) {
	sourceID, sourceOrgID, err := store.repositoryAdminAccess(ctx, actor.ID, sourceOwner, sourceSlug)
	if err != nil {
		return RepositoryLink{}, err
	}
	targetID, targetOrgID, err := store.repositoryAdminAccess(ctx, actor.ID, targetOwner, targetSlug)
	if err != nil {
		return RepositoryLink{}, err
	}
	var allowedSource, allowedTarget bool
	err = store.pool.QueryRow(ctx, `
		SELECT allow_cross_repository_links FROM repository_policies WHERE repository_id = $1
	`, sourceID).Scan(&allowedSource)
	if err != nil {
		return RepositoryLink{}, fmt.Errorf("read source link policy: %w", err)
	}
	err = store.pool.QueryRow(ctx, `
		SELECT allow_cross_repository_links FROM repository_policies WHERE repository_id = $1
	`, targetID).Scan(&allowedTarget)
	if err != nil {
		return RepositoryLink{}, fmt.Errorf("read target link policy: %w", err)
	}
	if !allowedSource || !allowedTarget {
		return RepositoryLink{}, ErrForbidden
	}
	link := RepositoryLink{ID: uuid.NewString(), SourceRepositoryID: sourceID,
		SourceRepository: sourceOwner + "/" + sourceSlug, TargetRepositoryID: targetID,
		TargetRepository: targetOwner + "/" + targetSlug, Kind: "declared", CreatedAt: time.Now().UTC()}
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return RepositoryLink{}, fmt.Errorf("begin repository link transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	_, err = transaction.Exec(ctx, `
		INSERT INTO repository_links (id, source_repository_id, target_repository_id, link_kind, created_by)
		VALUES ($1, $2, $3, 'declared', $4)
	`, link.ID, sourceID, targetID, actor.ID)
	if err != nil {
		return RepositoryLink{}, translateConstraintError("declare repository link", err)
	}
	if err := insertAuditDetails(
		ctx, transaction, actor.ID, sourceOrgID, sourceID, "repository.link.declare", "repository_link", link.ID,
		map[string]any{"targetRepositoryId": targetID, "targetOrganizationId": targetOrgID},
	); err != nil {
		return RepositoryLink{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return RepositoryLink{}, fmt.Errorf("commit repository link transaction: %w", err)
	}
	return link, nil
}

func (store *Store) ListRepositoryLinks(
	ctx context.Context,
	actor User,
	owner string,
	repositorySlug string,
) ([]RepositoryLink, error) {
	repositoryID, _, err := store.repositoryAdminAccess(ctx, actor.ID, owner, repositorySlug)
	if err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT l.id, l.source_repository_id, so.slug, sr.slug,
		       l.target_repository_id, towner.slug, tr.slug, l.link_kind, l.created_at
		FROM repository_links l
		JOIN repositories sr ON sr.id = l.source_repository_id
		JOIN organizations so ON so.id = sr.organization_id
		JOIN repositories tr ON tr.id = l.target_repository_id
		JOIN organizations towner ON towner.id = tr.organization_id
		WHERE l.source_repository_id = $1 OR l.target_repository_id = $1
		ORDER BY l.created_at DESC
	`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list repository links: %w", err)
	}
	defer rows.Close()
	links := make([]RepositoryLink, 0)
	for rows.Next() {
		var link RepositoryLink
		var sourceOwner, sourceSlug, targetOwner, targetSlug string
		if err := rows.Scan(&link.ID, &link.SourceRepositoryID, &sourceOwner, &sourceSlug,
			&link.TargetRepositoryID, &targetOwner, &targetSlug, &link.Kind, &link.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan repository link: %w", err)
		}
		link.SourceRepository = sourceOwner + "/" + sourceSlug
		link.TargetRepository = targetOwner + "/" + targetSlug
		links = append(links, link)
	}
	return links, rows.Err()
}

func insertAuditDetails(
	ctx context.Context,
	transaction pgx.Tx,
	actorID string,
	organizationID string,
	repositoryID string,
	action string,
	targetType string,
	targetID string,
	details map[string]any,
) error {
	encoded, err := json.Marshal(details)
	if err != nil {
		return fmt.Errorf("encode audit details: %w", err)
	}
	_, err = transaction.Exec(ctx, `
		INSERT INTO audit_events (
			id, organization_id, repository_id, actor_id, action, target_type, target_id, details
		) VALUES ($1, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, $4, $5, $6, $7, $8)
	`, uuid.New(), organizationID, repositoryID, actorID, action, targetType, targetID, encoded)
	if err != nil {
		return fmt.Errorf("record detailed audit event: %w", err)
	}
	return insertOutbox(ctx, transaction, "control."+action, uuid.NewString(), map[string]any{
		"actorId": actorID, "organizationId": organizationID, "repositoryId": repositoryID,
		"action": action, "targetType": targetType, "targetId": targetID, "details": details,
	})
}

func (store *Store) repositoryAdminAccess(
	ctx context.Context,
	actorID string,
	owner string,
	repositorySlug string,
) (string, string, error) {
	var repositoryID, organizationID string
	err := store.pool.QueryRow(ctx, `
		SELECT r.id, r.organization_id
		FROM repositories r
		JOIN organizations o ON o.id = r.organization_id
		JOIN users u ON u.id = $3 AND u.status = 'active'
		LEFT JOIN organization_memberships om
		  ON om.organization_id = o.id AND om.user_id = $3 AND om.active
		WHERE o.slug = $1 AND r.slug = $2 AND o.active
		  AND r.archived_at IS NULL AND r.migrating_at IS NULL
		  AND r.lifecycle_state = 'active'
		  AND (
			  om.role = 'owner'
				OR EXISTS (
					SELECT 1 FROM repository_memberships rm
					WHERE rm.repository_id = r.id AND rm.user_id = $3
				    AND rm.active AND rm.role = 'admin'
			  )
				OR EXISTS (
				  SELECT 1
				  FROM team_repository_roles tr
				  JOIN teams t ON t.id = tr.team_id AND t.organization_id = o.id
				  JOIN team_memberships tm ON tm.team_id = t.id AND tm.user_id = $3 AND tm.active
				  JOIN organization_memberships tom
				    ON tom.organization_id = o.id AND tom.user_id = $3 AND tom.active
				  WHERE tr.repository_id = r.id AND tr.active AND t.active AND tr.role = 'admin'
				)
		  )
	`, owner, repositorySlug, actorID).Scan(&repositoryID, &organizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("check repository administration: %w", err)
	}
	return repositoryID, organizationID, nil
}

var _ authz.Store = (*Store)(nil)
