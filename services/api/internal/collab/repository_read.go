package collab

import (
	"context"
	"fmt"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

// RepositoryReadStore supplies private/internal list data through the same
// visible-repository lookup used by detail and mutation handlers.
type RepositoryReadStore interface {
	ListIssuesForRepository(
		context.Context,
		string,
		RepositoryIssueQuery,
	) (RepositoryIssuePage, error)
	ListMergeRequestsForRepository(
		context.Context,
		string,
		RepositoryMergeRequestQuery,
	) (RepositoryMergeRequestPage, error)
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
