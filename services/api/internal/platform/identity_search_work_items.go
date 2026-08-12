package platform

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func searchIssueItems(
	ctx context.Context,
	tx pgx.Tx,
	query string,
	viewerID string,
	pattern string,
	limit int,
	offset int,
) ([]GlobalWorkItem, error) {
	rows, err := tx.Query(ctx, searchIssueQuery, query, viewerID, pattern, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("search issues: %w", err)
	}
	defer rows.Close()
	return scanSearchWorkItems(rows)
}

func searchPullRequestItems(
	ctx context.Context,
	tx pgx.Tx,
	query string,
	viewerID string,
	pattern string,
	limit int,
	offset int,
) ([]GlobalWorkItem, error) {
	rows, err := tx.Query(ctx, searchPullRequestQuery, query, viewerID, pattern, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("search pull requests: %w", err)
	}
	defer rows.Close()
	return scanSearchWorkItems(rows)
}

func scanSearchWorkItems(rows pgx.Rows) ([]GlobalWorkItem, error) {
	items := make([]GlobalWorkItem, 0)
	for rows.Next() {
		item, err := scanGlobalWorkItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate search work items: %w", err)
	}
	return items, nil
}

var searchIssueQuery = `
	SELECT issue.id, 'issue', repository.id, organization.slug, repository.slug,
	       repository.display_name, issue.number, issue.title, issue.state, false,
	       author.id, author.username, author.display_name, author.avatar_url,
	       COALESCE((
	           SELECT jsonb_agg(` + globalWorkItemUserJSON + ` ORDER BY assignment.assigned_at, item_user.id)
	           FROM issue_assignees assignment
	           JOIN users item_user ON item_user.id = assignment.user_id AND item_user.status = 'active'
	           WHERE assignment.issue_id = issue.id
	       ), '[]'::jsonb),
	       COALESCE((
	           SELECT jsonb_agg(` + globalWorkItemLabelJSON + ` ORDER BY item_label.name, item_label.id)
	           FROM issue_labels applied_label
	           JOIN labels item_label ON item_label.id = applied_label.label_id
	           WHERE applied_label.issue_id = issue.id
	       ), '[]'::jsonb),
	       milestone.number, milestone.title, issue.comment_count,
	       0, '', '', issue.created_at, issue.updated_at
	FROM issues issue
	JOIN repositories repository ON repository.id = issue.repository_id
	JOIN organizations organization ON organization.id = repository.organization_id
	JOIN users author ON author.id = issue.author_id
	LEFT JOIN repository_milestones milestone ON milestone.id = issue.milestone_id
	WHERE $1 <> ''
	  AND ` + repositoryAccessClause("repository", "$2") + `
	  AND ` + workItemSearchMatch("issue", "$1", "$3") + `
	ORDER BY issue.updated_at DESC, issue.id DESC
	LIMIT $4 OFFSET $5
`

var searchPullRequestQuery = `
	SELECT request.id, 'pull_request', repository.id, organization.slug, repository.slug,
	       repository.display_name, request.number, request.title, request.state, request.is_draft,
	       author.id, author.username, author.display_name, author.avatar_url,
	       COALESCE((
	           SELECT jsonb_agg(` + globalWorkItemUserJSON + ` ORDER BY assignment.assigned_at, item_user.id)
	           FROM merge_request_assignees assignment
	           JOIN users item_user ON item_user.id = assignment.user_id AND item_user.status = 'active'
	           WHERE assignment.merge_request_id = request.id
	       ), '[]'::jsonb),
	       COALESCE((
	           SELECT jsonb_agg(` + globalWorkItemLabelJSON + ` ORDER BY item_label.name, item_label.id)
	           FROM merge_request_labels applied_label
	           JOIN labels item_label ON item_label.id = applied_label.label_id
	           WHERE applied_label.merge_request_id = request.id
	       ), '[]'::jsonb),
	       milestone.number, milestone.title, request.comment_count,
	       (
	           SELECT COUNT(*) FROM merge_request_reviews review
	           WHERE review.merge_request_id = request.id
	             AND review.source_revision = request.source_revision
	             AND review.decision = 'approved'
	       ),
	       request.source_branch, request.target_branch,
	       request.created_at, request.updated_at
	FROM merge_requests request
	JOIN repositories repository ON repository.id = request.repository_id
	JOIN organizations organization ON organization.id = repository.organization_id
	JOIN users author ON author.id = request.author_id
	LEFT JOIN repository_milestones milestone ON milestone.id = request.milestone_id
	WHERE $1 <> ''
	  AND ` + repositoryAccessClause("repository", "$2") + `
	  AND ` + workItemSearchMatch("request", "$1", "$3") + `
	ORDER BY request.updated_at DESC, request.id DESC
	LIMIT $4 OFFSET $5
`
