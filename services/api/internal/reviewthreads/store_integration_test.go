package reviewthreads

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/database"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type reviewFixture struct {
	pool       *pgxpool.Pool
	repository RepositoryRef
	repoID     string
	requestID  string
	orgID      string
	author     platform.User
	writer     platform.User
	outsider   platform.User
	suspended  platform.User
}

func TestPostgresReviewThreadLifecyclePermissionsAndAudit(t *testing.T) {
	fixture := openReviewFixture(t)
	ctx := context.Background()
	store := NewStore(fixture.pool)
	input := CreateInput{
		Path: "src/main.go", Side: SideRight, LineNumber: 12, LineContent: "return start()",
		Body: "Please handle the error.", ExpectedBaseRevision: "base-revision",
		ExpectedHeadRevision: "head-revision",
	}
	if _, err := store.Create(ctx, fixture.outsider, fixture.repository, 1, input); !errors.Is(
		err, platform.ErrForbidden,
	) {
		t.Fatalf("outsider create error = %v, want forbidden", err)
	}
	if _, err := store.Create(ctx, fixture.suspended, fixture.repository, 1, input); !errors.Is(
		err, platform.ErrNotFound,
	) {
		t.Fatalf("suspended create error = %v, want not found", err)
	}
	if _, err := store.Create(ctx, fixture.author, fixture.repository, 1, CreateInput{
		Path: "src/main.go", Side: SideRight, LineNumber: 12, LineContent: "return start()",
		Body: "Stale", ExpectedBaseRevision: "old-base", ExpectedHeadRevision: "head-revision",
	}); !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("stale revision error = %v, want conflict", err)
	}
	thread, err := store.Create(ctx, fixture.author, fixture.repository, 1, input)
	if err != nil {
		t.Fatalf("create review thread: %v", err)
	}
	if len(thread.Comments) != 1 || thread.Comments[0].Body != input.Body || thread.Version != 1 {
		t.Fatalf("created thread = %+v", thread)
	}
	reply, err := store.Reply(ctx, fixture.writer, fixture.repository, 1, thread.ID, "I will update it.", "")
	if err != nil {
		t.Fatalf("reply to review thread: %v", err)
	}
	updated, err := store.UpdateComment(
		ctx, fixture.author, fixture.repository, 1, thread.ID,
		thread.Comments[0].ID, "Please return the error.", 1,
	)
	if err != nil || updated.Version != 2 || updated.EditedAt == nil {
		t.Fatalf("update review comment = %+v, error = %v", updated, err)
	}
	if _, err := store.UpdateComment(
		ctx, fixture.author, fixture.repository, 1, thread.ID,
		thread.Comments[0].ID, "Stale edit", 1,
	); !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("stale comment update error = %v, want conflict", err)
	}
	resolved, err := store.SetResolved(ctx, fixture.author, fixture.repository, 1, thread.ID, true, 1)
	if err != nil || !resolved.Resolved || resolved.Version != 2 || resolved.ResolvedAt == nil {
		t.Fatalf("resolved thread = %+v, error = %v", resolved, err)
	}
	if err := store.DeleteComment(
		ctx, fixture.writer, fixture.repository, 1, thread.ID, reply.ID, 1,
	); err != nil {
		t.Fatalf("writer delete reply: %v", err)
	}
	threads, err := store.List(ctx, fixture.repoID, 1, fixture.author.Username)
	if err != nil || len(threads) != 1 || len(threads[0].Comments) != 2 {
		t.Fatalf("list review threads = %+v, error = %v", threads, err)
	}
	if !threads[0].Resolved || !threads[0].Comments[1].Deleted || threads[0].Comments[1].Body != "" {
		t.Fatalf("stored review thread = %+v", threads[0])
	}
	assertReviewEvents(t, fixture, thread.ID)
	assertReviewTenantConstraint(t, fixture, thread.ID)
}

func TestPostgresReviewThreadResolutionUsesOptimisticVersion(t *testing.T) {
	fixture := openReviewFixture(t)
	ctx := context.Background()
	store := NewStore(fixture.pool)
	thread, err := store.Create(ctx, fixture.author, fixture.repository, 1, CreateInput{
		Path: "README.md", Side: SideLeft, LineNumber: 1, LineContent: "Old title",
		Body: "Keep the title.", ExpectedBaseRevision: "base-revision",
		ExpectedHeadRevision: "head-revision",
	})
	if err != nil {
		t.Fatalf("create concurrent review thread: %v", err)
	}
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			ready.Done()
			<-start
			_, updateErr := store.SetResolved(
				ctx, fixture.author, fixture.repository, 1, thread.ID, true, thread.Version,
			)
			results <- updateErr
		}()
	}
	ready.Wait()
	close(start)
	var successes, conflicts int
	for range 2 {
		switch err := <-results; {
		case err == nil:
			successes++
		case errors.Is(err, platform.ErrConflict):
			conflicts++
		default:
			t.Fatalf("concurrent resolution error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes = %d, conflicts = %d", successes, conflicts)
	}
}

