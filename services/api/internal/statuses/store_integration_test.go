package statuses

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/collab"
	"github.com/lorehub/lorehub/services/api/internal/database"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type statusFixture struct {
	pool     *pgxpool.Pool
	writer   platform.User
	reader   platform.User
	outsider platform.User
	repo     RepositoryRef
	orgID    string
	repoID   string
	orgSlug  string
}

func TestPostgresRevisionStatusHistoryIdempotencyAndTenantBoundary(t *testing.T) {
	fixture := openStatusFixture(t)
	ctx := context.Background()
	store := NewStore(fixture.pool)
	if _, err := store.Create(ctx, fixture.reader, fixture.repo, CreateInput{
		Revision: testRevision, Context: "build", State: "pending",
	}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("reader create error = %v, want forbidden", err)
	}
	if _, err := store.Create(ctx, fixture.outsider, fixture.repo, CreateInput{
		Revision: testRevision, Context: "build", State: "pending",
	}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("outsider create error = %v, want forbidden", err)
	}
	createStatus(t, store, fixture, CreateInput{
		Revision: testRevision, Context: "CI/Test", State: "pending", Description: "Running",
	})
	createStatus(t, store, fixture, CreateInput{
		Revision: testRevision, Context: "ci/test", State: "success", Description: "Passed",
	})
	createStatus(t, store, fixture, CreateInput{
		Revision: testRevision, Context: "lint", State: "success",
	})
	createStatus(t, store, fixture, CreateInput{
		Revision: testRevision, Context: "deploy", State: "failure",
	})
	page, err := store.List(ctx, fixture.repoID, testRevision, 1, 3)
	if err != nil {
		t.Fatalf("list statuses: %v", err)
	}
	if page.TotalCount != 4 || len(page.Statuses) != 3 || len(page.History) != 3 ||
		!page.HasNext || page.State != "failure" {
		t.Fatalf("status page = %#v", page)
	}
	assertLatestContext(t, page.Statuses, "ci/test", "success")

	key := "delivery-" + uuid.NewString()
	input := CreateInput{
		Revision: testRevision, Context: "security", State: "success",
		TargetURL: "https://ci.example.test/runs/1", IdempotencyKey: &key,
	}
	first, err := store.Create(ctx, fixture.writer, fixture.repo, input)
	if err != nil || !first.Created {
		t.Fatalf("first idempotent create = %#v, error = %v", first, err)
	}
	second, err := store.Create(ctx, fixture.writer, fixture.repo, input)
	if err != nil || second.Created || second.Status.ID != first.Status.ID {
		t.Fatalf("repeated idempotent create = %#v, error = %v", second, err)
	}
	input.State = "failure"
	if _, err := store.Create(ctx, fixture.writer, fixture.repo, input); !errors.Is(
		err, platform.ErrConflict,
	) {
		t.Fatalf("changed idempotent create error = %v, want conflict", err)
	}
	assertStatusAuditAndOutbox(t, fixture, first.Status.ID)
	assertCrossRepositoryIsolation(t, fixture, store)
}

