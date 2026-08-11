package collab

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func TestIntegrationReviewRequests(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "private", "triage")
	other := setupFixture(t, pool, "private", "read")
	number := seedMergeRequest(t, ctx, pool, fixture, fixture.alice.ID, "review-request-1")
	mergeRequest, err := store.GetMergeRequest(ctx, fixture.repoID, number)
	if err != nil {
		t.Fatal(err)
	}
	seedReviewTeam(t, ctx, pool, fixture, fixture.carol)

	candidates, err := store.ListReviewCandidates(ctx, fixture.alice, fixture.repoID, number, "")
	if err != nil {
		t.Fatalf("list review candidates: %v", err)
	}
	if !hasReviewCandidate(candidates, "user", fixture.bob.Username) ||
		!hasReviewCandidate(candidates, "team", "reviewers") ||
		hasReviewCandidate(candidates, "user", fixture.alice.Username) {
		t.Fatalf("review candidates = %#v", candidates)
	}

	userRequest, created, err := store.RequestUserReview(
		ctx, fixture.alice, fixture.repoID, number, fixture.bob.Username,
	)
	if err != nil || !created || userRequest.Status != "pending" {
		t.Fatalf("request user review = %#v, created = %t, error = %v", userRequest, created, err)
	}
	if existing, created, err := store.RequestUserReview(
		ctx, fixture.alice, fixture.repoID, number, fixture.bob.Username,
	); err != nil || created || existing.ID != userRequest.ID {
		t.Fatalf("idempotent user request = %#v, created = %t, error = %v", existing, created, err)
	}
	teamRequest, created, err := store.RequestTeamReview(
		ctx, fixture.alice, fixture.repoID, number, "reviewers",
	)
	if err != nil || !created || teamRequest.Kind != "team" {
		t.Fatalf("request team review = %#v, created = %t, error = %v", teamRequest, created, err)
	}
	assertReviewRequestNotifications(t, pool, fixture, userRequest.ID, teamRequest.ID)
	if _, _, err := store.RequestUserReview(
		ctx, fixture.alice, fixture.repoID, number, fixture.alice.Username,
	); !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("request author error = %v, want conflict", err)
	}

	if _, _, err := store.CreateReview(
		ctx, fixture.bob, fixture.repoID, number, ReviewInput{Decision: "approved"},
	); err != nil {
		t.Fatalf("user reviewer approval: %v", err)
	}
	if _, _, err := store.CreateReview(
		ctx, fixture.carol, fixture.repoID, number, ReviewInput{Decision: "changes_requested"},
	); err != nil {
		t.Fatalf("team reviewer decision: %v", err)
	}
	requests, err := store.ListReviewRequests(ctx, fixture.repoID, number)
	if err != nil {
		t.Fatalf("list review requests: %v", err)
	}
	assertReviewRequestStatus(t, requests, "user", fixture.bob.Username, "approved")
	assertReviewRequestStatus(t, requests, "team", "reviewers", "changes_requested")
	currentUser, created, err := store.RequestUserReview(
		ctx, fixture.alice, fixture.repoID, number, fixture.bob.Username,
	)
	if err != nil || created || currentUser.Status != "approved" {
		t.Fatalf("current idempotent user request = %#v, created = %t, error = %v", currentUser, created, err)
	}
	currentTeam, created, err := store.RequestTeamReview(
		ctx, fixture.alice, fixture.repoID, number, "reviewers",
	)
	if err != nil || created || currentTeam.Status != "changes_requested" {
		t.Fatalf("current idempotent team request = %#v, created = %t, error = %v", currentTeam, created, err)
	}

	mustExec(t, ctx, pool, `
		UPDATE merge_requests SET source_revision = 'review-request-2'
		WHERE id = $1 AND repository_id = $2
	`, mergeRequest.ID, fixture.repoID)
	requests, err = store.ListReviewRequests(ctx, fixture.repoID, number)
	if err != nil {
		t.Fatalf("list outdated review requests: %v", err)
	}
	assertReviewRequestStatus(t, requests, "user", fixture.bob.Username, "pending")
	assertReviewRequestStatus(t, requests, "team", "reviewers", "pending")

	assertReviewRequestTenantBoundary(
		t, pool, fixture, other.repoID, mergeRequest.ID,
	)
	mustExec(t, ctx, pool, `
		UPDATE repository_memberships SET active = false
		WHERE repository_id = $1 AND user_id = $2
	`, fixture.repoID, fixture.bob.ID)
	if err := store.RemoveUserReviewRequest(
		ctx, fixture.bob, fixture.repoID, number, fixture.bob.Username,
	); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("inactive manager error = %v, want forbidden", err)
	}
	if err := store.RemoveUserReviewRequest(
		ctx, fixture.alice, fixture.repoID, number, fixture.bob.Username,
	); err != nil {
		t.Fatalf("remove user review request: %v", err)
	}
	if err := store.RemoveUserReviewRequest(
		ctx, fixture.alice, fixture.repoID, number, fixture.bob.Username,
	); err != nil {
		t.Fatalf("idempotent review request removal: %v", err)
	}
	requests, err = store.ListReviewRequests(ctx, fixture.repoID, number)
	if err != nil || len(requests) != 1 || requests[0].Slug != "reviewers" {
		t.Fatalf("requests after removal = %#v, error = %v", requests, err)
	}
	assertReviewRequestAuditAndOutbox(t, pool, fixture.repoID, userRequest.ID, teamRequest.ID)
}

