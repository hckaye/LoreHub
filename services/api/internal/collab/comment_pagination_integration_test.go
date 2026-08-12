package collab

import (
	"context"
	"errors"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func TestRepositoryCommentPaginationPostgres(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "public", "write")
	issueID := seedRepositoryIssueListItem(
		t, ctx, fixture, "Paged issue", "", "open", fixture.alice.ID,
	)
	requestID := seedRepositoryPullRequestListItem(
		t, ctx, fixture, "Paged pull request", "", "open", false,
		"feature/comments", "main", fixture.alice.ID,
	)
	for index := range 3 {
		mustExec(t, ctx, pool, `
			INSERT INTO issue_comments (id, issue_id, author_id, body, created_at)
			VALUES ($1, $2, $3, $4, now() + ($5 * interval '1 second'))
		`, uuidNew(), issueID, fixture.alice.ID, "issue comment", index)
	}
	for index := range 2 {
		mustExec(t, ctx, pool, `
			INSERT INTO merge_request_comments (
				id, merge_request_id, author_id, body, created_at
			) VALUES ($1, $2, $3, $4, now() + ($5 * interval '1 second'))
		`, uuidNew(), requestID, fixture.alice.ID, "pull request comment", index)
	}

	issueFirst, err := store.ListIssueComments(ctx, fixture.repoID, 1, Page{Limit: 2})
	if err != nil || issueFirst.TotalCount == nil || *issueFirst.TotalCount != 3 ||
		len(issueFirst.Items) != 2 || !issueFirst.HasMore || issueFirst.NextCursor != "2" {
		t.Fatalf("first issue comment page = %#v, err=%v", issueFirst, err)
	}
	issueLast, err := store.ListIssueComments(ctx, fixture.repoID, 1, Page{Limit: 2, Cursor: "2"})
	if err != nil || issueLast.TotalCount == nil || *issueLast.TotalCount != 3 ||
		len(issueLast.Items) != 1 || issueLast.HasMore || issueLast.NextCursor != "" {
		t.Fatalf("last issue comment page = %#v, err=%v", issueLast, err)
	}
	pullFirst, err := store.ListMergeRequestComments(ctx, fixture.repoID, 1, Page{Limit: 1})
	if err != nil || pullFirst.TotalCount == nil || *pullFirst.TotalCount != 2 ||
		len(pullFirst.Items) != 1 || !pullFirst.HasMore || pullFirst.NextCursor != "1" {
		t.Fatalf("first pull request comment page = %#v, err=%v", pullFirst, err)
	}

	mustExec(t, ctx, pool, "UPDATE organizations SET active = false WHERE id = $1", fixture.orgID)
	if _, err := store.ListIssueComments(ctx, fixture.repoID, 1, Page{}); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("inactive organization issue comments error = %v", err)
	}
	if _, err := store.ListMergeRequestComments(
		ctx, fixture.repoID, 1, Page{},
	); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("inactive organization pull request comments error = %v", err)
	}
}
