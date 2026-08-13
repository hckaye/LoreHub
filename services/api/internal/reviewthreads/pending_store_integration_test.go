package reviewthreads

import (
	"context"
	"errors"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func TestPostgresPendingReviewHidesCommentsUntilSubmit(t *testing.T) {
	fixture := openReviewFixture(t)
	ctx := context.Background()
	store := NewStore(fixture.pool)
	pending, created, err := store.StartPendingReview(ctx, fixture.writer, fixture.repository, 1)
	if err != nil || !created || pending.Author != fixture.writer.Username {
		t.Fatalf("start pending review = %+v, created = %v, error = %v", pending, created, err)
	}
	again, created, err := store.StartPendingReview(ctx, fixture.writer, fixture.repository, 1)
	if err != nil || created || again.ID != pending.ID {
		t.Fatalf("second start = %+v, created = %v, error = %v", again, created, err)
	}
	published, err := store.Create(ctx, fixture.author, fixture.repository, 1, reviewInput("Ship it?"))
	if err != nil {
		t.Fatalf("create published thread: %v", err)
	}
	batched, err := store.Create(ctx, fixture.writer, fixture.repository, 1, pendingInput(pending.ID))
	if err != nil {
		t.Fatalf("create batched thread: %v", err)
	}
	if _, err := store.Reply(
		ctx, fixture.writer, fixture.repository, 1, published.ID, "Batched reply", pending.ID,
	); err != nil {
		t.Fatalf("batched reply: %v", err)
	}
	assertThreadComments(t, store, fixture, fixture.author.Username, map[string]int{published.ID: 1})
	assertThreadComments(t, store, fixture, fixture.writer.Username,
		map[string]int{published.ID: 2, batched.ID: 1})
	assertThreadComments(t, store, fixture, "", map[string]int{published.ID: 1})

	result, err := store.SubmitPendingReview(ctx, fixture.writer, fixture.repository, 1, SubmitInput{
		Decision: "approved",
	})
	if err != nil || result.PublishedComments != 2 || result.ReviewID == "" {
		t.Fatalf("submit pending review = %+v, error = %v", result, err)
	}
	assertThreadComments(t, store, fixture, fixture.author.Username,
		map[string]int{published.ID: 2, batched.ID: 1})
	assertReviewDecision(t, fixture, result.ReviewID, "approved")
	assertPendingReviewGone(t, store, fixture, fixture.writer.Username)
	threads, err := store.List(ctx, fixture.repoID, 1, fixture.author.Username)
	if err != nil {
		t.Fatalf("list published threads: %v", err)
	}
	for _, thread := range threads {
		for _, comment := range thread.Comments {
			if comment.Pending {
				t.Fatalf("comment %s is still pending after submit", comment.ID)
			}
		}
	}
	if _, err := store.Reply(
		ctx, fixture.writer, fixture.repository, 1, published.ID, "Late", pending.ID,
	); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("reply to submitted review error = %v, want not found", err)
	}
}

func TestPostgresPendingReviewSubmitKeepsCommentsWhenRejected(t *testing.T) {
	fixture := openReviewFixture(t)
	ctx := context.Background()
	store := NewStore(fixture.pool)
	pending, _, err := store.StartPendingReview(ctx, fixture.author, fixture.repository, 1)
	if err != nil {
		t.Fatalf("start pending review: %v", err)
	}
	thread, err := store.Create(ctx, fixture.author, fixture.repository, 1, pendingInput(pending.ID))
	if err != nil {
		t.Fatalf("create batched thread: %v", err)
	}
	// The pull request author cannot approve their own pull request, so the
	// whole submit is rolled back and the comment stays hidden.
	if _, err := store.SubmitPendingReview(ctx, fixture.author, fixture.repository, 1, SubmitInput{
		Decision: "approved",
	}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("self approval error = %v, want forbidden", err)
	}
	assertThreadComments(t, store, fixture, fixture.writer.Username, map[string]int{})
	current, err := store.PendingReview(ctx, fixture.repoID, 1, fixture.author.Username)
	if err != nil || current == nil || current.CommentCount != 1 {
		t.Fatalf("pending review after rejected submit = %+v, error = %v", current, err)
	}
	result, err := store.SubmitPendingReview(ctx, fixture.author, fixture.repository, 1, SubmitInput{
		Decision: "commented",
	})
	if err != nil || result.PublishedComments != 1 || result.ReviewID != "" {
		t.Fatalf("author submit = %+v, error = %v", result, err)
	}
	assertThreadComments(t, store, fixture, fixture.writer.Username, map[string]int{thread.ID: 1})
	assertPendingReviewGone(t, store, fixture, fixture.author.Username)
}