func TestIntegrationReviewRequestLimitIsConcurrent(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "private", "")
	number := seedMergeRequest(t, ctx, pool, fixture, fixture.alice.ID, "review-limit")
	users := make([]platform.User, 0, maxReviewRequests+1)
	userIDs := make([]string, 0, maxReviewRequests+1)
	for index := 0; index <= maxReviewRequests; index++ {
		user := platform.User{
			ID: uuidNew(), Username: fmt.Sprintf("reviewer-%02d-%s", index, fixture.orgID[:8]),
			DisplayName: fmt.Sprintf("Reviewer %02d", index),
		}
		mustExec(t, ctx, pool, `
			INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)
		`, user.ID, user.Username, user.DisplayName)
		mustExec(t, ctx, pool, `
			INSERT INTO repository_memberships (repository_id, user_id, role)
			VALUES ($1, $2, 'read')
		`, fixture.repoID, user.ID)
		users = append(users, user)
		userIDs = append(userIDs, user.ID)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1::uuid[])`, userIDs)
	})

	var wait sync.WaitGroup
	errorsByUser := make(chan error, len(users))
	for _, user := range users {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, _, err := store.RequestUserReview(
				context.Background(), fixture.alice, fixture.repoID, number, user.Username,
			)
			errorsByUser <- err
		}()
	}
	wait.Wait()
	close(errorsByUser)
	limits := 0
	for err := range errorsByUser {
		if errors.Is(err, ErrReviewRequestLimit) {
			limits++
			continue
		}
		if err != nil {
			t.Fatalf("concurrent review request: %v", err)
		}
	}
	requests, err := store.ListReviewRequests(ctx, fixture.repoID, number)
	if err != nil || len(requests) != maxReviewRequests || limits != 1 {
		t.Fatalf("concurrent requests = %d, limits = %d, error = %v", len(requests), limits, err)
	}
}

func seedReviewTeam(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture integrationFixture,
	member platform.User,
) string {
	t.Helper()
	teamID := uuidNew()
	mustExec(t, ctx, pool, `
		INSERT INTO teams (
			id, organization_id, slug, display_name, created_by, active
		) VALUES ($1, $2, 'reviewers', 'Reviewers', $3, true)
	`, teamID, fixture.orgID, fixture.alice.ID)
	mustExec(t, ctx, pool, `
		INSERT INTO team_memberships (team_id, user_id, role, active)
		VALUES ($1, $2, 'member', true)
	`, teamID, member.ID)
	mustExec(t, ctx, pool, `
		INSERT INTO team_repository_roles (
			team_id, repository_id, role, created_by, active
		) VALUES ($1, $2, 'read', $3, true)
	`, teamID, fixture.repoID, fixture.alice.ID)
	return teamID
}

func hasReviewCandidate(candidates []ReviewCandidate, kind string, slug string) bool {
	for _, candidate := range candidates {
		if candidate.Kind == kind && candidate.Slug == slug {
			return true
		}
	}
	return false
}

func assertReviewRequestStatus(
	t *testing.T,
	requests []ReviewRequest,
	kind string,
	slug string,
	status string,
) {
	t.Helper()
	for _, request := range requests {
		if request.Kind == kind && request.Slug == slug {
			if request.Status != status {
				t.Fatalf("review request %s/%s status = %q, want %q", kind, slug, request.Status, status)
			}
			return
		}
	}
	t.Fatalf("review request %s/%s was not found in %#v", kind, slug, requests)
}

func assertReviewRequestTenantBoundary(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture integrationFixture,
	otherRepositoryID string,
	mergeRequestID string,
) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO merge_request_review_requests (
			id, organization_id, repository_id, merge_request_id,
			reviewer_user_id, requested_by
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, uuidNew(), fixture.orgID, otherRepositoryID, mergeRequestID, fixture.carol.ID, fixture.alice.ID)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "23503" {
		t.Fatalf("cross-repository review request error = %v, want foreign key violation", err)
	}
}

func assertReviewRequestAuditAndOutbox(
	t *testing.T,
	pool *pgxpool.Pool,
	repositoryID string,
	userRequestID string,
	teamRequestID string,
) {
	t.Helper()
	var auditCount, outboxCount int
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM audit_events
		WHERE repository_id = $1 AND target_id IN ($2, $3)
		  AND action LIKE 'merge_request.review_request.%'
	`, repositoryID, userRequestID, teamRequestID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM outbox_events
		WHERE topic LIKE 'merge_request_review_request.%'
		  AND (event_key LIKE $1 OR event_key LIKE $2)
	`, userRequestID+"%", teamRequestID+"%").Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 3 || outboxCount != 3 {
		t.Fatalf("review request audit = %d, outbox = %d, want 3 each", auditCount, outboxCount)
	}
}

func assertReviewRequestNotifications(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture integrationFixture,
	userRequestID string,
	teamRequestID string,
) {
	t.Helper()
	store := platform.NewStore(pool)
	for index := 0; index < 3; index++ {
		if _, err := store.ListNotifications(context.Background(), fixture.bob, false, 20); err != nil {
			t.Fatalf("project review request notifications: %v", err)
		}
	}
	var userEventID, teamEventID string
	if err := pool.QueryRow(context.Background(), `
		SELECT id FROM outbox_events
		WHERE topic = 'merge_request_review_request.created' AND event_key LIKE $1
	`, userRequestID+"%").Scan(&userEventID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `
		SELECT id FROM outbox_events
		WHERE topic = 'merge_request_review_request.created' AND event_key LIKE $1
	`, teamRequestID+"%").Scan(&teamEventID); err != nil {
		t.Fatal(err)
	}
	assertNotificationCount := func(userID string, eventID string, want int) {
		t.Helper()
		var count int
		if err := pool.QueryRow(context.Background(), `
			SELECT COUNT(*) FROM notifications WHERE recipient_id = $1 AND source_event_id = $2
		`, userID, eventID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != want {
			t.Fatalf("notification recipient %s event %s count = %d, want %d", userID, eventID, count, want)
		}
	}
	assertNotificationCount(fixture.bob.ID, userEventID, 1)
	assertNotificationCount(fixture.carol.ID, userEventID, 0)
	assertNotificationCount(fixture.carol.ID, teamEventID, 1)
	assertNotificationCount(fixture.bob.ID, teamEventID, 0)
	assertNotificationCount(fixture.alice.ID, userEventID, 0)
	assertNotificationCount(fixture.alice.ID, teamEventID, 0)
}
