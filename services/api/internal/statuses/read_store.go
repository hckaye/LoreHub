package statuses

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const statusSelect = `
	SELECT status.id::text, status.revision, status.context, status.state,
	       status.description, status.target_url,
	       creator.id::text, creator.username, creator.display_name, creator.avatar_url,
	       status.created_at
	FROM revision_statuses status
	JOIN users creator ON creator.id = status.creator_id
`

func (store *store) List(
	ctx context.Context,
	repositoryID string,
	revision string,
	page int,
	perPage int,
) (Page, error) {
	var err error
	revision, err = validateRevision(revision)
	if err != nil {
		return Page{}, err
	}
	page, perPage = normalizePagination(page, perPage)
	tx, err := store.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return Page{}, fmt.Errorf("begin revision status list: %w", err)
	}
	defer rollback(ctx, tx)
	var repositoryActive bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM repositories repository
			JOIN organizations organization
			  ON organization.id = repository.organization_id AND organization.active
			WHERE repository.id = $1 AND repository.lifecycle_state = 'active'
		)
	`, repositoryID).Scan(&repositoryActive); err != nil {
		return Page{}, fmt.Errorf("authorize revision status list: %w", err)
	}
	if !repositoryActive {
		return Page{}, platform.ErrNotFound
	}
	var total int64
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM revision_statuses status
		JOIN repositories repository ON repository.id = status.repository_id
		JOIN organizations organization
		  ON organization.id = repository.organization_id AND organization.active
		WHERE status.repository_id = $1 AND status.revision = $2
		  AND repository.lifecycle_state = 'active'
	`, repositoryID, revision).Scan(&total); err != nil {
		return Page{}, fmt.Errorf("count revision statuses: %w", err)
	}
	latest, err := queryStatuses(ctx, tx, statusSelect+`
		WHERE status.id IN (
			SELECT DISTINCT ON (lower(candidate.context)) candidate.id
			FROM revision_statuses candidate
			JOIN repositories repository ON repository.id = candidate.repository_id
			JOIN organizations organization
			  ON organization.id = repository.organization_id AND organization.active
			WHERE candidate.repository_id = $1 AND candidate.revision = $2
			  AND repository.lifecycle_state = 'active'
			ORDER BY lower(candidate.context), candidate.created_at DESC, candidate.id DESC
		)
		ORDER BY status.context, status.created_at DESC, status.id DESC
	`, repositoryID, revision)
	if err != nil {
		return Page{}, fmt.Errorf("list latest revision statuses: %w", err)
	}
	history, err := queryStatuses(ctx, tx, statusSelect+`
		WHERE status.repository_id = $1 AND status.revision = $2
		  AND EXISTS (
		    SELECT 1 FROM repositories repository
		    JOIN organizations organization
		      ON organization.id = repository.organization_id AND organization.active
		    WHERE repository.id = status.repository_id
		      AND repository.lifecycle_state = 'active'
		  )
		ORDER BY status.created_at DESC, status.id DESC
		LIMIT $3 OFFSET $4
	`, repositoryID, revision, perPage+1, (page-1)*perPage)
	if err != nil {
		return Page{}, fmt.Errorf("list revision status history: %w", err)
	}
	hasNext := len(history) > perPage
	if hasNext {
		history = history[:perPage]
	}
	result := Page{
		Revision: revision, State: combinedState(latest), Statuses: latest,
		History: history, Page: page, PerPage: perPage, TotalCount: total, HasNext: hasNext,
	}
	if err := commit(ctx, tx, "revision status list"); err != nil {
		return Page{}, err
	}
	return result, nil
}

func queryStatuses(
	ctx context.Context,
	queries interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	},
	query string,
	arguments ...any,
) ([]Status, error) {
	rows, err := queries.Query(ctx, query, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	statuses := make([]Status, 0)
	for rows.Next() {
		status, err := scanStatus(rows)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return statuses, nil
}

func scanStatus(row rowScanner) (Status, error) {
	var status Status
	err := row.Scan(
		&status.ID, &status.Revision, &status.Context, &status.State,
		&status.Description, &status.TargetURL,
		&status.Creator.ID, &status.Creator.Username,
		&status.Creator.DisplayName, &status.Creator.AvatarURL,
		&status.CreatedAt,
	)
	return status, err
}

func combinedState(statuses []Status) string {
	if len(statuses) == 0 {
		return "pending"
	}
	pending := false
	for _, status := range statuses {
		switch status.State {
		case "error", "failure":
			return "failure"
		case "pending":
			pending = true
		}
	}
	if pending {
		return "pending"
	}
	return "success"
}

func normalizePagination(page int, perPage int) (int, int) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = defaultHistoryPerPage
	}
	if perPage > maxHistoryPerPage {
		perPage = maxHistoryPerPage
	}
	return page, perPage
}
