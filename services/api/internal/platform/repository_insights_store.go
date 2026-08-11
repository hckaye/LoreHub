package platform

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	defaultInsightsPeriodDays = 30
	maxInsightsContributors   = 10
)

func (store *Store) RepositoryInsights(
	ctx context.Context,
	actor *User,
	owner string,
	repositorySlug string,
	periodDays int,
) (RepositoryInsights, error) {
	transaction, err := store.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return RepositoryInsights{}, fmt.Errorf("begin repository insights: %w", err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	viewerID := ""
	if actor != nil {
		viewerID = actor.ID
	}
	var repositoryID string
	err = transaction.QueryRow(ctx, `
		SELECT repository.id::text
		FROM repositories repository
		JOIN organizations organization ON organization.id = repository.organization_id
		WHERE organization.slug = $1 AND repository.slug = $2
		  AND `+repositoryAccessClause("repository", "$3")+`
	`, owner, repositorySlug, viewerID).Scan(&repositoryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return RepositoryInsights{}, ErrNotFound
	}
	if err != nil {
		return RepositoryInsights{}, fmt.Errorf("authorize repository insights: %w", err)
	}
	if !validInsightsPeriod(periodDays) {
		periodDays = defaultInsightsPeriodDays
	}
	now := time.Now().UTC()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).
		AddDate(0, 0, -(periodDays - 1))
	result := RepositoryInsights{
		PeriodDays: periodDays, PeriodStart: start, PeriodEnd: now,
		Activity:     make([]RepositoryInsightDay, periodDays),
		Contributors: make([]RepositoryInsightContributor, 0),
	}
	for index := range result.Activity {
		result.Activity[index].Date = start.AddDate(0, 0, index).Format(time.DateOnly)
	}
	if err := readRepositoryInsightCurrent(ctx, transaction, repositoryID, &result.Current); err != nil {
		return RepositoryInsights{}, err
	}
	if err := readRepositoryInsightActivity(ctx, transaction, repositoryID, start, now, &result); err != nil {
		return RepositoryInsights{}, err
	}
	contributors, err := readRepositoryInsightContributors(ctx, transaction, repositoryID, start, now)
	if err != nil {
		return RepositoryInsights{}, err
	}
	result.Contributors = contributors
	if err := transaction.Commit(ctx); err != nil {
		return RepositoryInsights{}, fmt.Errorf("commit repository insights: %w", err)
	}
	return result, nil
}

func validInsightsPeriod(days int) bool {
	return days == 7 || days == 30 || days == 90
}

func readRepositoryInsightCurrent(
	ctx context.Context,
	transaction pgx.Tx,
	repositoryID string,
	current *RepositoryInsightCurrent,
) error {
	err := transaction.QueryRow(ctx, `
		SELECT
		    (SELECT count(*) FROM issues WHERE repository_id = $1 AND state = 'open'),
		    (SELECT count(*) FROM merge_requests WHERE repository_id = $1 AND state = 'open'),
		    (SELECT count(*) FROM repository_branch_states WHERE repository_id = $1),
		    (SELECT count(*) FROM repository_releases WHERE repository_id = $1 AND state = 'published')
	`, repositoryID).Scan(
		&current.OpenIssues,
		&current.OpenPullRequests,
		&current.Branches,
		&current.PublishedReleases,
	)
	if err != nil {
		return fmt.Errorf("read repository insight totals: %w", err)
	}
	return nil
}

func readRepositoryInsightActivity(
	ctx context.Context,
	transaction pgx.Tx,
	repositoryID string,
	start time.Time,
	end time.Time,
	result *RepositoryInsights,
) error {
	rows, err := transaction.Query(ctx, `
		WITH activity AS (
		    SELECT created_at AS occurred_at, 'issue_opened' AS kind
		    FROM issues WHERE repository_id = $1 AND created_at >= $2 AND created_at < $3
		    UNION ALL
		    SELECT closed_at, 'issue_closed'
		    FROM issues WHERE repository_id = $1 AND closed_at >= $2 AND closed_at < $3
		    UNION ALL
		    SELECT created_at, 'pull_request_opened'
		    FROM merge_requests WHERE repository_id = $1 AND created_at >= $2 AND created_at < $3
		    UNION ALL
		    SELECT merged_at, 'pull_request_merged'
		    FROM merge_requests WHERE repository_id = $1 AND merged_at >= $2 AND merged_at < $3
		    UNION ALL
		    SELECT completed_at,
		           CASE WHEN conclusion = 'success' THEN 'workflow_succeeded' ELSE 'workflow_completed' END
		    FROM ci_runs WHERE repository_id = $1 AND completed_at >= $2 AND completed_at < $3
		    UNION ALL
		    SELECT published_at, 'release_published'
		    FROM repository_releases WHERE repository_id = $1 AND published_at >= $2 AND published_at < $3
		    UNION ALL
		    SELECT occurred_at, 'branch_push'
		    FROM audit_events
		    WHERE repository_id = $1 AND action = 'branch.push' AND occurred_at >= $2 AND occurred_at < $3
		)
		SELECT (occurred_at AT TIME ZONE 'UTC')::date,
		       count(*) FILTER (WHERE kind = 'issue_opened'),
		       count(*) FILTER (WHERE kind = 'issue_closed'),
		       count(*) FILTER (WHERE kind = 'pull_request_opened'),
		       count(*) FILTER (WHERE kind = 'pull_request_merged'),
		       count(*) FILTER (WHERE kind IN ('workflow_succeeded', 'workflow_completed')),
		       count(*) FILTER (WHERE kind = 'workflow_succeeded'),
		       count(*) FILTER (WHERE kind = 'release_published'),
		       count(*) FILTER (WHERE kind = 'branch_push')
		FROM activity
		GROUP BY (occurred_at AT TIME ZONE 'UTC')::date
		ORDER BY (occurred_at AT TIME ZONE 'UTC')::date
	`, repositoryID, start, end)
	if err != nil {
		return fmt.Errorf("read repository insight activity: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		if err := scanRepositoryInsightDay(rows, start, result); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate repository insight activity: %w", err)
	}
	return nil
}

