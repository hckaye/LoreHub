package collab

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const (
	revisionCommentA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	revisionCommentB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestIntegrationRevisionCommentLifecycleAndPermissions(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "public", "triage")
	repository, err := store.LookupRepository(ctx, &fixture.carol, fixture.ownerSlug, fixture.repoSlug)
	if err != nil {
		t.Fatalf("lookup repository: %v", err)
	}

	first, err := store.CreateRevisionComment(
		ctx, fixture.carol, repository, revisionCommentA, "First comment",
	)
	if err != nil {
		t.Fatalf("reader create comment: %v", err)
	}
	second, err := store.CreateRevisionComment(
		ctx, fixture.bob, repository, revisionCommentA, "Second comment",
	)
	if err != nil {
		t.Fatalf("triage create comment: %v", err)
	}
	if _, err := store.CreateRevisionComment(
		ctx, fixture.carol, repository, "not-a-revision", "Invalid",
	); !errors.Is(err, platform.ErrInvalidInput) {
		t.Fatalf("invalid revision error = %v", err)
	}

	page, err := store.ListRevisionComments(ctx, nil, repository, revisionCommentA, 1, 1)
	if err != nil {
		t.Fatalf("anonymous list: %v", err)
	}
	if page.TotalCount != 2 || len(page.Items) != 1 || !page.HasNext || page.Items[0].ViewerCanUpdate {
		t.Fatalf("anonymous page = %+v", page)
	}
	secondPage, err := store.ListRevisionComments(ctx, &fixture.carol, repository, revisionCommentA, 2, 1)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(secondPage.Items) != 1 || secondPage.Items[0].ID != second.ID || secondPage.HasNext {
		t.Fatalf("second page = %+v", secondPage)
	}
	empty, err := store.ListRevisionComments(ctx, nil, repository, revisionCommentB, 1, 30)
	if err != nil || empty.TotalCount != 0 || len(empty.Items) != 0 {
		t.Fatalf("other revision page = %+v, err = %v", empty, err)
	}

	updated, err := store.UpdateRevisionComment(
		ctx, fixture.carol, repository, revisionCommentA, first.ID, "Author edit",
	)
	if err != nil || updated.Body != "Author edit" || updated.EditedAt == nil {
		t.Fatalf("author update = %+v, err = %v", updated, err)
	}
	if _, err := store.UpdateRevisionComment(
		ctx, fixture.carol, repository, revisionCommentA, second.ID, "Not allowed",
	); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("reader editing another author error = %v", err)
	}
	if _, err := store.UpdateRevisionComment(
		ctx, fixture.bob, repository, revisionCommentA, first.ID, "Triage edit",
	); err != nil {
		t.Fatalf("triage update: %v", err)
	}
	if err := store.DeleteRevisionComment(
		ctx, fixture.bob, repository, revisionCommentA, first.ID,
	); err != nil {
		t.Fatalf("triage delete: %v", err)
	}
	if err := store.DeleteRevisionComment(
		ctx, fixture.bob, repository, revisionCommentB, second.ID,
	); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("cross revision delete error = %v", err)
	}

	var auditCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM audit_events
		WHERE repository_id = $1 AND action LIKE 'revision_comment.%'
	`, fixture.repoID).Scan(&auditCount); err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if auditCount != 5 {
		t.Fatalf("audit count = %d, want 5", auditCount)
	}
	var outboxCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM outbox_events
		WHERE topic LIKE 'revision_comment.%'
		  AND payload->>'repositoryId' = $1
		  AND payload->'comment'->>'viewerCanUpdate' = 'false'
	`, fixture.repoID).Scan(&outboxCount); err != nil {
		t.Fatalf("count outbox events: %v", err)
	}
	if outboxCount != 5 {
		t.Fatalf("outbox count = %d, want 5", outboxCount)
	}
}

