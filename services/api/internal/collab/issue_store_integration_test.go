package collab

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func TestIntegrationIssueUpdatePermissionAndPrecondition(t *testing.T) {
	pool, s := integrationEnv(t)
	ctx := context.Background()
	fix := setupFixture(t, pool, "public", "triage")
	number := seedIssue(t, ctx, pool, fix, fix.carol.ID, "open")

	// Author can edit even without triage (carol has no repository role).
	_, err := s.UpdateIssue(ctx, fix.carol, fix.repoID, number, UpdateIssueInput{
		Title: ptrString("Edited by author"),
	})
	if err != nil {
		t.Fatalf("author edit: %v", err)
	}
	// Bob (triage) can close.
	closed, err := s.UpdateIssue(ctx, fix.bob, fix.repoID, number, UpdateIssueInput{State: ptrString("closed")})
	if err != nil {
		t.Fatalf("triage close: %v", err)
	}
	if closed.State != "closed" || closed.ClosedAt == nil {
		t.Fatalf("expected closed issue, got %+v", closed)
	}
	if closed.ClosedBy == nil || *closed.ClosedBy != fix.bob.Username {
		t.Fatalf("closed by = %v, want %q", closed.ClosedBy, fix.bob.Username)
	}
	reopened, err := s.UpdateIssue(ctx, fix.bob, fix.repoID, number, UpdateIssueInput{
		State: ptrString("open"),
	})
	if err != nil {
		t.Fatalf("reopen issue: %v", err)
	}
	if reopened.State != "open" || reopened.ClosedAt != nil || reopened.ClosedBy != nil {
		t.Fatalf("reopened issue retained close metadata: %+v", reopened)
	}
	// An authenticated public reader who is not the author cannot edit.
	stranger := platform.User{ID: uuidNew(), Username: "stranger-" + fix.orgID[:8]}
	mustExec(t, ctx, pool, `INSERT INTO users (id, username, display_name) VALUES ($1, $2, 'Stranger')`,
		stranger.ID, stranger.Username)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, stranger.ID)
	})
	_, err = s.UpdateIssue(ctx, stranger, fix.repoID, number, UpdateIssueInput{Title: ptrString("nope")})
	if !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("non-author read edit expected ErrForbidden, got %v", err)
	}
	// Optimistic concurrency: stale IfMatch fails.
	current, err := s.GetIssue(ctx, fix.repoID, number)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	stale := current.UpdatedAt.Add(-time.Minute)
	_, err = s.UpdateIssue(ctx, fix.carol, fix.repoID, number, UpdateIssueInput{
		Title: ptrString("stale"), IfMatch: &stale,
	})
	if !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("stale if-match expected ErrPreconditionFailed, got %v", err)
	}
}

func TestIntegrationIssueComments(t *testing.T) {
	pool, s := integrationEnv(t)
	ctx := context.Background()
	fix := setupFixture(t, pool, "public", "")
	number := seedIssue(t, ctx, pool, fix, fix.carol.ID, "open")

	comment, err := s.CreateIssueComment(ctx, fix.carol, fix.repoID, number, "first comment")
	if err != nil {
		t.Fatalf("create comment with read permission: %v", err)
	}
	if comment.Author != fix.carol.Username {
		t.Fatalf("comment author = %q, want %q", comment.Author, fix.carol.Username)
	}
	// Author can edit.
	edited, err := s.UpdateIssueComment(ctx, fix.carol, fix.repoID, number, comment.ID, "edited body")
	if err != nil {
		t.Fatalf("edit own comment: %v", err)
	}
	if edited.Body != "edited body" || edited.EditedAt == nil {
		t.Fatalf("edited comment = %+v", edited)
	}
	if _, err := s.UpdateIssueComment(ctx, fix.carol, fix.repoID, number, comment.ID, "edited again"); err != nil {
		t.Fatalf("second comment edit: %v", err)
	}
	other := setupFixture(t, pool, "public", "write")
	if _, err := s.UpdateIssueComment(
		ctx, fix.carol, other.repoID, number, comment.ID, "cross-repository",
	); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("cross-repository comment edit expected ErrNotFound, got %v", err)
	}
	if err := s.DeleteIssueComment(
		ctx, fix.carol, other.repoID, number, comment.ID,
	); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("cross-repository comment delete expected ErrNotFound, got %v", err)
	}
	// Bob (not author, read only) cannot edit.
	_, err = s.UpdateIssueComment(ctx, fix.bob, fix.repoID, number, comment.ID, "hacked")
	if !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("bob edit comment expected ErrForbidden, got %v", err)
	}
	// A repository administrator can moderate another user's comment.
	if _, err := s.UpdateIssueComment(ctx, fix.alice, fix.repoID, number, comment.ID, "moderated"); err != nil {
		t.Fatalf("admin edit comment: %v", err)
	}
	// Author can delete.
	if err := s.DeleteIssueComment(ctx, fix.carol, fix.repoID, number, comment.ID); err != nil {
		t.Fatalf("delete comment: %v", err)
	}
	if err := s.DeleteIssueComment(ctx, fix.carol, fix.repoID, number, comment.ID); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("second delete expected ErrNotFound, got %v", err)
	}
	if got := countTopic(t, ctx, pool, "issue_comment.updated"); got < 2 {
		t.Fatalf("comment updates outbox count = %d, want at least 2", got)
	}
	if got := countTopic(t, ctx, pool, "issue_comment.deleted"); got < 1 {
		t.Fatalf("comment delete outbox count = %d, want at least 1", got)
	}
}
