package platform

import (
	"context"
	"fmt"
)

func (store *Store) repositoryVisibleForActor(
	ctx context.Context,
	userID string,
	owner string,
	slug string,
) (bool, error) {
	var visible bool
	err := store.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM repositories r
			JOIN organizations o ON o.id = r.organization_id
			WHERE o.slug = $1 AND r.slug = $2
			  AND `+repositoryAccessClause("r", "$3")+`
		)
	`, owner, slug, userID).Scan(&visible)
	if err != nil {
		return false, fmt.Errorf("check repository visibility: %w", err)
	}
	return visible, nil
}
