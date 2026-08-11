package wiki

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func (store *store) List(
	ctx context.Context,
	repositoryID string,
	query string,
	limit int,
) ([]PageSummary, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := store.pool.Query(ctx, `
		SELECT page.id, page.slug, page.title, page.version,
		       creator.username, editor.username, page.created_at, page.updated_at
		FROM repository_wiki_pages page
		JOIN users creator ON creator.id = page.created_by
		JOIN users editor ON editor.id = page.updated_by
		WHERE page.repository_id = $1 AND page.archived_at IS NULL
		  AND ($2 = '' OR page.title ILIKE '%' || $2 || '%' OR page.slug ILIKE '%' || $2 || '%')
		ORDER BY page.updated_at DESC, page.id DESC
		LIMIT $3
	`, repositoryID, query, limit)
	if err != nil {
		return nil, fmt.Errorf("list wiki pages: %w", err)
	}
	defer rows.Close()
	pages := make([]PageSummary, 0)
	for rows.Next() {
		var page PageSummary
		if err := rows.Scan(
			&page.ID, &page.Slug, &page.Title, &page.Version,
			&page.CreatedBy, &page.UpdatedBy, &page.CreatedAt, &page.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan wiki page: %w", err)
		}
		pages = append(pages, page)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate wiki pages: %w", err)
	}
	return pages, nil
}

func (store *store) Get(ctx context.Context, repositoryID string, slug string) (Page, error) {
	page, err := scanPage(store.pool.QueryRow(ctx, pageQuery+`
		WHERE page.repository_id = $1 AND page.slug = $2 AND page.archived_at IS NULL
	`, repositoryID, slug))
	if errors.Is(err, pgx.ErrNoRows) {
		return Page{}, platform.ErrNotFound
	}
	if err != nil {
		return Page{}, fmt.Errorf("get wiki page: %w", err)
	}
	return page, nil
}

func (store *store) History(
	ctx context.Context,
	repositoryID string,
	slug string,
	limit int,
) ([]Revision, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	rows, err := store.pool.Query(ctx, `
		SELECT version.version, version.slug, version.title, version.edit_summary,
		       editor.username, version.created_at
		FROM repository_wiki_pages page
		JOIN repository_wiki_page_versions version
		  ON version.page_id = page.id AND version.repository_id = page.repository_id
		JOIN users editor ON editor.id = version.edited_by
		WHERE page.repository_id = $1 AND page.slug = $2 AND page.archived_at IS NULL
		ORDER BY version.version DESC
		LIMIT $3
	`, repositoryID, slug, limit)
	if err != nil {
		return nil, fmt.Errorf("list wiki page history: %w", err)
	}
	defer rows.Close()
	revisions := make([]Revision, 0)
	for rows.Next() {
		var revision Revision
		if err := rows.Scan(
			&revision.Version, &revision.Slug, &revision.Title, &revision.EditSummary,
			&revision.EditedBy, &revision.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan wiki page revision: %w", err)
		}
		revisions = append(revisions, revision)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate wiki page history: %w", err)
	}
	if len(revisions) == 0 {
		return nil, platform.ErrNotFound
	}
	return revisions, nil
}

func (store *store) Revision(
	ctx context.Context,
	repositoryID string,
	slug string,
	version int,
) (Revision, error) {
	var revision Revision
	err := store.pool.QueryRow(ctx, `
		SELECT history.version, history.slug, history.title, history.body,
		       history.edit_summary, editor.username, history.created_at
		FROM repository_wiki_pages page
		JOIN repository_wiki_page_versions history
		  ON history.page_id = page.id AND history.repository_id = page.repository_id
		JOIN users editor ON editor.id = history.edited_by
		WHERE page.repository_id = $1 AND page.slug = $2
		  AND page.archived_at IS NULL AND history.version = $3
	`, repositoryID, slug, version).Scan(
		&revision.Version, &revision.Slug, &revision.Title, &revision.Body,
		&revision.EditSummary, &revision.EditedBy, &revision.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Revision{}, platform.ErrNotFound
	}
	if err != nil {
		return Revision{}, fmt.Errorf("get wiki page revision: %w", err)
	}
	return revision, nil
}

const pageQuery = `
	SELECT page.id, page.slug, page.title, page.version,
	       creator.username, editor.username, page.created_at, page.updated_at, page.body
	FROM repository_wiki_pages page
	JOIN users creator ON creator.id = page.created_by
	JOIN users editor ON editor.id = page.updated_by
`

type rowScanner interface {
	Scan(...any) error
}

func scanPage(row rowScanner) (Page, error) {
	var page Page
	err := row.Scan(
		&page.ID, &page.Slug, &page.Title, &page.Version,
		&page.CreatedBy, &page.UpdatedBy, &page.CreatedAt, &page.UpdatedAt, &page.Body,
	)
	return page, err
}
