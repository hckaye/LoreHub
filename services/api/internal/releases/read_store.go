package releases

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type releaseQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

const releaseSelect = `
	SELECT release.id, release.tag_name, release.title, release.notes,
	       release.source_branch, release.revision, release.state,
	       creator.username, publisher.username, release.published_at,
	       release.version, release.created_at, release.updated_at,
	       COALESCE(
	           jsonb_agg(
	               jsonb_build_object(
	                   'id', asset.id,
	                   'name', asset.name,
	                   'externalUrl', asset.external_url,
	                   'createdBy', asset_creator.username,
	                   'createdAt', asset.created_at
	               ) ORDER BY asset.created_at, asset.id
	           ) FILTER (WHERE asset.id IS NOT NULL),
	           '[]'::jsonb
	       )
	FROM repository_releases release
	JOIN users creator ON creator.id = release.created_by
	LEFT JOIN users publisher ON publisher.id = release.published_by
	LEFT JOIN release_asset_links asset ON asset.release_id = release.id
	LEFT JOIN users asset_creator ON asset_creator.id = asset.created_by
`

func (store *store) List(
	ctx context.Context,
	repositoryID string,
	includeDrafts bool,
	page int,
	perPage int,
) (ReleasePage, error) {
	offset := (page - 1) * perPage
	rows, err := store.pool.Query(ctx, releaseSelect+`
		WHERE release.repository_id = $1 AND ($2 OR release.state = 'published')
		GROUP BY release.id, creator.username, publisher.username
		ORDER BY (release.state = 'published') DESC,
		         release.published_at DESC NULLS LAST, release.updated_at DESC, release.id DESC
		LIMIT $3 OFFSET $4
	`, repositoryID, includeDrafts, perPage+1, offset)
	if err != nil {
		return ReleasePage{}, fmt.Errorf("list releases: %w", err)
	}
	defer rows.Close()
	items := make([]Release, 0, perPage+1)
	for rows.Next() {
		release, scanErr := scanRelease(rows)
		if scanErr != nil {
			return ReleasePage{}, fmt.Errorf("scan release: %w", scanErr)
		}
		items = append(items, release)
	}
	if err := rows.Err(); err != nil {
		return ReleasePage{}, fmt.Errorf("iterate releases: %w", err)
	}
	hasNext := len(items) > perPage
	if hasNext {
		items = items[:perPage]
	}
	return ReleasePage{Releases: items, Page: page, PerPage: perPage, HasNext: hasNext}, nil
}

func (store *store) Get(
	ctx context.Context,
	repositoryID string,
	releaseID string,
	includeDrafts bool,
) (Release, error) {
	return loadRelease(ctx, store.pool, repositoryID, releaseID, includeDrafts)
}

func loadRelease(
	ctx context.Context,
	database releaseQueryer,
	repositoryID string,
	releaseID string,
	includeDrafts bool,
) (Release, error) {
	release, err := scanRelease(database.QueryRow(ctx, releaseSelect+`
		WHERE release.repository_id = $1 AND release.id = $2
		  AND ($3 OR release.state = 'published')
		GROUP BY release.id, creator.username, publisher.username
	`, repositoryID, releaseID, includeDrafts))
	if errors.Is(err, pgx.ErrNoRows) {
		return Release{}, platform.ErrNotFound
	}
	if err != nil {
		return Release{}, fmt.Errorf("get release: %w", err)
	}
	return release, nil
}

func scanRelease(row pgx.Row) (Release, error) {
	var release Release
	var assetsJSON []byte
	err := row.Scan(
		&release.ID, &release.TagName, &release.Title, &release.Notes,
		&release.SourceBranch, &release.Revision, &release.State,
		&release.CreatedBy, &release.PublishedBy, &release.PublishedAt,
		&release.Version, &release.CreatedAt, &release.UpdatedAt, &assetsJSON,
	)
	if err != nil {
		return Release{}, err
	}
	if err := json.Unmarshal(assetsJSON, &release.Assets); err != nil {
		return Release{}, fmt.Errorf("decode release assets: %w", err)
	}
	if release.Assets == nil {
		release.Assets = make([]AssetLink, 0)
	}
	return release, nil
}
