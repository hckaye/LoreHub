package runner

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type RepositoryAccess struct {
	ID             string
	OrganizationID string
	Owner          string
	Slug           string
	LoreURL        string
	DefaultBranch  string
	Visibility     string
	CanRead        bool
	CanWrite       bool
}

func (store *Store) RepositoryForActions(
	ctx context.Context,
	owner string,
	slug string,
	actorID string,
) (RepositoryAccess, error) {
	if actorID != "" {
		if _, err := uuid.Parse(actorID); err != nil {
			return RepositoryAccess{}, ErrActionNotFound
		}
	}
	var repository RepositoryAccess
	err := store.pool.QueryRow(ctx, `
		SELECT r.id, r.organization_id, o.slug, r.slug, r.lore_url, r.default_branch, r.visibility,
		       ($3 = '' AND r.visibility = 'public')
		       OR ($3 <> '' AND EXISTS (
		           SELECT 1
		           FROM users u
		           WHERE u.id = NULLIF($3, '')::uuid AND u.status = 'active'
		       ) AND (
		           r.visibility = 'public'
		           OR EXISTS (
		               SELECT 1
		               FROM organization_memberships om
		               WHERE om.organization_id = r.organization_id
		                 AND om.user_id = NULLIF($3, '')::uuid AND om.active
		                 AND r.visibility = 'internal'
		           )
		           OR EXISTS (
		               SELECT 1
		               FROM organization_memberships om
		               WHERE om.organization_id = r.organization_id
		                 AND om.user_id = NULLIF($3, '')::uuid AND om.active
		                 AND r.visibility = 'private'
		                 AND (
		                     om.role = 'owner'
		                     OR EXISTS (
		                         SELECT 1
		                         FROM repository_memberships rm
		                         WHERE rm.repository_id = r.id AND rm.user_id = om.user_id AND rm.active
		                     )
		                     OR EXISTS (
		                         SELECT 1
	                         FROM teams t
	                         JOIN team_memberships tm ON tm.team_id = t.id
	                         JOIN team_repository_roles tr ON tr.team_id = t.id
		                         WHERE t.organization_id = r.organization_id AND t.active
		                           AND tm.user_id = om.user_id AND tm.active
		                           AND tr.repository_id = r.id AND tr.active
		                     )
		                 )
		           )
		       )),
		       $3 <> '' AND EXISTS (
		           SELECT 1
		           FROM users u
		           JOIN organization_memberships om ON om.organization_id = r.organization_id
		           WHERE u.id = NULLIF($3, '')::uuid AND u.status = 'active'
		             AND om.user_id = u.id AND om.active
		             AND (
		                 om.role = 'owner'
		                 OR EXISTS (
		                     SELECT 1
		                     FROM repository_memberships rm
		                     WHERE rm.repository_id = r.id AND rm.user_id = u.id AND rm.active
		                       AND rm.role IN ('admin', 'maintain', 'write')
		                 )
		                 OR EXISTS (
		                     SELECT 1
	                     FROM teams t
	                     JOIN team_memberships tm ON tm.team_id = t.id
	                     JOIN team_repository_roles tr ON tr.team_id = t.id
		                     WHERE t.organization_id = r.organization_id AND t.active
		                       AND tm.user_id = u.id AND tm.active
		                       AND tr.repository_id = r.id AND tr.active
		                       AND tr.role IN ('admin', 'maintain', 'write')
		                 )
		             )
		       )
		FROM repositories r
		JOIN organizations o ON o.id = r.organization_id
		WHERE o.slug = $1 AND r.slug = $2 AND r.archived_at IS NULL AND o.active
	`, owner, slug, actorID).Scan(
		&repository.ID,
		&repository.OrganizationID,
		&repository.Owner,
		&repository.Slug,
		&repository.LoreURL,
		&repository.DefaultBranch,
		&repository.Visibility,
		&repository.CanRead,
		&repository.CanWrite,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return RepositoryAccess{}, ErrActionNotFound
	}
	if err != nil {
		return RepositoryAccess{}, fmt.Errorf("find Actions repository: %w", err)
	}
	if !repository.CanRead {
		return RepositoryAccess{}, ErrActionNotFound
	}
	return repository, nil
}
