package collab

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func TestRepositoryEngagementIntegration(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "public", "")

	starred, err := store.SetRepositoryStar(ctx, fixture.bob, fixture.repoID, true)
	if err != nil {
		t.Fatalf("star repository: %v", err)
	}
	if starred.StarCount != 1 || !starred.ViewerHasStarred {
		t.Fatalf("starred snapshot = %+v", starred)
	}
	if _, err := store.SetRepositoryStar(ctx, fixture.bob, fixture.repoID, true); err != nil {
		t.Fatalf("repeat star repository: %v", err)
	}
	watched, err := store.SetRepositoryWatch(ctx, fixture.bob, fixture.repoID, true)
	if err != nil {
		t.Fatalf("watch repository: %v", err)
	}
	if watched.WatcherCount != 1 || !watched.ViewerIsWatching || !watched.ViewerHasStarred {
		t.Fatalf("watched snapshot = %+v", watched)
	}

	viewerRepository, err := store.LookupRepository(
		ctx, &fixture.bob, fixture.ownerSlug, fixture.repoSlug,
	)
	if err != nil {
		t.Fatalf("lookup viewer repository: %v", err)
	}
	if viewerRepository.StarCount != 1 || viewerRepository.WatcherCount != 1 ||
		!viewerRepository.ViewerHasStarred || !viewerRepository.ViewerIsWatching {
		t.Fatalf("viewer repository = %+v", viewerRepository)
	}
	publicRepository, err := store.LookupRepository(ctx, nil, fixture.ownerSlug, fixture.repoSlug)
	if err != nil {
		t.Fatalf("lookup public repository: %v", err)
	}
	if publicRepository.StarCount != 1 || publicRepository.WatcherCount != 1 ||
		publicRepository.ViewerHasStarred || publicRepository.ViewerIsWatching {
		t.Fatalf("public repository = %+v", publicRepository)
	}

	assertEngagementEventCount(t, pool, fixture.repoID, "repository.star", 1)
	assertEngagementEventCount(t, pool, fixture.repoID, "repository.watch", 1)
	assertEngagementTopicCount(t, pool, fixture.repoID, "repository_engagement.starred", 1)
	assertEngagementTopicCount(t, pool, fixture.repoID, "repository_engagement.watched", 1)

	if _, err := store.SetRepositoryStar(ctx, fixture.bob, fixture.repoID, false); err != nil {
		t.Fatalf("unstar repository: %v", err)
	}
	unwatched, err := store.SetRepositoryWatch(ctx, fixture.bob, fixture.repoID, false)
	if err != nil {
		t.Fatalf("unwatch repository: %v", err)
	}
	if unwatched.StarCount != 0 || unwatched.WatcherCount != 0 ||
		unwatched.ViewerHasStarred || unwatched.ViewerIsWatching {
		t.Fatalf("removed engagement snapshot = %+v", unwatched)
	}
	assertEngagementEventCount(t, pool, fixture.repoID, "repository.unstar", 1)
	assertEngagementEventCount(t, pool, fixture.repoID, "repository.unwatch", 1)

	if _, err := store.SetRepositoryStar(ctx, fixture.carol, fixture.repoID, true); err != nil {
		t.Fatalf("public reader star: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET status = 'suspended' WHERE id = $1`, fixture.carol.ID); err != nil {
		t.Fatalf("suspend reader: %v", err)
	}
	_, err = store.SetRepositoryStar(ctx, fixture.carol, fixture.repoID, false)
	if !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("suspended reader mutation error = %v", err)
	}
	afterSuspension, err := store.LookupRepository(ctx, nil, fixture.ownerSlug, fixture.repoSlug)
	if err != nil {
		t.Fatalf("lookup repository after suspension: %v", err)
	}
	if afterSuspension.StarCount != 0 {
		t.Fatalf("suspended stargazer remained in count: %+v", afterSuspension)
	}

	privateFixture := setupFixture(t, pool, "private", "")
	if _, err := store.SetRepositoryWatch(
		ctx, privateFixture.carol, privateFixture.repoID, true,
	); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("private outsider mutation error = %v", err)
	}
}

