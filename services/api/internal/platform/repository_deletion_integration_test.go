package platform

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/authz"
)

type repositoryDeletionFixture struct {
	pool           *pgxpool.Pool
	store          *Store
	owner          User
	writer         User
	organizationID string
	repositoryID   string
	ownerSlug      string
	repositorySlug string
	runID          string
	jobID          string
}

func TestRepositoryDeletionCanBeRestoredBeforePurge(t *testing.T) {
	fixture := newRepositoryDeletionFixture(t)
	ctx := context.Background()
	confirmation := fixture.ownerSlug + "/" + fixture.repositorySlug
	if _, err := fixture.store.ScheduleRepositoryDeletion(
		ctx,
		fixture.owner,
		fixture.ownerSlug,
		fixture.repositorySlug,
		"wrong/repository",
		24*time.Hour,
	); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid deletion confirmation error = %v", err)
	}
	if _, err := fixture.store.ScheduleRepositoryDeletion(
		ctx,
		fixture.writer,
		fixture.ownerSlug,
		fixture.repositorySlug,
		confirmation,
		24*time.Hour,
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("repository admin deletion error = %v, want forbidden", err)
	}
	deleted, err := fixture.store.ScheduleRepositoryDeletion(
		ctx,
		fixture.owner,
		fixture.ownerSlug,
		fixture.repositorySlug,
		confirmation,
		24*time.Hour,
	)
	if err != nil {
		t.Fatalf("schedule repository deletion: %v", err)
	}
	if deleted.Purging || deleted.RequestedBy != fixture.owner.Username ||
		deleted.PurgeAfter.Sub(deleted.RequestedAt) < 23*time.Hour {
		t.Fatalf("unexpected deleted repository: %+v", deleted)
	}
	if _, err := fixture.store.RepositoryForRead(
		ctx,
		&fixture.owner,
		fixture.ownerSlug,
		fixture.repositorySlug,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted repository remained readable: %v", err)
	}
	if _, err := fixture.store.ListDeletedRepositories(
		ctx,
		fixture.writer,
		fixture.ownerSlug,
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("writer deleted-repository list error = %v", err)
	}
	items, err := fixture.store.ListDeletedRepositories(ctx, fixture.owner, fixture.ownerSlug)
	if err != nil || len(items) != 1 || items[0].ID != fixture.repositoryID {
		t.Fatalf("deleted repository list = %+v, err=%v", items, err)
	}
	_, permissions, err := fixture.store.ServicePrincipalResource(
		ctx,
		"lorehub-repository-lifecycle",
		"urc-"+canonicalTestLoreID(fixture.repositoryID),
	)
	if err != nil || !slices.Equal(permissions, []string{authz.PermissionObliterate}) {
		t.Fatalf("repository lifecycle permissions = %v, err=%v", permissions, err)
	}
	assertArchivedRunCancelled(t, fixture.pool, fixture.runID, fixture.jobID)

	repository, err := fixture.store.RestoreRepository(
		ctx,
		fixture.owner,
		fixture.ownerSlug,
		fixture.repositorySlug,
	)
	if err != nil || repository.LifecycleState != "active" {
		t.Fatalf("restore repository = %+v, err=%v", repository, err)
	}
	if _, err := fixture.store.RepositoryForRead(
		ctx,
		&fixture.writer,
		fixture.ownerSlug,
		fixture.repositorySlug,
	); err != nil {
		t.Fatalf("restored repository is not readable: %v", err)
	}
	if _, _, err := fixture.store.ServicePrincipalResource(
		ctx,
		"lorehub-repository-lifecycle",
		"urc-"+canonicalTestLoreID(fixture.repositoryID),
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("restored lifecycle principal error = %v, want forbidden", err)
	}
	assertRepositoryDeletionEvent(t, fixture.pool, fixture.repositoryID, "repository.deletion.schedule")
	assertRepositoryDeletionEvent(t, fixture.pool, fixture.repositoryID, "repository.deletion.restore")
}

