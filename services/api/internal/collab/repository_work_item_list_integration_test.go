package collab

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepositoryIssueListFiltersCountsAndPaginationPostgres(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "public", "write")
	labelID := uuidNew()
	milestoneID := uuidNew()
	mustExec(t, ctx, pool, `
		INSERT INTO labels (id, repository_id, name, description, color)
		VALUES ($1, $2, 'Bug', '', 'D73A4A')
	`, labelID, fixture.repoID)
	mustExec(t, ctx, pool, `
		INSERT INTO repository_milestones (
			id, repository_id, number, title, description, state, created_by
		) VALUES ($1, $2, 1, 'Release', '', 'open', $3)
	`, milestoneID, fixture.repoID, fixture.alice.ID)
	openID := seedRepositoryIssueListItem(
		t, ctx, fixture, "Renderer crash", "GPU details", "open", fixture.alice.ID,
	)
	closedID := seedRepositoryIssueListItem(
		t, ctx, fixture, "Renderer cleanup", "Resolved", "closed", fixture.bob.ID,
	)
	mustExec(t, ctx, pool, `
		UPDATE issues SET milestone_id = $2 WHERE id = $1
	`, openID, milestoneID)
	mustExec(t, ctx, pool, `
		INSERT INTO issue_labels (issue_id, label_id) VALUES ($1, $2)
	`, openID, labelID)
	mustExec(t, ctx, pool, `
		INSERT INTO issue_assignees (
			issue_id, repository_id, user_id, assigned_by
		) VALUES ($1, $2, $3, $4)
	`, openID, fixture.repoID, fixture.bob.ID, fixture.alice.ID)
	mustExec(t, ctx, pool, `
		INSERT INTO issue_comments (id, issue_id, author_id, body)
		VALUES ($1, $2, $3, 'first'), ($4, $2, $3, 'second')
	`, uuidNew(), openID, fixture.alice.ID, uuidNew())

	page, err := store.ListIssuesForRepository(ctx, fixture.repoID, RepositoryIssueQuery{
		State: "all", Search: "renderer", Author: fixture.alice.Username,
		Assignee: fixture.bob.Username, Labels: []string{"bug"},
		MilestoneNumber: pointerToInt64(1), Sort: "comments", Direction: "desc",
		Page: 1, PerPage: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalCount != 1 || page.OpenCount != 1 || page.ClosedCount != 0 ||
		len(page.Issues) != 1 || page.Issues[0].ID != openID || page.HasNext ||
		page.Issues[0].CommentCount != 2 || len(page.Issues[0].Labels) != 1 ||
		len(page.Issues[0].Assignees) != 1 || page.Issues[0].Milestone == nil {
		t.Fatalf("filtered issue page = %#v", page)
	}
	page, err = store.ListIssuesForRepository(ctx, fixture.repoID, RepositoryIssueQuery{
		State: "all", WithoutMilestone: true, Page: 1, PerPage: 1,
	})
	if err != nil || page.TotalCount != 1 || page.Issues[0].ID != closedID {
		t.Fatalf("issue without milestone page = %#v, err=%v", page, err)
	}
}

func TestRepositoryPullRequestListFiltersCountsAndMetadataPostgres(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "public", "write")
	labelID := uuidNew()
	milestoneID := uuidNew()
	mustExec(t, ctx, pool, `
		INSERT INTO labels (id, repository_id, name, description, color)
		VALUES ($1, $2, 'Ready', '', '0E8A16')
	`, labelID, fixture.repoID)
	mustExec(t, ctx, pool, `
		INSERT INTO repository_milestones (
			id, repository_id, number, title, description, state, created_by
		) VALUES ($1, $2, 1, 'Release', '', 'open', $3)
	`, milestoneID, fixture.repoID, fixture.alice.ID)
	draftID := seedRepositoryPullRequestListItem(
		t, ctx, fixture, "Renderer update", "GPU details", "open", true,
		"feature/render", "main", fixture.alice.ID,
	)
	seedRepositoryPullRequestListItem(
		t, ctx, fixture, "Old work", "", "merged", false,
		"feature/old", "main", fixture.bob.ID,
	)
	mustExec(t, ctx, pool, `
		UPDATE merge_requests SET milestone_id = $2 WHERE id = $1
	`, draftID, milestoneID)
	mustExec(t, ctx, pool, `
		INSERT INTO merge_request_labels (
			merge_request_id, repository_id, label_id, applied_by
		) VALUES ($1, $2, $3, $4)
	`, draftID, fixture.repoID, labelID, fixture.alice.ID)
	mustExec(t, ctx, pool, `
		INSERT INTO merge_request_assignees (
			merge_request_id, repository_id, user_id, assigned_by
		) VALUES ($1, $2, $3, $4)
	`, draftID, fixture.repoID, fixture.bob.ID, fixture.alice.ID)
	mustExec(t, ctx, pool, `
		INSERT INTO merge_request_comments (id, merge_request_id, author_id, body)
		VALUES ($1, $2, $3, 'ready')
	`, uuidNew(), draftID, fixture.alice.ID)
	draft := true
	page, err := store.ListMergeRequestsForRepository(ctx, fixture.repoID, RepositoryMergeRequestQuery{
		State: "all", Search: "renderer", Author: fixture.alice.Username,
		Assignee: fixture.bob.Username, Labels: []string{"ready"},
		MilestoneNumber: pointerToInt64(1), SourceBranch: "feature/render", TargetBranch: "main",
		Draft: &draft, Sort: "comments", Direction: "desc", Page: 1, PerPage: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalCount != 1 || page.OpenCount != 1 || page.ClosedCount != 0 ||
		page.MergedCount != 0 || len(page.MergeRequests) != 1 ||
		page.MergeRequests[0].ID != draftID || page.MergeRequests[0].CommentCount != 1 ||
		len(page.MergeRequests[0].Labels) != 1 || len(page.MergeRequests[0].Assignees) != 1 ||
		page.MergeRequests[0].Milestone == nil {
		t.Fatalf("filtered pull request page = %#v", page)
	}
}

func TestRepositoryWorkItemCommentCountersPostgres(t *testing.T) {
	pool, _ := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "public", "write")
	issueID := seedRepositoryIssueListItem(
		t, ctx, fixture, "Counter", "", "open", fixture.alice.ID,
	)
	requestID := seedRepositoryPullRequestListItem(
		t, ctx, fixture, "Counter", "", "open", false,
		"feature/counter", "main", fixture.alice.ID,
	)
	otherIssueID := seedRepositoryIssueListItem(
		t, ctx, fixture, "Other counter", "", "open", fixture.alice.ID,
	)
	otherRequestID := seedRepositoryPullRequestListItem(
		t, ctx, fixture, "Other counter", "", "open", false,
		"feature/other-counter", "main", fixture.alice.ID,
	)
	issueCommentID := uuidNew()
	requestCommentID := uuidNew()
	mustExec(t, ctx, pool, `
		INSERT INTO issue_comments (id, issue_id, author_id, body) VALUES ($1, $2, $3, 'one')
	`, issueCommentID, issueID, fixture.alice.ID)
	mustExec(t, ctx, pool, `
		INSERT INTO merge_request_comments (
			id, merge_request_id, author_id, body
		) VALUES ($1, $2, $3, 'one')
	`, requestCommentID, requestID, fixture.alice.ID)
	assertRepositoryWorkItemCommentCount(t, pool, "issues", issueID, 1)
	assertRepositoryWorkItemCommentCount(t, pool, "merge_requests", requestID, 1)
	mustExec(t, ctx, pool, `
		UPDATE issue_comments SET issue_id = $2 WHERE id = $1
	`, issueCommentID, otherIssueID)
	mustExec(t, ctx, pool, `
		UPDATE merge_request_comments SET merge_request_id = $2 WHERE id = $1
	`, requestCommentID, otherRequestID)
	assertRepositoryWorkItemCommentCount(t, pool, "issues", issueID, 0)
	assertRepositoryWorkItemCommentCount(t, pool, "issues", otherIssueID, 1)
	assertRepositoryWorkItemCommentCount(t, pool, "merge_requests", requestID, 0)
	assertRepositoryWorkItemCommentCount(t, pool, "merge_requests", otherRequestID, 1)
	mustExec(t, ctx, pool, `
		DELETE FROM issue_comments WHERE id = $1
	`, issueCommentID)
	mustExec(t, ctx, pool, `
		DELETE FROM merge_request_comments WHERE id = $1
	`, requestCommentID)
	assertRepositoryWorkItemCommentCount(t, pool, "issues", issueID, 0)
	assertRepositoryWorkItemCommentCount(t, pool, "merge_requests", requestID, 0)
	assertRepositoryWorkItemCommentCount(t, pool, "issues", otherIssueID, 0)
	assertRepositoryWorkItemCommentCount(t, pool, "merge_requests", otherRequestID, 0)
	mustExec(t, ctx, pool, `
		INSERT INTO issue_comments (id, issue_id, author_id, body) VALUES ($1, $2, $3, 'cascade')
	`, uuidNew(), issueID, fixture.alice.ID)
	mustExec(t, ctx, pool, `
		INSERT INTO merge_request_comments (
			id, merge_request_id, author_id, body
		) VALUES ($1, $2, $3, 'cascade')
	`, uuidNew(), requestID, fixture.alice.ID)
	mustExec(t, ctx, pool, "DELETE FROM repositories WHERE id = $1", fixture.repoID)
}

func seedRepositoryIssueListItem(
	t *testing.T,
	ctx context.Context,
	fixture integrationFixture,
	title string,
	body string,
	state string,
	authorID string,
) string {
	t.Helper()
	id := uuidNew()
	var number int64
	if err := fixture.pool.QueryRow(ctx, `
		UPDATE repository_counters SET next_issue_number = next_issue_number + 1
		WHERE repository_id = $1 RETURNING next_issue_number - 1
	`, fixture.repoID).Scan(&number); err != nil {
		t.Fatal(err)
	}
	mustExec(t, ctx, fixture.pool, `
		INSERT INTO issues (id, repository_id, number, title, body, state, author_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, id, fixture.repoID, number, title, body, state, authorID)
	return id
}

func seedRepositoryPullRequestListItem(
	t *testing.T,
	ctx context.Context,
	fixture integrationFixture,
	title string,
	body string,
	state string,
	draft bool,
	source string,
	target string,
	authorID string,
) string {
	t.Helper()
	id := uuidNew()
	var number int64
	if err := fixture.pool.QueryRow(ctx, `
		UPDATE repository_counters SET next_merge_request_number = next_merge_request_number + 1
		WHERE repository_id = $1 RETURNING next_merge_request_number - 1
	`, fixture.repoID).Scan(&number); err != nil {
		t.Fatal(err)
	}
	mustExec(t, ctx, fixture.pool, `
		INSERT INTO merge_requests (
			id, repository_id, number, title, body, state, is_draft,
			draft_changed_at, draft_changed_by,
			source_branch, target_branch, source_revision, target_revision, author_id
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			CASE WHEN $7 THEN now() ELSE NULL END,
			CASE WHEN $7 THEN $10::uuid ELSE NULL END,
			$8, $9, 'source-rev', 'target-rev', $10
		)
	`, id, fixture.repoID, number, title, body, state, draft, source, target, authorID)
	return id
}

func assertRepositoryWorkItemCommentCount(
	t *testing.T,
	pool *pgxpool.Pool,
	table string,
	id string,
	want int64,
) {
	t.Helper()
	var count int64
	if err := pool.QueryRow(
		context.Background(), "SELECT comment_count FROM "+table+" WHERE id = $1", id,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("%s comment count = %d, want %d", table, count, want)
	}
}