func TestRepositoryWatchDrivesPublicNotificationsIntegration(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "public", "")
	if _, err := store.SetRepositoryWatch(ctx, fixture.bob, fixture.repoID, true); err != nil {
		t.Fatalf("watch repository: %v", err)
	}
	platformStore := platform.NewStore(pool)
	firstEvent := insertEngagementIssueEvent(t, pool, fixture, 1)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM outbox_events WHERE id = $1`, firstEvent)
	})
	projectNotificationEvent(t, pool, platformStore, fixture.bob, firstEvent)
	if countNotificationSource(t, pool, fixture.bob.ID, firstEvent) != 1 {
		t.Fatal("watcher did not receive the repository notification")
	}
	pullRequestEvent := insertEngagementPullRequestCommentEvent(t, pool, fixture)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM outbox_events WHERE id = $1`, pullRequestEvent)
	})
	projectNotificationEvent(t, pool, platformStore, fixture.bob, pullRequestEvent)
	if countNotificationSource(t, pool, fixture.bob.ID, pullRequestEvent) != 1 {
		t.Fatal("watcher did not receive the pull request comment notification")
	}
	assertNotificationHref(
		t, pool, fixture.bob.ID, pullRequestEvent,
		"/"+fixture.ownerSlug+"/"+fixture.repoSlug+"/pulls/1",
	)

	if _, err := store.SetRepositoryWatch(ctx, fixture.bob, fixture.repoID, false); err != nil {
		t.Fatalf("unwatch repository: %v", err)
	}
	secondEvent := insertEngagementIssueEvent(t, pool, fixture, 2)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM outbox_events WHERE id = $1`, secondEvent)
	})
	projectNotificationEvent(t, pool, platformStore, fixture.bob, secondEvent)
	if countNotificationSource(t, pool, fixture.bob.ID, secondEvent) != 0 {
		t.Fatal("unwatched user received a later repository notification")
	}
}

func TestRepositoryStarConcurrentIdempotenceIntegration(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "public", "")
	results := make(chan error, 8)
	for attempt := 0; attempt < cap(results); attempt++ {
		go func() {
			_, err := store.SetRepositoryStar(ctx, fixture.bob, fixture.repoID, true)
			results <- err
		}()
	}
	for attempt := 0; attempt < cap(results); attempt++ {
		if err := <-results; err != nil {
			t.Fatalf("concurrent star %d: %v", attempt, err)
		}
	}
	repository, err := store.LookupRepository(ctx, &fixture.bob, fixture.ownerSlug, fixture.repoSlug)
	if err != nil {
		t.Fatalf("lookup repository after concurrent star: %v", err)
	}
	if repository.StarCount != 1 || !repository.ViewerHasStarred {
		t.Fatalf("repository after concurrent star = %+v", repository)
	}
	assertEngagementEventCount(t, pool, fixture.repoID, "repository.star", 1)
	assertEngagementTopicCount(t, pool, fixture.repoID, "repository_engagement.starred", 1)
}

func insertEngagementIssueEvent(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture integrationFixture,
	number int,
) string {
	t.Helper()
	issueID := uuid.NewString()
	eventID := uuid.NewString()
	mustExec(t, context.Background(), pool, `
		INSERT INTO issues (id, repository_id, number, title, author_id)
		VALUES ($1, $2, $3, $4, $5)
	`, issueID, fixture.repoID, number, "Watched issue", fixture.alice.ID)
	mustExec(t, context.Background(), pool, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, 'issue.created', $2, '{"title":"Watched issue"}')
	`, eventID, issueID)
	return eventID
}

func insertEngagementPullRequestCommentEvent(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture integrationFixture,
) string {
	t.Helper()
	requestID := uuid.NewString()
	commentID := uuid.NewString()
	eventID := uuid.NewString()
	mustExec(t, context.Background(), pool, `
		INSERT INTO merge_requests (
			id, repository_id, number, title, source_branch, target_branch,
			source_revision, target_revision, author_id
		) VALUES ($1, $2, 1, 'Watched pull request', 'feature', 'main', 'source', 'target', $3)
	`, requestID, fixture.repoID, fixture.alice.ID)
	mustExec(t, context.Background(), pool, `
		INSERT INTO merge_request_comments (id, merge_request_id, author_id, body)
		VALUES ($1, $2, $3, 'Watched pull request comment')
	`, commentID, requestID, fixture.alice.ID)
	mustExec(t, context.Background(), pool, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, 'merge_request_comment.created', $2, $3)
	`, eventID, commentID, `{"body":"Watched pull request comment"}`)
	return eventID
}

func countNotificationSource(
	t *testing.T,
	pool *pgxpool.Pool,
	recipientID string,
	sourceID string,
) int64 {
	t.Helper()
	var count int64
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM notifications WHERE recipient_id = $1 AND source_event_id = $2
	`, recipientID, sourceID).Scan(&count); err != nil {
		t.Fatalf("count notification source: %v", err)
	}
	return count
}

func assertNotificationHref(
	t *testing.T,
	pool *pgxpool.Pool,
	recipientID string,
	sourceID string,
	want string,
) {
	t.Helper()
	var href string
	if err := pool.QueryRow(context.Background(), `
		SELECT href FROM notifications WHERE recipient_id = $1 AND source_event_id = $2
	`, recipientID, sourceID).Scan(&href); err != nil {
		t.Fatalf("read notification href: %v", err)
	}
	if href != want {
		t.Fatalf("notification href = %q, want %q", href, want)
	}
}

func projectNotificationEvent(
	t *testing.T,
	pool *pgxpool.Pool,
	store *platform.Store,
	actor platform.User,
	eventID string,
) {
	t.Helper()
	for attempt := 0; attempt < 20; attempt++ {
		if _, err := store.ListNotifications(context.Background(), actor, false, 20); err != nil {
			t.Fatalf("project notification event: %v", err)
		}
		var processed bool
		err := pool.QueryRow(context.Background(), `
			SELECT status = 'processed'
			FROM notification_projection_ledger
			WHERE source_event_id = $1
		`, eventID).Scan(&processed)
		if err == nil && processed {
			return
		}
	}
	t.Fatalf("notification event %s was not projected", eventID)
}

func assertEngagementEventCount(
	t *testing.T,
	pool *pgxpool.Pool,
	repositoryID string,
	action string,
	want int64,
) {
	t.Helper()
	var count int64
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM audit_events WHERE repository_id = $1 AND action = $2
	`, repositoryID, action).Scan(&count); err != nil {
		t.Fatalf("count engagement audit: %v", err)
	}
	if count != want {
		t.Fatalf("engagement audit %q count = %d, want %d", action, count, want)
	}
}

func assertEngagementTopicCount(
	t *testing.T,
	pool *pgxpool.Pool,
	repositoryID string,
	topic string,
	want int64,
) {
	t.Helper()
	var count int64
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM outbox_events
		WHERE topic = $2 AND split_part(event_key, ':', 1) = $1
	`, repositoryID, topic).Scan(&count); err != nil {
		t.Fatalf("count engagement outbox: %v", err)
	}
	if count != want {
		t.Fatalf("engagement outbox %q count = %d, want %d", topic, count, want)
	}
}