func TestRepositoryDeletionClaimsRetryAndRemovesControlPlaneRecords(t *testing.T) {
	fixture := newRepositoryDeletionFixture(t)
	ctx := context.Background()
	_, err := fixture.store.ScheduleRepositoryDeletion(
		ctx,
		fixture.owner,
		fixture.ownerSlug,
		fixture.repositorySlug,
		fixture.ownerSlug+"/"+fixture.repositorySlug,
		time.Hour,
	)
	if err != nil {
		t.Fatalf("schedule repository deletion: %v", err)
	}
	mustIdentityExec(t, fixture.pool, `
		UPDATE repository_deletions
		SET requested_at = now() - interval '2 hours',
		    purge_after = now() - interval '2 minutes',
		    next_attempt_at = now() - interval '1 minute'
		WHERE repository_id = $1
	`, fixture.repositoryID)
	claim, err := fixture.store.ClaimRepositoryDeletion(ctx, "worker-1", 2*time.Minute)
	if err != nil || claim == nil || claim.RepositoryID != fixture.repositoryID || claim.Attempt != 1 {
		t.Fatalf("first deletion claim = %+v, err=%v", claim, err)
	}
	other, err := fixture.store.ClaimRepositoryDeletion(ctx, "worker-2", 2*time.Minute)
	if err != nil || other != nil {
		t.Fatalf("concurrent deletion claim = %+v, err=%v", other, err)
	}
	if _, err := fixture.store.RestoreRepository(
		ctx,
		fixture.owner,
		fixture.ownerSlug,
		fixture.repositorySlug,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("restore after purge started error = %v, want conflict", err)
	}
	if err := fixture.store.FailRepositoryDeletion(
		ctx,
		"worker-1",
		*claim,
		5*time.Minute,
		errors.New("temporary Lore failure"),
	); err != nil {
		t.Fatalf("record deletion failure: %v", err)
	}
	claim, err = fixture.store.ClaimRepositoryDeletion(ctx, "worker-2", 2*time.Minute)
	if err != nil || claim != nil {
		t.Fatalf("early retry claim = %+v, err=%v", claim, err)
	}
	mustIdentityExec(t, fixture.pool, `UPDATE organizations SET active = false WHERE id = $1`, fixture.organizationID)
	_, permissions, err := fixture.store.ServicePrincipalResource(
		ctx,
		"lorehub-repository-lifecycle",
		"urc-"+canonicalTestLoreID(fixture.repositoryID),
	)
	if err != nil || !slices.Equal(permissions, []string{authz.PermissionObliterate}) {
		t.Fatalf("inactive organization lifecycle permissions = %v, err=%v", permissions, err)
	}
	mustIdentityExec(t, fixture.pool, `
		UPDATE repository_deletions SET next_attempt_at = now() - interval '1 minute'
		WHERE repository_id = $1
	`, fixture.repositoryID)
	claim, err = fixture.store.ClaimRepositoryDeletion(ctx, "worker-2", 2*time.Minute)
	if err != nil || claim == nil || claim.Attempt != 2 {
		t.Fatalf("retry deletion claim = %+v, err=%v", claim, err)
	}
	if err := fixture.store.CompleteRepositoryDeletion(ctx, "worker-2", *claim); err != nil {
		t.Fatalf("complete repository deletion: %v", err)
	}
	var repositoryCount int
	if err := fixture.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM repositories WHERE id = $1
	`, fixture.repositoryID).Scan(&repositoryCount); err != nil {
		t.Fatalf("count deleted repository: %v", err)
	}
	if repositoryCount != 0 {
		t.Fatalf("deleted repository count = %d", repositoryCount)
	}
	var owner, slug string
	if err := fixture.pool.QueryRow(ctx, `
		SELECT repository_owner, repository_slug
		FROM audit_events
		WHERE target_id = $1 AND action = 'repository.deletion.complete'
	`, fixture.repositoryID).Scan(&owner, &slug); err != nil {
		t.Fatalf("read permanent deletion audit event: %v", err)
	}
	if owner != fixture.ownerSlug || slug != fixture.repositorySlug {
		t.Fatalf("permanent deletion audit context = %s/%s", owner, slug)
	}
}

func newRepositoryDeletionFixture(t *testing.T) repositoryDeletionFixture {
	t.Helper()
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	fixture := repositoryDeletionFixture{
		pool: pool, store: store, owner: platformTestUser("delete-owner-" + suffix),
		writer: platformTestUser("delete-writer-" + suffix), organizationID: uuid.NewString(),
		repositoryID: uuid.NewString(), ownerSlug: "delete-org-" + suffix,
		repositorySlug: "delete-repository-" + suffix, runID: uuid.NewString(), jobID: uuid.NewString(),
	}
	for _, user := range []User{fixture.owner, fixture.writer} {
		mustIdentityExec(t, pool, `
			INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)
		`, user.ID, user.Username, user.DisplayName)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Deletion organization', 'private', $3)
	`, fixture.organizationID, fixture.ownerSlug, fixture.owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'owner'), ($1, $3, 'member')
	`, fixture.organizationID, fixture.owner.ID, fixture.writer.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, visibility,
			lore_repository_id, lore_url, default_branch, created_by, archived_at, archived_by
		) VALUES ($1, $2, $3, 'Deletion repository', 'private', $4, $5, 'main', $6, now(), $6)
	`, fixture.repositoryID, fixture.organizationID, fixture.repositorySlug,
		canonicalTestLoreID(fixture.repositoryID), "lore://"+fixture.repositorySlug, fixture.owner.ID)
	mustIdentityExec(t, pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, fixture.repositoryID)
	mustIdentityExec(t, pool, `
		INSERT INTO repository_policies (repository_id, updated_by) VALUES ($1, $2)
	`, fixture.repositoryID, fixture.owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role) VALUES ($1, $2, 'admin')
	`, fixture.repositoryID, fixture.writer.ID)
	workflowID := uuid.NewString()
	mustIdentityExec(t, pool, `
		INSERT INTO ci_workflows (id, repository_id, path, name, last_seen_revision)
		VALUES ($1, $2, '.lorehub/workflows/delete.yml', 'Delete', $3)
	`, workflowID, fixture.repositoryID, strings.Repeat("a", 64))
	mustIdentityExec(t, pool, `
		INSERT INTO ci_runs (
			id, repository_id, workflow_id, run_number, event_name, branch,
			revision, actor_id, status, event_payload
		) VALUES ($1, $2, $3, 1, 'push', 'main', $4, $5, 'in_progress', '{}')
	`, fixture.runID, fixture.repositoryID, workflowID, strings.Repeat("a", 64), fixture.writer.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO ci_jobs (id, run_id, name, status, lease_owner, lease_expires_at)
		VALUES ($1, $2, 'delete-test', 'in_progress', 'runner-1', now() + interval '5 minutes')
	`, fixture.jobID, fixture.runID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, fixture.organizationID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id IN ($1, $2)`, fixture.owner.ID, fixture.writer.ID)
	})
	return fixture
}

func assertRepositoryDeletionEvent(
	t *testing.T,
	pool *pgxpool.Pool,
	repositoryID string,
	action string,
) {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM audit_events WHERE repository_id = $1 AND action = $2
	`, repositoryID, action).Scan(&count); err != nil {
		t.Fatalf("count repository deletion audit events: %v", err)
	}
	if count != 1 {
		t.Fatalf("repository deletion audit count = %d for %s", count, action)
	}
}
