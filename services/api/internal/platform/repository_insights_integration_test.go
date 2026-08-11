package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRepositoryInsightsAuthorizationAggregationAndTenantBoundaryIntegration(t *testing.T) {
	fixture := authorizationIntegrationFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	recent := now.Add(-48 * time.Hour)
	seedRepositoryInsights(t, fixture, recent, now)

	insights, err := fixture.store.RepositoryInsights(ctx, &fixture.manager, fixture.orgSlug, "a", 7)
	if err != nil {
		t.Fatalf("read repository insights: %v", err)
	}
	if insights.PeriodDays != 7 || len(insights.Activity) != 7 {
		t.Fatalf("insights period = %d, days = %d", insights.PeriodDays, len(insights.Activity))
	}
	if insights.Current.OpenIssues != 1 || insights.Current.OpenPullRequests != 1 ||
		insights.Current.Branches != 1 || insights.Current.PublishedReleases != 1 {
		t.Fatalf("current insights = %+v", insights.Current)
	}
	wantPeriod := RepositoryInsightPeriod{
		IssuesOpened: 2, IssuesClosed: 1, PullRequestsOpened: 2, PullRequestsMerged: 1,
		WorkflowRunsCompleted: 2, WorkflowRunsSucceeded: 1, ReleasesPublished: 1, BranchPushes: 1,
	}
	if insights.Period != wantPeriod {
		t.Fatalf("period insights = %+v, want %+v", insights.Period, wantPeriod)
	}
	var activityCount int64
	for _, day := range insights.Activity {
		activityCount += day.ActivityCount()
	}
	if activityCount != 10 {
		t.Fatalf("activity count = %d, want 10", activityCount)
	}
	if len(insights.Contributors) != 2 || insights.Contributors[0].Username != fixture.manager.Username ||
		insights.Contributors[0].ActivityCount != 2 {
		t.Fatalf("contributors = %+v", insights.Contributors)
	}

	if _, err := fixture.store.RepositoryInsights(
		ctx, &fixture.bob, fixture.orgSlug, "a", 7,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unrelated private repository insights error = %v", err)
	}
	if _, err := fixture.store.RepositoryInsights(ctx, nil, fixture.orgSlug, "a", 7); !errors.Is(err, ErrNotFound) {
		t.Fatalf("anonymous private repository insights error = %v", err)
	}
	authorizationMustExec(t, fixture.pool, `UPDATE repositories SET visibility = 'public' WHERE id = $1`,
		fixture.repositoryA)
	publicInsights, err := fixture.store.RepositoryInsights(ctx, nil, fixture.orgSlug, "a", 90)
	if err != nil || publicInsights.PeriodDays != 90 || len(publicInsights.Activity) != 90 {
		t.Fatalf("public repository insights = %+v, err=%v", publicInsights, err)
	}
}

func seedRepositoryInsights(t *testing.T, fixture authorizationFixture, recent time.Time, now time.Time) {
	issueOpen := uuid.NewString()
	issueClosed := uuid.NewString()
	otherIssue := uuid.NewString()
	authorizationMustExec(t, fixture.pool, `
		INSERT INTO issues (
		    id, repository_id, number, title, state, author_id, closed_by, closed_at, created_at, updated_at
		) VALUES
		($1, $2, 1, 'Open insight issue', 'open', $3, NULL, NULL, $4, $4),
		($5, $2, 2, 'Closed insight issue', 'closed', $3, $3, $6, $4, $6),
		($7, $8, 1, 'Other repository issue', 'open', $3, NULL, NULL, $4, $4)
	`, issueOpen, fixture.repositoryA, fixture.manager.ID, recent, issueClosed, recent.Add(time.Hour),
		otherIssue, fixture.repositoryB)

	openRequest := uuid.NewString()
	mergedRequest := uuid.NewString()
	authorizationMustExec(t, fixture.pool, `
		INSERT INTO merge_requests (
		    id, repository_id, number, title, state, source_branch, target_branch,
		    source_revision, target_revision, author_id, merged_by, merged_revision,
		    merged_at, closed_at, created_at, updated_at
		) VALUES
		($1, $2, 1, 'Open insight request', 'open', 'feature', 'main', 'source-a', 'target-a', $3,
		 NULL, NULL, NULL, NULL, $4, $4),
		($5, $2, 2, 'Merged insight request', 'merged', 'merged', 'main', 'source-b', 'target-b', $3,
		 $3, 'merged-revision', $6, NULL, $4, $6)
	`, openRequest, fixture.repositoryA, fixture.manager.ID, recent, mergedRequest, recent.Add(2*time.Hour))

	authorizationMustExec(t, fixture.pool, `
		INSERT INTO ci_runs (
		    id, repository_id, run_number, event_name, branch, revision, actor_id,
		    status, conclusion, event_payload, queued_at, started_at, completed_at
		) VALUES
		($1, $2, 1, 'push', 'main', 'revision-a', $3, 'completed', 'success', '{}', $4, $4, $5),
		($6, $2, 2, 'push', 'main', 'revision-b', $3, 'completed', 'failure', '{}', $4, $4, $7)
	`, uuid.NewString(), fixture.repositoryA, fixture.manager.ID, recent, recent.Add(3*time.Hour),
		uuid.NewString(), recent.Add(4*time.Hour))

	authorizationMustExec(t, fixture.pool, `
		INSERT INTO repository_releases (
		    id, repository_id, tag_name, title, source_branch, revision, state,
		    created_by, published_by, published_at, created_at, updated_at
		) VALUES ($1, $2, 'v1.0.0', 'Insight release', 'main', $3, 'published', $4, $4, $5, $5, $5)
	`, uuid.NewString(), fixture.repositoryA, strings.Repeat("a", 64), fixture.manager.ID,
		recent.Add(5*time.Hour))
	authorizationMustExec(t, fixture.pool, `
		INSERT INTO repository_branch_states (repository_id, branch_id, branch_name, latest_revision, observed_at)
		VALUES ($1, 'branch-a', 'main', 'revision-a', $2)
	`, fixture.repositoryA, recent)

	authorizationMustExec(t, fixture.pool, `
		INSERT INTO audit_events (
		    id, organization_id, repository_id, actor_id, action, target_type, target_id, occurred_at
		) VALUES
		($1, $2, $3, $4, 'branch.push', 'lore_branch', 'branch-a', $5),
		($6, $2, $3, $4, 'issue.create', 'issue', $7, $5),
		($8, $2, $3, $9, 'issue_comment.create', 'issue_comment', $10, $5),
		($11, $2, $12, $4, 'branch.push', 'lore_branch', 'branch-b', $5),
		($13, $2, $3, $4, 'branch.push', 'lore_branch', 'old', $14)
	`, uuid.NewString(), fixture.orgID, fixture.repositoryA, fixture.manager.ID, recent.Add(6*time.Hour),
		uuid.NewString(), issueOpen, uuid.NewString(), fixture.alice.ID, uuid.NewString(), uuid.NewString(),
		fixture.repositoryB, uuid.NewString(), now.Add(-120*24*time.Hour))
}
