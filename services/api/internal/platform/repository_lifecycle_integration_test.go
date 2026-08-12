package platform

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/authz"
)

func TestRepositoryArchiveLifecycleIsReadOnlyAndAudited(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	owner := platformTestUser("archive-owner-" + suffix)
	writer := platformTestUser("archive-writer-" + suffix)
	organizationID := uuid.NewString()
	repositoryID := uuid.NewString()
	workflowID := uuid.NewString()
	runID := uuid.NewString()
	jobID := uuid.NewString()
	organizationSlug := "archive-org-" + suffix
	repositorySlug := "archive-repository-" + suffix
	for _, user := range []User{owner, writer} {
		mustIdentityExec(t, pool, `
			INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)
		`, user.ID, user.Username, user.DisplayName)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Archive organization', 'public', $3)
	`, organizationID, organizationSlug, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'owner'), ($1, $3, 'member')
	`, organizationID, owner.ID, writer.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, $3, 'Archive repository', 'public', $4, $5, 'main', $6)
	`, repositoryID, organizationID, repositorySlug, canonicalTestLoreID(repositoryID),
		"lore://"+repositorySlug, owner.ID)
	mustIdentityExec(t, pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, repositoryID)
	mustIdentityExec(t, pool, `
		INSERT INTO repository_policies (repository_id, updated_by) VALUES ($1, $2)
	`, repositoryID, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role)
		VALUES ($1, $2, 'write')
	`, repositoryID, writer.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO ci_workflows (id, repository_id, path, name, last_seen_revision)
		VALUES ($1, $2, '.lorehub/workflows/test.yml', 'Test', $3)
	`, workflowID, repositoryID, strings.Repeat("a", 64))
	mustIdentityExec(t, pool, `
		INSERT INTO ci_runs (
			id, repository_id, workflow_id, run_number, event_name, branch,
			revision, actor_id, status, event_payload
		) VALUES ($1, $2, $3, 1, 'push', 'main', $4, $5, 'in_progress', '{}')
	`, runID, repositoryID, workflowID, strings.Repeat("a", 64), writer.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO ci_jobs (id, run_id, name, status, lease_owner, lease_expires_at)
		VALUES ($1, $2, 'test', 'in_progress', 'runner-1', now() + interval '5 minutes')
	`, jobID, runID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, organizationID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id IN ($1, $2)`, owner.ID, writer.ID)
	})

	confirmation := organizationSlug + "/" + repositorySlug
	if _, err := store.SetRepositoryArchived(
		ctx, owner, organizationSlug, repositorySlug, true, "wrong/repository",
	); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid archive confirmation error = %v", err)
	}
	if _, err := store.SetRepositoryArchived(
		ctx, writer, organizationSlug, repositorySlug, true, confirmation,
	); !errors.Is(err, ErrForbidden) {
		t.Fatalf("writer archive error = %v, want forbidden", err)
	}
	repository, err := store.SetRepositoryArchived(
		ctx, owner, organizationSlug, repositorySlug, true, confirmation,
	)
	if err != nil || repository.ArchivedAt == nil {
		t.Fatalf("archive repository = %+v, err=%v", repository, err)
	}
	readable, err := store.RepositoryForRead(ctx, &writer, organizationSlug, repositorySlug)
	if err != nil || readable.ArchivedAt == nil {
		t.Fatalf("read archived repository = %+v, err=%v", readable, err)
	}
	if _, err := store.RepositoryForWrite(ctx, writer, organizationSlug, repositorySlug); !errors.Is(err, ErrNotFound) {
		t.Fatalf("write archived repository error = %v", err)
	}
	if _, err := store.RepositoryForSettings(ctx, owner, organizationSlug, repositorySlug); err != nil {
		t.Fatalf("read archived settings: %v", err)
	}
	name := "Changed while archived"
	if _, err := store.UpdateRepositorySettings(ctx, owner, organizationSlug, repositorySlug,
		UpdateRepositorySettingsInput{DisplayName: &name}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("update archived settings error = %v", err)
	}
	permissions, err := store.EffectivePermissions(ctx, writer.ID, "urc-"+canonicalTestLoreID(repositoryID))
	if err != nil || !slices.Equal(permissions.Permissions, []string{authz.PermissionRead}) {
		t.Fatalf("archived Lore permissions = %+v, err=%v", permissions, err)
	}
	assertArchivedRunCancelled(t, pool, runID, jobID)
	assertRepositoryArchiveEvents(t, pool, repositoryID, "repository.archive", "repository.archived")

	if _, err := store.SetRepositoryArchived(
		ctx, owner, organizationSlug, repositorySlug, true, confirmation,
	); err != nil {
		t.Fatalf("repeat archive: %v", err)
	}
	assertRepositoryArchiveEventCount(t, pool, repositoryID, "repository.archive", 1)
	repository, err = store.SetRepositoryArchived(
		ctx, owner, organizationSlug, repositorySlug, false, confirmation,
	)
	if err != nil || repository.ArchivedAt != nil {
		t.Fatalf("unarchive repository = %+v, err=%v", repository, err)
	}
	if _, err := store.RepositoryForWrite(ctx, writer, organizationSlug, repositorySlug); err != nil {
		t.Fatalf("write unarchived repository: %v", err)
	}
	assertRepositoryArchiveEvents(t, pool, repositoryID, "repository.unarchive", "repository.unarchived")
}

func assertArchivedRunCancelled(t *testing.T, pool *pgxpool.Pool, runID string, jobID string) {
	t.Helper()
	var runStatus, runConclusion string
	var cancelRequested bool
	if err := pool.QueryRow(context.Background(), `
		SELECT status, conclusion, cancel_requested FROM ci_runs WHERE id = $1
	`, runID).Scan(&runStatus, &runConclusion, &cancelRequested); err != nil {
		t.Fatalf("read archived run: %v", err)
	}
	if runStatus != "cancelled" || runConclusion != "cancelled" || !cancelRequested {
		t.Fatalf("archived run state = %s/%s cancel=%t", runStatus, runConclusion, cancelRequested)
	}
	var jobStatus, jobConclusion string
	var leaseOwner *string
	if err := pool.QueryRow(context.Background(), `
		SELECT status, conclusion, lease_owner FROM ci_jobs WHERE id = $1
	`, jobID).Scan(&jobStatus, &jobConclusion, &leaseOwner); err != nil {
		t.Fatalf("read archived job: %v", err)
	}
	if jobStatus != "cancelled" || jobConclusion != "cancelled" || leaseOwner != nil {
		t.Fatalf("archived job state = %s/%s lease=%v", jobStatus, jobConclusion, leaseOwner)
	}
}

func assertRepositoryArchiveEvents(
	t *testing.T,
	pool *pgxpool.Pool,
	repositoryID string,
	action string,
	topic string,
) {
	t.Helper()
	assertRepositoryArchiveEventCount(t, pool, repositoryID, action, 1)
	var count int
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM outbox_events
		WHERE topic = $1 AND payload ->> 'repositoryId' = $2
	`, topic, repositoryID).Scan(&count); err != nil {
		t.Fatalf("count repository archive outbox events: %v", err)
	}
	if count != 1 {
		t.Fatalf("repository archive outbox count = %d, want 1", count)
	}
}

func assertRepositoryArchiveEventCount(
	t *testing.T,
	pool *pgxpool.Pool,
	repositoryID string,
	action string,
	want int,
) {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM audit_events WHERE repository_id = $1 AND action = $2
	`, repositoryID, action).Scan(&count); err != nil {
		t.Fatalf("count repository archive audit events: %v", err)
	}
	if count != want {
		t.Fatalf("repository archive audit count = %d, want %d", count, want)
	}
}
