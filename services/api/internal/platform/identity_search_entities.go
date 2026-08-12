package platform

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func searchRepositories(
	ctx context.Context,
	tx pgx.Tx,
	query string,
	viewerID string,
	pattern string,
	limit int,
	offset int,
) ([]Repository, error) {
	rows, err := tx.Query(ctx, repositorySelect+`
		WHERE $1 <> ''
		  AND (
		      to_tsvector(
		          'simple'::regconfig,
		          COALESCE(r.slug, '') || ' ' || COALESCE(r.display_name, '') || ' ' ||
		          COALESCE(r.description, '')
		      ) @@ plainto_tsquery('simple', $1)
		      OR lower(
		          COALESCE(r.slug, '') || ' ' || COALESCE(r.display_name, '') || ' ' ||
		          COALESCE(r.description, '')
		      ) LIKE lower($3) ESCAPE '\'
		      OR lower(o.slug) LIKE lower($3) ESCAPE '\'
		      OR EXISTS (
		          SELECT 1 FROM repository_topics topic
		          WHERE topic.repository_id = r.id AND topic.topic LIKE lower($3) ESCAPE '\'
		      )
		  )
		  AND `+repositoryAccessClause("r", "$2")+`
		GROUP BY r.id, o.slug
		ORDER BY r.updated_at DESC, r.id DESC
		LIMIT $4 OFFSET $5
	`, query, viewerID, pattern, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("search repositories: %w", err)
	}
	defer rows.Close()
	return scanRepositories(rows)
}

func searchOrganizations(
	ctx context.Context,
	tx pgx.Tx,
	query string,
	viewerID string,
	pattern string,
	limit int,
	offset int,
) ([]OrganizationView, error) {
	rows, err := tx.Query(ctx, `
		SELECT o.id, o.slug, o.display_name, o.description, o.visibility,
		       o.website_url, o.contact_email, o.default_repository_visibility,
		       COALESCE(viewer.role, ''),
		       (
		           SELECT COUNT(*)
		           FROM organization_memberships members
		           JOIN users member_user
		             ON member_user.id = members.user_id AND member_user.status = 'active'
		           WHERE members.organization_id = o.id AND members.active
		       ),
		       (
		           SELECT COUNT(*)
		           FROM repositories r
		           WHERE r.organization_id = o.id
		             AND `+repositoryAccessClause("r", "$2")+`
		       ),
		       (
		           SELECT COUNT(*)
		           FROM teams t
		           WHERE t.organization_id = o.id AND t.active
		       ),
		       o.created_at
		FROM organizations o
		LEFT JOIN organization_memberships viewer
		  ON viewer.organization_id = o.id AND viewer.user_id = NULLIF($2, '')::uuid
		 AND viewer.active
		 AND EXISTS (
		     SELECT 1 FROM users viewer_user
		     WHERE viewer_user.id = viewer.user_id AND viewer_user.status = 'active'
		 )
		WHERE $1 <> ''
		  AND (
		      to_tsvector(
		          'simple'::regconfig,
		          COALESCE(o.slug, '') || ' ' || COALESCE(o.display_name, '') || ' ' ||
		          COALESCE(o.description, '')
		      ) @@ plainto_tsquery('simple', $1)
		      OR lower(
		          COALESCE(o.slug, '') || ' ' || COALESCE(o.display_name, '') || ' ' ||
		          COALESCE(o.description, '')
		      ) LIKE lower($3) ESCAPE '\'
		  )
		  AND o.active AND (o.visibility = 'public' OR viewer.user_id IS NOT NULL)
		ORDER BY o.updated_at DESC, o.id DESC
		LIMIT $4 OFFSET $5
	`, query, viewerID, pattern, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("search organizations: %w", err)
	}
	defer rows.Close()
	return scanOrganizationViews(rows)
}

func searchUsers(
	ctx context.Context,
	tx pgx.Tx,
	query string,
	pattern string,
	limit int,
	offset int,
) ([]UserSearchResult, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, username, display_name, avatar_url
		FROM users
		WHERE $1 <> '' AND status = 'active'
		  AND (
		      to_tsvector(
		          'simple'::regconfig,
		          COALESCE(username, '') || ' ' || COALESCE(display_name, '') || ' ' ||
		          COALESCE(bio, '') || ' ' || COALESCE(company, '') || ' ' || COALESCE(location, '')
		      ) @@ plainto_tsquery('simple', $1)
		      OR lower(
		          COALESCE(username, '') || ' ' || COALESCE(display_name, '') || ' ' ||
		          COALESCE(bio, '') || ' ' || COALESCE(company, '') || ' ' ||
		          COALESCE(location, '')
		      ) LIKE lower($2) ESCAPE '\'
		  )
		ORDER BY lower(username), id
		LIMIT $3 OFFSET $4
	`, query, pattern, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("search users: %w", err)
	}
	defer rows.Close()
	users := make([]UserSearchResult, 0)
	for rows.Next() {
		var result UserSearchResult
		if err := rows.Scan(&result.ID, &result.Username, &result.DisplayName, &result.AvatarURL); err != nil {
			return nil, fmt.Errorf("scan user search result: %w", err)
		}
		users = append(users, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user search results: %w", err)
	}
	return users, nil
}