func TestIntegrationRevisionCommentsRespectRepositoryLifecycle(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "private", "read")
	repository, err := store.LookupRepository(ctx, &fixture.bob, fixture.ownerSlug, fixture.repoSlug)
	if err != nil {
		t.Fatalf("lookup repository: %v", err)
	}
	comment, err := store.CreateRevisionComment(
		ctx, fixture.bob, repository, revisionCommentA, "Before archive",
	)
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	archivedAt := time.Now().UTC()
	mustExec(t, ctx, pool, `
		UPDATE repositories SET archived_at = $2, archived_by = $3 WHERE id = $1
	`, fixture.repoID, archivedAt, fixture.alice.ID)
	repository.ArchivedAt = &archivedAt
	page, err := store.ListRevisionComments(ctx, &fixture.bob, repository, revisionCommentA, 1, 30)
	if err != nil || len(page.Items) != 1 || page.Items[0].ViewerCanUpdate {
		t.Fatalf("archived list = %+v, err = %v", page, err)
	}
	if _, err := store.CreateRevisionComment(
		ctx, fixture.bob, repository, revisionCommentA, "After archive",
	); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("archived create error = %v", err)
	}
	if err := store.DeleteRevisionComment(
		ctx, fixture.bob, repository, revisionCommentA, comment.ID,
	); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("archived delete error = %v", err)
	}

	mustExec(t, ctx, pool, `UPDATE repositories SET archived_at = NULL, archived_by = NULL WHERE id = $1`,
		fixture.repoID)
	repository.ArchivedAt = nil
	mustExec(t, ctx, pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, fixture.bob.ID)
	if _, err := store.ListRevisionComments(
		ctx, &fixture.bob, repository, revisionCommentA, 1, 30,
	); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("suspended viewer error = %v", err)
	}
}

func TestIntegrationRevisionCommentsAllowExternalDirectReader(t *testing.T) {
	pool, store := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "internal", "")
	reader := platform.User{
		ID: uuidNew(), Username: "external-" + fixture.orgID[:8], DisplayName: "External reader",
	}
	mustExec(t, ctx, pool, `
		INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)
	`, reader.ID, reader.Username, reader.DisplayName)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, reader.ID)
	})
	mustExec(t, ctx, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role)
		VALUES ($1, $2, 'read')
	`, fixture.repoID, reader.ID)
	repository, err := store.LookupRepository(ctx, &reader, fixture.ownerSlug, fixture.repoSlug)
	if err != nil {
		t.Fatalf("external reader lookup: %v", err)
	}
	comment, err := store.CreateRevisionComment(
		ctx, reader, repository, revisionCommentA, "External review",
	)
	if err != nil || comment.Author.ID != reader.ID {
		t.Fatalf("external reader create = %+v, err = %v", comment, err)
	}
	mustExec(t, ctx, pool, `
		UPDATE repository_memberships SET active = false
		WHERE repository_id = $1 AND user_id = $2
	`, fixture.repoID, reader.ID)
	if _, err := store.ListRevisionComments(
		ctx, &reader, repository, revisionCommentA, 1, 30,
	); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("revoked external reader error = %v", err)
	}
}

func TestIntegrationRevisionCommentDatabaseConstraints(t *testing.T) {
	pool, _ := integrationEnv(t)
	ctx := context.Background()
	fixture := setupFixture(t, pool, "public", "")
	for _, input := range []struct {
		name     string
		revision string
		body     string
	}{
		{name: "uppercase revision", revision: "A" + revisionCommentA[1:], body: "Valid"},
		{name: "blank body", revision: revisionCommentA, body: "  \n\t"},
		{name: "oversized body", revision: revisionCommentA, body: strings.Repeat("x", 1_000_001)},
	} {
		t.Run(input.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, `
				INSERT INTO revision_comments (
					id, repository_id, revision, author_id, body
				) VALUES ($1, $2, $3, $4, $5)
			`, uuidNew(), fixture.repoID, input.revision, fixture.alice.ID, input.body)
			if err == nil {
				t.Fatal("database accepted an invalid revision comment")
			}
		})
	}
}