func scanRepositoryInsightDay(rows pgx.Rows, start time.Time, result *RepositoryInsights) error {
	var date time.Time
	var day RepositoryInsightDay
	var succeeded int64
	if err := rows.Scan(
		&date,
		&day.IssuesOpened,
		&day.IssuesClosed,
		&day.PullRequestsOpened,
		&day.PullRequestsMerged,
		&day.WorkflowRunsCompleted,
		&succeeded,
		&day.ReleasesPublished,
		&day.BranchPushes,
	); err != nil {
		return fmt.Errorf("scan repository insight activity: %w", err)
	}
	index := int(date.Sub(start).Hours() / 24)
	if index < 0 || index >= len(result.Activity) {
		return fmt.Errorf("repository insight activity date %s is outside its period", date.Format(time.DateOnly))
	}
	day.Date = date.Format(time.DateOnly)
	result.Activity[index] = day
	result.Period.IssuesOpened += day.IssuesOpened
	result.Period.IssuesClosed += day.IssuesClosed
	result.Period.PullRequestsOpened += day.PullRequestsOpened
	result.Period.PullRequestsMerged += day.PullRequestsMerged
	result.Period.WorkflowRunsCompleted += day.WorkflowRunsCompleted
	result.Period.WorkflowRunsSucceeded += succeeded
	result.Period.ReleasesPublished += day.ReleasesPublished
	result.Period.BranchPushes += day.BranchPushes
	return nil
}

func readRepositoryInsightContributors(
	ctx context.Context,
	transaction pgx.Tx,
	repositoryID string,
	start time.Time,
	end time.Time,
) ([]RepositoryInsightContributor, error) {
	rows, err := transaction.Query(ctx, `
		SELECT COALESCE(event.actor_id::text, ''),
		       COALESCE(event.actor_username, actor.username, ''),
		       COALESCE(event.actor_display_name, actor.display_name, ''),
		       count(*), max(event.occurred_at)
		FROM audit_events event
		LEFT JOIN users actor ON actor.id = event.actor_id
		WHERE event.repository_id = $1
		  AND event.occurred_at >= $2 AND event.occurred_at < $3
		  AND COALESCE(event.actor_username, actor.username, '') <> ''
		  AND (
		      event.action LIKE 'issue.%' OR event.action LIKE 'issue_comment.%'
		      OR event.action LIKE 'merge_request.%' OR event.action LIKE 'merge_request_comment.%'
		      OR event.action LIKE 'merge_request_review.%' OR event.action LIKE 'branch.%'
		      OR event.action LIKE 'release.%'
		  )
		GROUP BY event.actor_id,
		         COALESCE(event.actor_username, actor.username, ''),
		         COALESCE(event.actor_display_name, actor.display_name, '')
		ORDER BY count(*) DESC, max(event.occurred_at) DESC,
		         COALESCE(event.actor_username, actor.username, '')
		LIMIT $4
	`, repositoryID, start, end, maxInsightsContributors)
	if err != nil {
		return nil, fmt.Errorf("read repository insight contributors: %w", err)
	}
	defer rows.Close()
	contributors := make([]RepositoryInsightContributor, 0)
	for rows.Next() {
		var contributor RepositoryInsightContributor
		if err := rows.Scan(
			&contributor.ID,
			&contributor.Username,
			&contributor.DisplayName,
			&contributor.ActivityCount,
			&contributor.LastActiveAt,
		); err != nil {
			return nil, fmt.Errorf("scan repository insight contributor: %w", err)
		}
		contributors = append(contributors, contributor)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repository insight contributors: %w", err)
	}
	return contributors, nil
}