func openReviewFixture(t *testing.T) reviewFixture {
	t.Helper()
	databaseURL := os.Getenv("LOREHUB_TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("LOREHUB_TEST_DATABASE_URL or DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, databaseURL, 5*time.Second)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := database.Migrate(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("migrate PostgreSQL: %v", err)
	}
	t.Cleanup(pool.Close)
	fixture := seedReviewFixture(t, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, fixture.orgID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id IN ($1, $2, $3, $4)`,
			fixture.author.ID, fixture.writer.ID, fixture.outsider.ID, fixture.suspended.ID)
	})
	return fixture
}

func seedReviewFixture(t *testing.T, pool *pgxpool.Pool) reviewFixture {
	t.Helper()
	orgID := uuid.NewString()
	repoID := uuid.NewString()
	requestID := uuid.NewString()
	author := platform.User{ID: uuid.NewString(), Username: "review-author-" + orgID[:8]}
	writer := platform.User{ID: uuid.NewString(), Username: "review-writer-" + orgID[:8]}
	outsider := platform.User{ID: uuid.NewString(), Username: "review-outsider-" + orgID[:8]}
	suspended := platform.User{ID: uuid.NewString(), Username: "review-suspended-" + orgID[:8]}
	mustReviewExec(t, pool, `
		INSERT INTO users (id, username, display_name, status) VALUES
		($1, $2, 'Author', 'active'), ($3, $4, 'Writer', 'active'),
		($5, $6, 'Outsider', 'active'), ($7, $8, 'Suspended', 'suspended')
	`, author.ID, author.Username, writer.ID, writer.Username,
		outsider.ID, outsider.Username, suspended.ID, suspended.Username)
	mustReviewExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, description, visibility, created_by)
		VALUES ($1, $2, 'Review Org', '', 'private', $3)
	`, orgID, "review-org-"+orgID[:8], writer.ID)
	mustReviewExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'member'), ($1, $3, 'member'), ($1, $4, 'member'), ($1, $5, 'member')
	`, orgID, author.ID, writer.ID, outsider.ID, suspended.ID)
	mustReviewExec(t, pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, description, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, 'game', 'Game', '', 'private', $3, $4, 'main', $5)
	`, repoID, orgID, strings.ReplaceAll(repoID, "-", ""), "https://lore.invalid/"+repoID, writer.ID)
	mustReviewExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role)
		VALUES ($1, $2, 'read'), ($1, $3, 'write'), ($1, $4, 'write')
	`, repoID, author.ID, writer.ID, suspended.ID)
	mustReviewExec(t, pool, `
		INSERT INTO merge_requests (
			id, repository_id, number, title, source_branch, target_branch,
			source_revision, target_revision, author_id
		) VALUES ($1, $2, 1, 'Review changes', 'feature', 'main', 'head-revision', 'base-revision', $3)
	`, requestID, repoID, author.ID)
	return reviewFixture{
		pool: pool, repository: RepositoryRef{ID: repoID, OrganizationID: orgID},
		repoID: repoID, requestID: requestID, orgID: orgID,
		author: author, writer: writer, outsider: outsider, suspended: suspended,
	}
}

func assertReviewEvents(t *testing.T, fixture reviewFixture, threadID string) {
	t.Helper()
	var auditCount, outboxCount int
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM audit_events
		WHERE repository_id = $1 AND action LIKE 'merge_request_review_%'
	`, fixture.repoID).Scan(&auditCount); err != nil {
		t.Fatalf("count review audit events: %v", err)
	}
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM outbox_events
		WHERE topic LIKE 'merge_request_review_%' AND payload ->> 'threadId' = $1
	`, threadID).Scan(&outboxCount); err != nil {
		t.Fatalf("count review outbox events: %v", err)
	}
	if auditCount != 6 || outboxCount != 5 {
		t.Fatalf("audit count = %d, outbox count = %d, want 6 and 5", auditCount, outboxCount)
	}
	page, err := platform.NewStore(fixture.pool).ListNotifications(
		context.Background(), fixture.writer, false, 100,
	)
	if err != nil {
		t.Fatalf("project review thread notifications: %v", err)
	}
	found := false
	for _, notification := range page.Items {
		if strings.HasPrefix(notification.Topic, "merge_request_review_") &&
			strings.HasSuffix(notification.Href, "/pulls/1") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("review thread notification was not linked to the pull request")
	}
}

func assertReviewTenantConstraint(t *testing.T, fixture reviewFixture, threadID string) {
	t.Helper()
	otherRepoID := uuid.NewString()
	mustReviewExec(t, fixture.pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, description, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, 'other', 'Other', '', 'private', $3, $4, 'main', $5)
	`, otherRepoID, fixture.orgID, strings.ReplaceAll(otherRepoID, "-", ""),
		"https://lore.invalid/"+otherRepoID, fixture.writer.ID)
	_, err := fixture.pool.Exec(context.Background(), `
		INSERT INTO merge_request_review_comments (
			id, repository_id, thread_id, author_id, body
		) VALUES ($1, $2, $3, $4, 'Wrong repository')
	`, uuid.NewString(), otherRepoID, threadID, fixture.writer.ID)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "23503" {
		t.Fatalf("cross-repository comment error = %v, want foreign key violation", err)
	}
}

func mustReviewExec(t *testing.T, pool *pgxpool.Pool, query string, arguments ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), query, arguments...); err != nil {
		t.Fatalf("execute review fixture query: %v", err)
	}
}