func TestPostgresRevisionStatusAccessLifecycle(t *testing.T) {
	fixture := openStatusFixture(t)
	ctx := context.Background()
	store := NewStore(fixture.pool)
	created := createStatus(t, store, fixture, CreateInput{
		Revision: testRevision, Context: "build", State: "success",
	})
	if created.Status.Creator.ID != fixture.writer.ID {
		t.Fatalf("creator = %#v", created.Status.Creator)
	}
	mustStatusExec(t, fixture.pool, `
		UPDATE repositories SET archived_at = now(), archived_by = $2 WHERE id = $1
	`, fixture.repoID, fixture.writer.ID)
	if _, err := store.Create(ctx, fixture.writer, fixture.repo, CreateInput{
		Revision: testRevision, Context: "archived", State: "success",
	}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("archived create error = %v, want forbidden", err)
	}
	if _, err := store.List(ctx, fixture.repoID, testRevision, 1, 30); err != nil {
		t.Fatalf("archived read: %v", err)
	}
	mustStatusExec(t, fixture.pool, `
		UPDATE repositories SET archived_at = NULL, archived_by = NULL WHERE id = $1
	`, fixture.repoID)
	mustStatusExec(t, fixture.pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, fixture.writer.ID)
	if _, err := store.Create(ctx, fixture.writer, fixture.repo, CreateInput{
		Revision: testRevision, Context: "suspended", State: "success",
	}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("suspended create error = %v, want forbidden", err)
	}
	mustStatusExec(t, fixture.pool, `UPDATE users SET status = 'active' WHERE id = $1`, fixture.writer.ID)
	mustStatusExec(t, fixture.pool, `UPDATE organizations SET active = false WHERE id = $1`, fixture.orgID)
	if _, err := store.Create(ctx, fixture.writer, fixture.repo, CreateInput{
		Revision: testRevision, Context: "inactive", State: "success",
	}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("inactive organization create error = %v, want forbidden", err)
	}
	if _, err := store.List(ctx, fixture.repoID, testRevision, 1, 30); !errors.Is(
		err, platform.ErrNotFound,
	) {
		t.Fatalf("inactive organization read error = %v, want not found", err)
	}
	mustStatusExec(t, fixture.pool, `UPDATE organizations SET active = true WHERE id = $1`, fixture.orgID)
	mustStatusExec(
		t, fixture.pool, `UPDATE repositories SET lifecycle_state = 'failed' WHERE id = $1`, fixture.repoID,
	)
	if _, err := store.Create(ctx, fixture.writer, fixture.repo, CreateInput{
		Revision: testRevision, Context: "failed", State: "success",
	}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("failed repository create error = %v, want forbidden", err)
	}
	if _, err := store.List(ctx, fixture.repoID, testRevision, 1, 30); !errors.Is(
		err, platform.ErrNotFound,
	) {
		t.Fatalf("failed repository read error = %v, want not found", err)
	}
}

func TestPostgresRevisionStatusConcurrentIdempotency(t *testing.T) {
	fixture := openStatusFixture(t)
	store := NewStore(fixture.pool)
	key := "concurrent-" + uuid.NewString()
	input := CreateInput{
		Revision: testRevision, Context: "build", State: "success", IdempotencyKey: &key,
	}
	results := make(chan CreateResult, 2)
	errorsFound := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			result, err := store.Create(context.Background(), fixture.writer, fixture.repo, input)
			if err != nil {
				errorsFound <- err
				return
			}
			results <- result
		}()
	}
	workers.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent idempotent create: %v", err)
	}
	created := 0
	statusID := ""
	for result := range results {
		if result.Created {
			created++
		}
		if statusID == "" {
			statusID = result.Status.ID
		} else if result.Status.ID != statusID {
			t.Fatalf("concurrent status IDs = %q and %q", statusID, result.Status.ID)
		}
	}
	if created != 1 {
		t.Fatalf("created results = %d, want 1", created)
	}
	assertStatusAuditAndOutbox(t, fixture, statusID)
}

func TestPostgresRevisionStatusPublicAndPrivateRepositoryLookup(t *testing.T) {
	fixture := openStatusFixture(t)
	ctx := context.Background()
	repositories := collab.NewStore(fixture.pool)
	if _, err := repositories.LookupRepository(ctx, nil, fixture.orgSlug, "game"); !errors.Is(
		err, platform.ErrNotFound,
	) {
		t.Fatalf("anonymous private lookup error = %v, want not found", err)
	}
	mustStatusExec(
		t, fixture.pool, `UPDATE repositories SET visibility = 'public' WHERE id = $1`, fixture.repoID,
	)
	repository, err := repositories.LookupRepository(ctx, nil, fixture.orgSlug, "game")
	if err != nil || repository.ID != fixture.repoID {
		t.Fatalf("anonymous public lookup = %#v, error = %v", repository, err)
	}
}

