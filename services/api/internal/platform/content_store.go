package platform

import (
	"context"
	"errors"
	"fmt"
)

func (store *Store) ObserveBranchState(
	ctx context.Context,
	repositoryID string,
	branchID string,
	branchName string,
	latestRevision string,
) error {
	if repositoryID == "" || branchID == "" || branchName == "" || latestRevision == "" {
		return errors.New("the Lore branch observation is incomplete")
	}
	_, err := store.pool.Exec(ctx, `
		INSERT INTO repository_branch_states (
			repository_id, branch_id, branch_name, latest_revision, observed_at
		) VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (repository_id, branch_id) DO UPDATE SET
			branch_name = EXCLUDED.branch_name,
			latest_revision = EXCLUDED.latest_revision,
			observed_at = EXCLUDED.observed_at
	`, repositoryID, branchID, branchName, latestRevision)
	if err != nil {
		return fmt.Errorf("record Lore branch state: %w", err)
	}
	return nil
}

func (store *Store) ObserveLoreBranchRevision(
	ctx context.Context,
	loreRepositoryID string,
	branchID string,
	revision string,
) error {
	if loreRepositoryID == "" || branchID == "" || revision == "" {
		return errors.New("the Lore branch revision observation is incomplete")
	}
	command, err := store.pool.Exec(ctx, `
		UPDATE repository_branch_states states
		SET latest_revision = $3, observed_at = now()
		FROM repositories repositories
		WHERE repositories.id = states.repository_id
		  AND repositories.lore_repository_id = $1
		  AND repositories.lifecycle_state = 'active'
		  AND states.branch_id = $2
	`, loreRepositoryID, branchID, revision)
	if err != nil {
		return fmt.Errorf("record Lore branch revision: %w", err)
	}
	if command.RowsAffected() != 1 {
		return errors.New("the Lore branch is not observed in the control plane")
	}
	return nil
}

func (store *Store) DeleteLoreBranchState(
	ctx context.Context,
	loreRepositoryID string,
	branchID string,
) error {
	if loreRepositoryID == "" || branchID == "" {
		return errors.New("the Lore branch deletion observation is incomplete")
	}
	_, err := store.pool.Exec(ctx, `
		DELETE FROM repository_branch_states states
		USING repositories repositories
		WHERE repositories.id = states.repository_id
		  AND repositories.lore_repository_id = $1
		  AND states.branch_id = $2
	`, loreRepositoryID, branchID)
	if err != nil {
		return fmt.Errorf("remove Lore branch state: %w", err)
	}
	return nil
}

func (store *Store) ListIssuesForRead(
	ctx context.Context,
	actor *User,
	repository Repository,
	state string,
) ([]Issue, error) {
	if _, err := store.RepositoryForRead(ctx, actor, repository.Owner, repository.Slug); err != nil {
		return nil, err
	}
	if state != "open" && state != "closed" {
		state = "open"
	}
	rows, err := store.pool.Query(ctx, `
		SELECT i.id, i.number, i.title, i.body, i.state, author.username,
		       assignee.username, COUNT(c.id), i.created_at, i.updated_at
		FROM issues i
		JOIN users author ON author.id = i.author_id
		LEFT JOIN users assignee ON assignee.id = i.assignee_id
		LEFT JOIN issue_comments c ON c.issue_id = i.id
		WHERE i.repository_id = $1 AND i.state = $2
		GROUP BY i.id, author.username, assignee.username
		ORDER BY i.updated_at DESC
		LIMIT 100
	`, repository.ID, state)
	if err != nil {
		return nil, fmt.Errorf("list authorized issues: %w", err)
	}
	defer rows.Close()
	issues := make([]Issue, 0)
	for rows.Next() {
		var issue Issue
		if err := rows.Scan(&issue.ID, &issue.Number, &issue.Title, &issue.Body, &issue.State, &issue.Author,
			&issue.Assignee, &issue.CommentCount, &issue.CreatedAt, &issue.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan authorized issue: %w", err)
		}
		issues = append(issues, issue)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate authorized issues: %w", err)
	}
	return issues, nil
}

func (store *Store) ListMergeRequestsForRead(
	ctx context.Context,
	actor *User,
	repository Repository,
	state string,
) ([]MergeRequest, error) {
	if _, err := store.RepositoryForRead(ctx, actor, repository.Owner, repository.Slug); err != nil {
		return nil, err
	}
	if state != "open" && state != "closed" && state != "merged" {
		state = "open"
	}
	rows, err := store.pool.Query(ctx, `
		SELECT mr.id, mr.number, mr.title, mr.body, mr.state,
		       mr.source_branch, mr.target_branch, mr.source_revision, mr.target_revision,
		       author.username,
		       COUNT(review.id) FILTER (WHERE review.decision = 'approved'),
		       mr.created_at, mr.updated_at
		FROM merge_requests mr
		JOIN users author ON author.id = mr.author_id
		LEFT JOIN merge_request_reviews review ON review.merge_request_id = mr.id
		WHERE mr.repository_id = $1 AND mr.state = $2
		GROUP BY mr.id, author.username
		ORDER BY mr.updated_at DESC
		LIMIT 100
	`, repository.ID, state)
	if err != nil {
		return nil, fmt.Errorf("list authorized merge requests: %w", err)
	}
	defer rows.Close()
	mergeRequests := make([]MergeRequest, 0)
	for rows.Next() {
		var mergeRequest MergeRequest
		if err := rows.Scan(
			&mergeRequest.ID,
			&mergeRequest.Number,
			&mergeRequest.Title,
			&mergeRequest.Body,
			&mergeRequest.State,
			&mergeRequest.SourceBranch,
			&mergeRequest.TargetBranch,
			&mergeRequest.SourceRevision,
			&mergeRequest.TargetRevision,
			&mergeRequest.Author,
			&mergeRequest.ApprovalCount,
			&mergeRequest.CreatedAt,
			&mergeRequest.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan authorized merge request: %w", err)
		}
		mergeRequests = append(mergeRequests, mergeRequest)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate authorized merge requests: %w", err)
	}
	return mergeRequests, nil
}

func (store *Store) ListCIRunsForRead(
	ctx context.Context,
	actor *User,
	repository Repository,
) ([]CIRun, error) {
	if _, err := store.RepositoryForRead(ctx, actor, repository.Owner, repository.Slug); err != nil {
		return nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT run.id, run.run_number, run.event_name, run.branch, run.revision,
		       run.status, run.conclusion, run.queued_at, run.started_at, run.completed_at
		FROM ci_runs run
		WHERE run.repository_id = $1
		ORDER BY run.run_number DESC
		LIMIT 50
	`, repository.ID)
	if err != nil {
		return nil, fmt.Errorf("list authorized CI runs: %w", err)
	}
	defer rows.Close()
	runs := make([]CIRun, 0)
	for rows.Next() {
		var run CIRun
		if err := rows.Scan(
			&run.ID,
			&run.RunNumber,
			&run.EventName,
			&run.Branch,
			&run.Revision,
			&run.Status,
			&run.Conclusion,
			&run.QueuedAt,
			&run.StartedAt,
			&run.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("scan authorized CI run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate authorized CI runs: %w", err)
	}
	return runs, nil
}
