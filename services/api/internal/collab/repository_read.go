package collab

import (
	"context"
	"fmt"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

// RepositoryReadStore supplies private/internal list data through the same
// visible-repository lookup used by detail and mutation handlers.
type RepositoryReadStore interface {
	ListIssuesForRepository(ctx context.Context, repositoryID, state string) ([]Issue, error)
	ListMergeRequestsForRepository(ctx context.Context, repositoryID, state string) ([]MergeRequest, error)
	ListCIRunsForRepository(ctx context.Context, repositoryID string) ([]platform.CIRun, error)
}

func (s *store) ListCIRunsForRepository(ctx context.Context, repositoryID string) ([]platform.CIRun, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, run_number, event_name, branch, revision, status, conclusion,
		       queued_at, started_at, completed_at
		FROM ci_runs
		WHERE repository_id = $1
		ORDER BY run_number DESC
		LIMIT 100
	`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list repository CI runs: %w", err)
	}
	defer rows.Close()
	runs := make([]platform.CIRun, 0)
	for rows.Next() {
		var run platform.CIRun
		if err := rows.Scan(&run.ID, &run.RunNumber, &run.EventName, &run.Branch, &run.Revision,
			&run.Status, &run.Conclusion, &run.QueuedAt, &run.StartedAt, &run.CompletedAt); err != nil {
			return nil, fmt.Errorf("scan repository CI run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repository CI runs: %w", err)
	}
	return runs, nil
}

func (s *store) ListIssuesForRepository(ctx context.Context, repositoryID, state string) ([]Issue, error) {
	query := `
		SELECT i.id, i.number, i.title, i.body, i.state, author.username, i.author_id,
		       assignee.username, COUNT(DISTINCT c.id), i.created_at, i.updated_at,
		       closed_by.username, i.closed_at
		FROM issues i
		JOIN users author ON author.id = i.author_id
		LEFT JOIN users assignee ON assignee.id = i.assignee_id
		LEFT JOIN users closed_by ON closed_by.id = i.closed_by
		LEFT JOIN issue_comments c ON c.issue_id = i.id
		WHERE i.repository_id = $1
	`
	args := []any{repositoryID}
	if state == "open" || state == "closed" {
		query += " AND i.state = $2\n"
		args = append(args, state)
	}
	query += ` GROUP BY i.id, author.username, assignee.username, closed_by.username
		ORDER BY i.updated_at DESC, i.id DESC LIMIT 100`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list repository issues: %w", err)
	}
	defer rows.Close()
	issues := make([]Issue, 0)
	for rows.Next() {
		var issue Issue
		if err := rows.Scan(&issue.ID, &issue.Number, &issue.Title, &issue.Body, &issue.State,
			&issue.Author, &issue.AuthorID, &issue.Assignee, &issue.CommentCount, &issue.CreatedAt,
			&issue.UpdatedAt, &issue.ClosedBy, &issue.ClosedAt); err != nil {
			return nil, fmt.Errorf("scan repository issue: %w", err)
		}
		issues = append(issues, issue)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repository issues: %w", err)
	}
	return issues, nil
}

func (s *store) ListMergeRequestsForRepository(
	ctx context.Context,
	repositoryID string,
	state string,
) ([]MergeRequest, error) {
	query := `
		SELECT mr.id, mr.number, mr.title, mr.body, mr.state,
		       mr.source_branch, mr.target_branch, mr.source_revision, mr.target_revision,
		       author.username, mr.author_id, merged.username, mr.merged_revision,
		       mr.merged_at, mr.created_at, mr.updated_at, mr.closed_at,
		       COUNT(rv.id) FILTER (WHERE rv.source_revision = mr.source_revision AND rv.decision = 'approved')
		FROM merge_requests mr
		JOIN users author ON author.id = mr.author_id
		LEFT JOIN users merged ON merged.id = mr.merged_by
		LEFT JOIN merge_request_reviews rv ON rv.merge_request_id = mr.id
		WHERE mr.repository_id = $1
	`
	args := []any{repositoryID}
	if state == "open" || state == "closed" || state == "merged" {
		query += " AND mr.state = $2\n"
		args = append(args, state)
	}
	query += ` GROUP BY mr.id, author.username, merged.username
		ORDER BY mr.updated_at DESC, mr.id DESC LIMIT 100`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list repository merge requests: %w", err)
	}
	defer rows.Close()
	mergeRequests := make([]MergeRequest, 0)
	for rows.Next() {
		var mergeRequest MergeRequest
		if err := rows.Scan(
			&mergeRequest.ID, &mergeRequest.Number, &mergeRequest.Title, &mergeRequest.Body, &mergeRequest.State,
			&mergeRequest.SourceBranch, &mergeRequest.TargetBranch, &mergeRequest.SourceRevision,
			&mergeRequest.TargetRevision, &mergeRequest.Author, &mergeRequest.AuthorID, &mergeRequest.MergedBy,
			&mergeRequest.MergedRevision, &mergeRequest.MergedAt, &mergeRequest.CreatedAt, &mergeRequest.UpdatedAt,
			&mergeRequest.ClosedAt, &mergeRequest.ApprovalCount,
		); err != nil {
			return nil, fmt.Errorf("scan repository merge request: %w", err)
		}
		mergeRequests = append(mergeRequests, mergeRequest)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repository merge requests: %w", err)
	}
	return mergeRequests, nil
}