func TestRevisionStatusMigrationPreservesRepositoryDeletion(t *testing.T) {
	fixture := openStatusFixture(t)
	ctx := context.Background()
	environmentID := uuid.NewString()
	workflowID := uuid.NewString()
	runID := uuid.NewString()
	jobID := uuid.NewString()
	mustStatusExec(t, fixture.pool, `
		INSERT INTO repository_environments (id, repository_id, name, created_by)
		VALUES ($1, $2, 'Production', $3)
	`, environmentID, fixture.repoID, fixture.writer.ID)
	mustStatusExec(t, fixture.pool, `
		INSERT INTO ci_workflows (
			id, repository_id, path, name, last_seen_revision, trigger_config
		) VALUES ($1, $2, '.github/workflows/deploy.yml', 'Deploy', 'revision', '{}')
	`, workflowID, fixture.repoID)
	mustStatusExec(t, fixture.pool, `
		INSERT INTO ci_runs (
			id, repository_id, workflow_id, run_number, event_name,
			branch, revision, status, event_payload
		) VALUES ($1, $2, $3, 1, 'push', 'main', 'revision', 'queued', '{}')
	`, runID, fixture.repoID, workflowID)
	mustStatusExec(t, fixture.pool, `
		INSERT INTO ci_jobs (id, run_id, name, status, attempt)
		VALUES ($1, $2, 'deploy', 'queued', 1)
	`, jobID, runID)
	mustStatusExec(t, fixture.pool, `
		INSERT INTO deployments (
			id, repository_id, environment_id, environment_name, run_id, job_id,
			branch, revision, status, wait_until
		) VALUES ($1, $2, $3, 'Production', $4, $5, 'main', 'revision', 'queued', now())
	`, uuid.NewString(), fixture.repoID, environmentID, runID, jobID)
	if _, err := fixture.pool.Exec(ctx, `DELETE FROM repositories WHERE id = $1`, fixture.repoID); err != nil {
		t.Fatalf("delete repository with deployment history: %v", err)
	}
}

func createStatus(
	t *testing.T,
	store Store,
	fixture statusFixture,
	input CreateInput,
) CreateResult {
	t.Helper()
	result, err := store.Create(context.Background(), fixture.writer, fixture.repo, input)
	if err != nil {
		t.Fatalf("create status %#v: %v", input, err)
	}
	return result
}

func assertLatestContext(t *testing.T, statuses []Status, contextName string, state string) {
	t.Helper()
	matches := 0
	for _, status := range statuses {
		if stringsEqualFold(status.Context, contextName) {
			matches++
			if status.Context != contextName || status.State != state {
				t.Fatalf("latest context = %#v", status)
			}
		}
	}
	if matches != 1 {
		t.Fatalf("latest context %q appeared %d times in %#v", contextName, matches, statuses)
	}
}

func stringsEqualFold(first string, second string) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		a := first[index]
		b := second[index]
		if a >= 'A' && a <= 'Z' {
			a += 'a' - 'A'
		}
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}

func assertStatusAuditAndOutbox(t *testing.T, fixture statusFixture, statusID string) {
	t.Helper()
	var auditCount, outboxCount int
	var payload []byte
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM audit_events
		WHERE repository_id = $1 AND target_id = $2 AND action = 'revision_status.created'
	`, fixture.repoID, statusID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) OVER (), payload FROM outbox_events
		WHERE topic = 'revision_status.created' AND event_key = $1
		LIMIT 1
	`, statusID).Scan(&outboxCount, &payload); err != nil {
		t.Fatal(err)
	}
	if auditCount != 1 || outboxCount != 1 {
		t.Fatalf("audit count = %d, outbox count = %d", auditCount, outboxCount)
	}
	var event map[string]any
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatalf("decode status outbox payload: %v", err)
	}
	for _, field := range []string{"context", "description", "revision", "state", "targetUrl"} {
		if _, found := event[field]; !found {
			t.Fatalf("status outbox payload omitted %q: %s", field, payload)
		}
	}
}

