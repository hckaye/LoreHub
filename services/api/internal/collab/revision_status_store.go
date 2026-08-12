package collab

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// RevisionStatusStore exposes the latest status per context for an exact
// repository revision.
type RevisionStatusStore interface {
	ListRevisionStatusChecks(
		ctx context.Context,
		repositoryID string,
		revision string,
	) ([]RevisionStatusCheck, error)
}

func (s *store) ListRevisionStatusChecks(
	ctx context.Context,
	repositoryID string,
	revision string,
) ([]RevisionStatusCheck, error) {
	return listRevisionStatusChecks(ctx, s.pool, repositoryID, revision)
}

type revisionStatusQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func listRevisionStatusChecks(
	ctx context.Context,
	queryer revisionStatusQueryer,
	repositoryID string,
	revision string,
) ([]RevisionStatusCheck, error) {
	rows, err := queryer.Query(ctx, `
		SELECT DISTINCT ON (lower(status.context))
		       status.context, status.state, COALESCE(status.description, ''),
		       COALESCE(status.target_url, ''),
		       creator.username, status.created_at
		FROM revision_statuses status
		JOIN repositories repository
		  ON repository.id = status.repository_id
		 AND repository.lifecycle_state = 'active'
		 AND repository.archived_at IS NULL
		JOIN organizations organization
		  ON organization.id = repository.organization_id AND organization.active
		JOIN users creator ON creator.id = status.creator_id
		WHERE status.repository_id = $1 AND status.revision = $2
		ORDER BY lower(status.context), status.created_at DESC, status.id DESC
	`, repositoryID, revision)
	if err != nil {
		return nil, fmt.Errorf("list revision status checks: %w", err)
	}
	defer rows.Close()

	checks := make([]RevisionStatusCheck, 0)
	for rows.Next() {
		var check RevisionStatusCheck
		if err := rows.Scan(
			&check.Context,
			&check.State,
			&check.Description,
			&check.TargetURL,
			&check.Creator,
			&check.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan revision status check: %w", err)
		}
		checks = append(checks, check)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate revision status checks: %w", err)
	}
	return checks, nil
}
