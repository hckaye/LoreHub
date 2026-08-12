package collab

import (
	"context"
	"errors"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func TestIntegrationMergeRequestCommentsAndAuthorMutation(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "private", "read")
	mergeRequestID := uuidNew()
	mustExec(t, ctx, pool, `
		INSERT INTO merge_requests (
			id, repository_id, number, title, body, state,
			source_branch, target_branch, source_revision, target_revision, author_id
		) VALUES ($1, $2, 1, 'Conversation', '', 'open', 'feature', 'main', 'source', 'target', $3)
	`, mergeRequestID, fixture.repoID, fixture.bob.ID)

	comment, err := store.CreateMergeRequestComment(ctx, fixture.bob, fixture.repoID, 1, "First comment")
	if err != nil {
		t.Fatalf("reader create comment: %v", err)
	}
	if comment.Author != fixture.bob.Username || comment.Body != "First comment" {
		t.Fatalf("created comment = %+v", comment)
	}
	if _, err := store.CreateMergeRequestComment(
		ctx, fixture.carol, fixture.repoID, 1, "Hidden reader",
	); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("unauthorized comment error = %v, want forbidden", err)
	}
	updated, err := store.UpdateMergeRequestComment(
		ctx, fixture.bob, fixture.repoID, 1, comment.ID, "Edited comment",
	)
	if err != nil {
		t.Fatalf("author edit comment: %v", err)
	}
	if updated.EditedAt == nil || updated.Body != "Edited comment" {
		t.Fatalf("updated comment = %+v", updated)
	}
	comments, err := store.ListMergeRequestComments(ctx, fixture.repoID, 1, Page{Limit: 100})
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if comments.TotalCount == nil || *comments.TotalCount != 1 ||
		len(comments.Items) != 1 || comments.Items[0].Body != "Edited comment" {
		t.Fatalf("comments = %+v", comments.Items)
	}

	newTitle := "Author edited title"
	request, err := store.UpdateMergeRequest(ctx, fixture.bob, fixture.repoID, 1,
		UpdateMergeRequestInput{Title: &newTitle})
	if err != nil {
		t.Fatalf("author update pull request: %v", err)
	}
	if request.Title != newTitle {
		t.Fatalf("updated pull request = %+v", request)
	}
	if err := store.DeleteMergeRequestComment(ctx, fixture.alice, fixture.repoID, 1, comment.ID); err != nil {
		t.Fatalf("organization owner delete comment: %v", err)
	}
	comments, err = store.ListMergeRequestComments(ctx, fixture.repoID, 1, Page{Limit: 100})
	if err != nil || comments.TotalCount == nil || *comments.TotalCount != 0 || len(comments.Items) != 0 {
		t.Fatalf("comments after delete = %+v, error = %v", comments.Items, err)
	}
	var auditCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM audit_events
		WHERE repository_id = $1 AND action LIKE 'merge_request_comment.%'
	`, fixture.repoID).Scan(&auditCount); err != nil {
		t.Fatalf("count comment audit events: %v", err)
	}
	if auditCount != 3 {
		t.Fatalf("audit count = %d, want 3", auditCount)
	}
}