func TestPostgresPendingReviewDiscardRemovesBatchedComments(t *testing.T) {
	fixture := openReviewFixture(t)
	ctx := context.Background()
	store := NewStore(fixture.pool)
	published, err := store.Create(ctx, fixture.author, fixture.repository, 1, reviewInput("Please review"))
	if err != nil {
		t.Fatalf("create published thread: %v", err)
	}
	pending, _, err := store.StartPendingReview(ctx, fixture.writer, fixture.repository, 1)
	if err != nil {
		t.Fatalf("start pending review: %v", err)
	}
	batched, err := store.Create(ctx, fixture.writer, fixture.repository, 1, pendingInput(pending.ID))
	if err != nil {
		t.Fatalf("create batched thread: %v", err)
	}
	if _, err := store.Reply(
		ctx, fixture.writer, fixture.repository, 1, published.ID, "Batched reply", pending.ID,
	); err != nil {
		t.Fatalf("batched reply: %v", err)
	}
	if err := store.DiscardPendingReview(ctx, fixture.writer, fixture.repository, 1); err != nil {
		t.Fatalf("discard pending review: %v", err)
	}
	assertThreadComments(t, store, fixture, fixture.writer.Username, map[string]int{published.ID: 1})
	assertPendingReviewGone(t, store, fixture, fixture.writer.Username)
	var threadCount int
	if err := fixture.pool.QueryRow(ctx, `
		SELECT count(*) FROM merge_request_review_threads WHERE id = $1
	`, batched.ID).Scan(&threadCount); err != nil {
		t.Fatalf("count discarded thread: %v", err)
	}
	if threadCount != 0 {
		t.Fatalf("discarded thread count = %d, want 0", threadCount)
	}
	if err := store.DiscardPendingReview(
		ctx, fixture.writer, fixture.repository, 1,
	); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("repeated discard error = %v, want not found", err)
	}
}

func reviewInput(body string) CreateInput {
	return CreateInput{
		Path: "src/main.go", Side: SideRight, LineNumber: 12, LineContent: "return start()",
		Body: body, ExpectedBaseRevision: "base-revision", ExpectedHeadRevision: "head-revision",
	}
}

func pendingInput(pendingReviewID string) CreateInput {
	input := reviewInput("Batched comment")
	input.LineNumber = 13
	input.PendingReviewID = pendingReviewID
	return input
}

func assertThreadComments(
	t *testing.T,
	store Store,
	fixture reviewFixture,
	viewer string,
	want map[string]int,
) {
	t.Helper()
	threads, err := store.List(context.Background(), fixture.repoID, 1, viewer)
	if err != nil {
		t.Fatalf("list review threads for %q: %v", viewer, err)
	}
	if len(threads) != len(want) {
		t.Fatalf("threads visible to %q = %d, want %d", viewer, len(threads), len(want))
	}
	for _, thread := range threads {
		count, ok := want[thread.ID]
		if !ok {
			t.Fatalf("thread %s must not be visible to %q", thread.ID, viewer)
		}
		if len(thread.Comments) != count {
			t.Fatalf("comments of %s visible to %q = %d, want %d",
				thread.ID, viewer, len(thread.Comments), count)
		}
	}
}

func assertReviewDecision(t *testing.T, fixture reviewFixture, reviewID string, want string) {
	t.Helper()
	var decision, revision string
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT decision, source_revision FROM merge_request_reviews WHERE id = $1
	`, reviewID).Scan(&decision, &revision); err != nil {
		t.Fatalf("read submitted review: %v", err)
	}
	if decision != want || revision != "head-revision" {
		t.Fatalf("submitted review decision = %q, revision = %q", decision, revision)
	}
}

func assertPendingReviewGone(t *testing.T, store Store, fixture reviewFixture, author string) {
	t.Helper()
	pending, err := store.PendingReview(context.Background(), fixture.repoID, 1, author)
	if err != nil {
		t.Fatalf("read pending review of %q: %v", author, err)
	}
	if pending != nil {
		t.Fatalf("pending review of %q still exists: %+v", author, pending)
	}
}