func assertCrossRepositoryIsolation(t *testing.T, fixture statusFixture, store Store) {
	t.Helper()
	ctx := context.Background()
	otherRepoID := uuid.NewString()
	mustStatusExec(t, fixture.pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, description, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, $3, 'Other', '', 'private', $4, $5, 'main', $6)
	`, otherRepoID, fixture.orgID, "other-"+otherRepoID[:8], compactStatusUUID(otherRepoID),
		"https://lore.invalid/"+otherRepoID, fixture.writer.ID)
	mustStatusExec(t, fixture.pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, otherRepoID)
	mustStatusExec(t, fixture.pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role)
		VALUES ($1, $2, 'write')
	`, otherRepoID, fixture.writer.ID)
	other := RepositoryRef{ID: otherRepoID, OrganizationID: fixture.orgID}
	if _, err := store.Create(ctx, fixture.writer, other, CreateInput{
		Revision: testRevision, Context: "other", State: "success",
	}); err != nil {
		t.Fatalf("other repository create: %v", err)
	}
	page, err := store.List(ctx, fixture.repoID, testRevision, 1, 100)
	if err != nil {
		t.Fatalf("first repository list: %v", err)
	}
	for _, status := range page.History {
		if status.Context == "other" {
			t.Fatalf("cross-repository status leaked: %#v", status)
		}
	}
	if _, err := store.Create(
		ctx, fixture.writer, RepositoryRef{ID: fixture.repoID, OrganizationID: uuid.NewString()},
		CreateInput{Revision: testRevision, Context: "wrong-org", State: "success"},
	); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("cross-organization reference error = %v, want forbidden", err)
	}
}

func openStatusFixture(t *testing.T) statusFixture {
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
	fixture := seedStatusFixture(t, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, fixture.orgID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id IN ($1, $2, $3)`,
			fixture.writer.ID, fixture.reader.ID, fixture.outsider.ID)
	})
	return fixture
}

func seedStatusFixture(t *testing.T, pool *pgxpool.Pool) statusFixture {
	t.Helper()
	orgID := uuid.NewString()
	repoID := uuid.NewString()
	writer := platform.User{ID: uuid.NewString(), Username: "status-writer-" + orgID[:8]}
	reader := platform.User{ID: uuid.NewString(), Username: "status-reader-" + orgID[:8]}
	outsider := platform.User{ID: uuid.NewString(), Username: "status-outside-" + orgID[:8]}
	mustStatusExec(t, pool, `
		INSERT INTO users (id, username, display_name)
		VALUES ($1, $2, 'Writer'), ($3, $4, 'Reader'), ($5, $6, 'Outsider')
	`, writer.ID, writer.Username, reader.ID, reader.Username, outsider.ID, outsider.Username)
	orgSlug := "status-org-" + orgID[:8]
	mustStatusExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, description, visibility, created_by)
		VALUES ($1, $2, 'Status Org', '', 'private', $3)
	`, orgID, orgSlug, writer.ID)
	mustStatusExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'member'), ($1, $3, 'member')
	`, orgID, writer.ID, reader.ID)
	mustStatusExec(t, pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, description, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, 'game', 'Game', '', 'private', $3, $4, 'main', $5)
	`, repoID, orgID, compactStatusUUID(repoID), "https://lore.invalid/"+repoID, writer.ID)
	mustStatusExec(t, pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, repoID)
	mustStatusExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role)
		VALUES ($1, $2, 'write'), ($1, $3, 'read')
	`, repoID, writer.ID, reader.ID)
	return statusFixture{
		pool: pool, writer: writer, reader: reader, outsider: outsider,
		repo:  RepositoryRef{ID: repoID, OrganizationID: orgID},
		orgID: orgID, repoID: repoID, orgSlug: orgSlug,
	}
}

func mustStatusExec(t *testing.T, pool *pgxpool.Pool, query string, arguments ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), query, arguments...); err != nil {
		t.Fatal(err)
	}
}

func compactStatusUUID(value string) string {
	result := make([]byte, 0, 32)
	for _, character := range []byte(value) {
		if character != '-' {
			result = append(result, character)
		}
	}
	return string(result)
}
