package platform

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func searchCounts(
	ctx context.Context,
	tx pgx.Tx,
	query string,
	viewerID string,
	pattern string,
) (SearchCounts, error) {
	var counts SearchCounts
	err := tx.QueryRow(ctx, `
		SELECT
		  (
		    SELECT COUNT(*)
		    FROM repositories r
		    JOIN organizations o ON o.id = r.organization_id AND o.active
		    WHERE $1 <> '' AND `+repositorySearchMatch("r", "o", "$1", "$3")+`
		      AND `+repositoryAccessClause("r", "$2")+`
		  ),
		  (
		    SELECT COUNT(*)
		    FROM organizations o
		    WHERE $1 <> '' AND o.active
		      AND `+organizationSearchMatch("o", "$1", "$3")+`
		      AND (
		        o.visibility = 'public'
		        OR EXISTS (
		          SELECT 1
		          FROM organization_memberships membership
		          JOIN users viewer_user
		            ON viewer_user.id = membership.user_id AND viewer_user.status = 'active'
		          WHERE membership.organization_id = o.id
		            AND membership.user_id = NULLIF($2, '')::uuid AND membership.active
		        )
		      )
		  ),
		  (
		    SELECT COUNT(*)
		    FROM users search_user
		    WHERE $1 <> '' AND search_user.status = 'active'
		      AND `+userSearchMatch("search_user", "$1", "$3")+`
		  ),
		  (
		    SELECT COUNT(*)
		    FROM issues issue
		    JOIN repositories repository ON repository.id = issue.repository_id
		    WHERE $1 <> ''
		      AND `+repositoryAccessClause("repository", "$2")+`
		      AND `+workItemSearchMatch("issue", "$1", "$3")+`
		  ),
		  (
		    SELECT COUNT(*)
		    FROM merge_requests request
		    JOIN repositories repository ON repository.id = request.repository_id
		    WHERE $1 <> ''
		      AND `+repositoryAccessClause("repository", "$2")+`
		      AND `+workItemSearchMatch("request", "$1", "$3")+`
		  )
	`, query, viewerID, pattern).Scan(
		&counts.Repositories, &counts.Organizations, &counts.Users,
		&counts.Issues, &counts.PullRequests,
	)
	if err != nil {
		return SearchCounts{}, fmt.Errorf("count search results: %w", err)
	}
	return counts, nil
}

func repositorySearchMatch(repository string, organization string, query string, pattern string) string {
	return `(
		to_tsvector(
			'simple'::regconfig,
			COALESCE(` + repository + `.slug, '') || ' ' ||
			COALESCE(` + repository + `.display_name, '') || ' ' ||
			COALESCE(` + repository + `.description, '')
		) @@ plainto_tsquery('simple', ` + query + `)
		OR lower(
			COALESCE(` + repository + `.slug, '') || ' ' ||
			COALESCE(` + repository + `.display_name, '') || ' ' ||
			COALESCE(` + repository + `.description, '')
		) LIKE lower(` + pattern + `) ESCAPE '\'
		OR lower(` + organization + `.slug) LIKE lower(` + pattern + `) ESCAPE '\'
		OR EXISTS (
			SELECT 1 FROM repository_topics search_topic
			WHERE search_topic.repository_id = ` + repository + `.id
			  AND search_topic.topic LIKE lower(` + pattern + `) ESCAPE '\'
		)
	)`
}

func organizationSearchMatch(organization string, query string, pattern string) string {
	return `(
		to_tsvector(
			'simple'::regconfig,
			COALESCE(` + organization + `.slug, '') || ' ' ||
			COALESCE(` + organization + `.display_name, '') || ' ' ||
			COALESCE(` + organization + `.description, '')
		) @@ plainto_tsquery('simple', ` + query + `)
		OR lower(
			COALESCE(` + organization + `.slug, '') || ' ' ||
			COALESCE(` + organization + `.display_name, '') || ' ' ||
			COALESCE(` + organization + `.description, '')
		) LIKE lower(` + pattern + `) ESCAPE '\'
	)`
}

func userSearchMatch(user string, query string, pattern string) string {
	return `(
		to_tsvector(
			'simple'::regconfig,
			COALESCE(` + user + `.username, '') || ' ' || COALESCE(` + user + `.display_name, '') || ' ' ||
			COALESCE(` + user + `.bio, '') || ' ' || COALESCE(` + user + `.company, '') || ' ' ||
			COALESCE(` + user + `.location, '')
		) @@ plainto_tsquery('simple', ` + query + `)
		OR lower(
			COALESCE(` + user + `.username, '') || ' ' || COALESCE(` + user + `.display_name, '') || ' ' ||
			COALESCE(` + user + `.bio, '') || ' ' || COALESCE(` + user + `.company, '') || ' ' ||
			COALESCE(` + user + `.location, '')
		) LIKE lower(` + pattern + `) ESCAPE '\'
	)`
}

func workItemSearchMatch(item string, query string, pattern string) string {
	return `(
		to_tsvector('simple', ` + item + `.title || ' ' || ` + item + `.body)
			@@ websearch_to_tsquery('simple', ` + query + `)
		OR (` + item + `.title || ' ' || ` + item + `.body)
			ILIKE ` + pattern + ` ESCAPE '\'
	)`
}
