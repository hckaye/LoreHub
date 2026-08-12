package discussions

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/database"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type discussionFixture struct {
	pool     *pgxpool.Pool
	orgID    string
	repoID   string
	repo     RepositoryRef
	owner    platform.User
	writer   platform.User
	reader   platform.User
	outsider platform.User
	suspend  platform.User
	revoked  platform.User
}

func TestPostgresDiscussionLifecyclePermissionsAndTenantBoundary(t *testing.T) {
	fixture := openDiscussionFixture(t)
	ctx := context.Background()
	store := NewStore(fixture.pool)

	question, err := store.Create(ctx, fixture.owner, fixture.repo, CreateInput{
		CategorySlug: "questions", Title: "How do builds work?", Body: "Please explain.",
	})
	if err != nil {
		t.Fatalf("create question: %v", err)
	}
	if question.Number != 1 || question.Category.Format != "question" {
		t.Fatalf("created question = %+v", question)
	}

	comment, err := store.CreateComment(
		ctx, fixture.writer, fixture.repo, question.Number, nil, "Builds use Lore revisions.",
	)
	if err != nil {
		t.Fatalf("create answer comment: %v", err)
	}
	reply, err := store.CreateComment(
		ctx, fixture.reader, fixture.repo, question.Number, &comment.ID, "That helps, thanks.",
	)
	if err != nil || reply.ParentID == nil || *reply.ParentID != comment.ID {
		t.Fatalf("create reply = %+v, error = %v", reply, err)
	}
	if _, err := store.SetAnswer(
		ctx, fixture.reader, fixture.repo, question.Number, comment.ID, true,
	); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("non-author answer error = %v, want forbidden", err)
	}
	question, err = store.SetAnswer(ctx, fixture.owner, fixture.repo, question.Number, comment.ID, true)
	if err != nil || !question.Answered {
		t.Fatalf("accept answer = %+v, error = %v", question, err)
	}

	if _, err := store.SetVote(ctx, fixture.reader, fixture.repo, question.Number, true); err != nil {
		t.Fatalf("add vote: %v", err)
	}
	if _, err := store.SetVote(ctx, fixture.reader, fixture.repo, question.Number, true); err != nil {
		t.Fatalf("repeat add vote: %v", err)
	}
	if _, err := store.SetVote(ctx, fixture.reader, fixture.repo, question.Number, false); err != nil {
		t.Fatalf("remove vote: %v", err)
	}
	if _, err := store.SetVote(ctx, fixture.reader, fixture.repo, question.Number, false); err != nil {
		t.Fatalf("repeat remove vote: %v", err)
	}
	assertDiscussionEventMetadata(t, fixture, "discussion.vote.updated", fixture.repoID)

	if _, err := store.Update(ctx, fixture.reader, fixture.repo, question.Number, UpdateInput{
		Locked: boolPointer(true),
	}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("reader lock error = %v, want forbidden", err)
	}
	question, err = store.Update(ctx, fixture.owner, fixture.repo, question.Number, UpdateInput{
		State: boolString("closed"), Locked: boolPointer(true), Pinned: boolPointer(true),
	})
	if err != nil || question.State != "closed" || !question.Locked || !question.Pinned {
		t.Fatalf("close and lock question = %+v, error = %v", question, err)
	}
	if _, err := store.CreateComment(
		ctx, fixture.reader, fixture.repo, question.Number, nil, "blocked",
	); !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("locked comment error = %v, want conflict", err)
	}
	if _, err := store.CreateComment(
		ctx, fixture.writer, fixture.repo, question.Number, nil, "moderator note",
	); err != nil {
		t.Fatalf("moderator comment on locked discussion: %v", err)
	}

	if _, err := store.UpdateComment(
		ctx, fixture.reader, fixture.repo, question.Number, reply.ID, "edited",
	); !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("closed reply edit error = %v, want conflict", err)
	}
	if err := store.DeleteComment(ctx, fixture.owner, fixture.repo, question.Number, comment.ID); err != nil {
		t.Fatalf("moderator delete answer: %v", err)
	}
	question, err = store.Get(ctx, fixture.repoID, question.Number, fixture.owner.ID, 1, 100)
	if err != nil || question.Answered || question.TotalComments != 2 {
		t.Fatalf("question after answer deletion = %+v, error = %v", question, err)
	}

	if _, err := store.Create(ctx, fixture.writer, fixture.repo, CreateInput{
		CategorySlug: "general", Title: "Writer announcement", Body: "A notice.",
	}); err != nil {
		t.Fatalf("writer general discussion: %v", err)
	}
	announcement, err := store.Create(ctx, fixture.writer, fixture.repo, CreateInput{
		CategorySlug: "announcements", Title: "Announcement", Body: "Important.",
	})
	if !errors.Is(err, platform.ErrNotFound) || announcement.ID != "" {
		t.Fatalf("missing announcement category = %+v, error = %v", announcement, err)
	}
	category, err := store.CreateCategory(ctx, fixture.owner, fixture.repo, CategoryInput{
		Slug: "announcements", Name: "Announcements", Format: "announcement",
	})
	if err != nil {
		t.Fatalf("create announcement category: %v", err)
	}
	if _, err := store.Create(ctx, fixture.reader, fixture.repo, CreateInput{
		CategorySlug: category.Slug, Title: "Not allowed",
	}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("reader announcement error = %v, want forbidden", err)
	}
	announcement, err = store.Create(ctx, fixture.owner, fixture.repo, CreateInput{
		CategorySlug: category.Slug, Title: "Announcement", Body: "Important.",
	})
	if err != nil {
		t.Fatalf("owner announcement: %v", err)
	}
	if err := store.DeleteCategory(ctx, fixture.owner, fixture.repo, category.Slug); !errors.Is(
		err, platform.ErrConflict,
	) {
		t.Fatalf("category with discussion delete error = %v, want conflict", err)
	}

	page, err := store.List(ctx, fixture.repoID, fixture.owner.ID, ListFilter{
		State: "open", Query: "Important", Sort: "newest", Page: 1, PerPage: 1,
	})
	if err != nil || page.TotalCount != 1 || len(page.Discussions) != 1 || page.Discussions[0].ID != announcement.ID {
		t.Fatalf("filtered page = %+v, error = %v", page, err)
	}
	if _, err := store.List(ctx, fixture.repoID, fixture.owner.ID, ListFilter{State: "bogus"}); err != nil {
		t.Fatalf("store list normalizes unknown state: %v", err)
	}

	if err := store.Delete(ctx, fixture.reader, fixture.repo, question.Number); !errors.Is(
		err, platform.ErrForbidden,
	) {
		t.Fatalf("non-author locked delete error = %v, want forbidden", err)
	}
	if err := store.Delete(ctx, fixture.owner, fixture.repo, question.Number); err != nil {
		t.Fatalf("delete question: %v", err)
	}
	if _, err := store.Get(ctx, fixture.repoID, question.Number, fixture.owner.ID, 1, 100); !errors.Is(
		err, platform.ErrNotFound,
	) {
		t.Fatalf("deleted question get error = %v, want not found", err)
	}
	assertDiscussionEventMetadata(t, fixture, "discussion.deleted", fixture.repoID)

	if _, err := store.Create(ctx, fixture.suspend, fixture.repo, CreateInput{
		CategorySlug: "general", Title: "Suspended",
	}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("suspended create error = %v, want forbidden", err)
	}
	if _, err := store.Create(ctx, fixture.revoked, fixture.repo, CreateInput{
		CategorySlug: "general", Title: "Revoked",
	}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("revoked membership create error = %v, want forbidden", err)
	}
	wrongTenant := RepositoryRef{ID: fixture.repoID, OrganizationID: uuid.NewString()}
	if _, err := store.Create(ctx, fixture.owner, wrongTenant, CreateInput{
		CategorySlug: "general", Title: "Cross tenant",
	}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("cross-tenant create error = %v, want forbidden", err)
	}

	if _, err := fixture.pool.Exec(ctx, `
		UPDATE repositories SET archived_at = now(), archived_by = $2 WHERE id = $1
	`, fixture.repoID, fixture.owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(ctx, fixture.owner, fixture.repo, CreateInput{
		CategorySlug: "general", Title: "Archived",
	}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("archived create error = %v, want forbidden", err)
	}
}

func openDiscussionFixture(t *testing.T) discussionFixture {
	t.Helper()
	databaseURL := os.Getenv("LOREHUB_TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("LOREHUB_TEST_DATABASE_URL or DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	fixture := seedDiscussionFixture(t, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, fixture.orgID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id IN ($1,$2,$3,$4,$5,$6)`,
			fixture.owner.ID, fixture.writer.ID, fixture.reader.ID, fixture.outsider.ID, fixture.suspend.ID,
			fixture.revoked.ID)
	})
	return fixture
}

func seedDiscussionFixture(t *testing.T, pool *pgxpool.Pool) discussionFixture {
	t.Helper()
	orgID, repoID := uuid.NewString(), uuid.NewString()
	owner := testDiscussionUser("discussion-owner", "Owner")
	writer := testDiscussionUser("discussion-writer", "Writer")
	reader := testDiscussionUser("discussion-reader", "Reader")
	outsider := testDiscussionUser("discussion-outsider", "Outsider")
	suspend := testDiscussionUser("discussion-suspended", "Suspended")
	revoked := testDiscussionUser("discussion-revoked", "Revoked")
	mustDiscussionExec(t, pool, `
		INSERT INTO users (id, username, display_name) VALUES
		($1,$2,$3),($4,$5,$6),($7,$8,$9),($10,$11,$12),($13,$14,$15),($16,$17,$18)
	`, owner.ID, owner.Username, owner.DisplayName, writer.ID, writer.Username, writer.DisplayName,
		reader.ID, reader.Username, reader.DisplayName, outsider.ID, outsider.Username, outsider.DisplayName,
		suspend.ID, suspend.Username, suspend.DisplayName, revoked.ID, revoked.Username, revoked.DisplayName)
	mustDiscussionExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, description, visibility, created_by)
		VALUES ($1, $2, 'Discussion Org', '', 'private', $3)
	`, orgID, "discussion-org-"+orgID[:8], owner.ID)
	mustDiscussionExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role) VALUES
		($1,$2,'owner'),($1,$3,'member'),($1,$4,'member'),($1,$5,'member'),($1,$6,'member'),($1,$7,'member')
	`, orgID, owner.ID, writer.ID, reader.ID, outsider.ID, suspend.ID, revoked.ID)
	mustDiscussionExec(t, pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, description, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1,$2,'discussions','Discussions','', 'private', $3, $4, 'main', $5)
	`, repoID, orgID, compactDiscussionUUID(repoID), "https://lore.invalid/"+repoID, owner.ID)
	mustDiscussionExec(t, pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, repoID)
	mustDiscussionExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role) VALUES
		($1,$2,'admin'),($1,$3,'write'),($1,$4,'read'),($1,$5,'write'),($1,$6,'write')
	`, repoID, owner.ID, writer.ID, reader.ID, suspend.ID, revoked.ID)
	mustDiscussionExec(t, pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, suspend.ID)
	mustDiscussionExec(t, pool, `
		UPDATE repository_memberships SET active = false
		WHERE repository_id = $1 AND user_id = $2
	`,
		repoID, revoked.ID)
	return discussionFixture{
		pool: pool, orgID: orgID, repoID: repoID,
		repo: RepositoryRef{ID: repoID, OrganizationID: orgID}, owner: owner,
		writer: writer, reader: reader, outsider: outsider, suspend: suspend, revoked: revoked,
	}
}

func testDiscussionUser(prefix string, displayName string) platform.User {
	return platform.User{ID: uuid.NewString(), Username: prefix + "-" + uuid.NewString()[:8], DisplayName: displayName}
}

func mustDiscussionExec(t *testing.T, pool *pgxpool.Pool, query string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), query, args...); err != nil {
		t.Fatalf("discussion fixture SQL: %v", err)
	}
}

func assertDiscussionEventMetadata(t *testing.T, fixture discussionFixture, action string, repositoryID string) {
	t.Helper()
	var auditPayload, outboxPayload []byte
	ctx := context.Background()
	if err := fixture.pool.QueryRow(ctx, `
		SELECT details FROM audit_events WHERE action = $1 AND repository_id = $2
		ORDER BY occurred_at DESC LIMIT 1
	`, action, repositoryID).Scan(&auditPayload); err != nil {
		t.Fatalf("read %s audit event: %v", action, err)
	}
	if err := fixture.pool.QueryRow(ctx, `
		SELECT payload FROM outbox_events WHERE topic = $1 ORDER BY created_at DESC LIMIT 1
	`, action).Scan(&outboxPayload); err != nil {
		t.Fatalf("read %s outbox event: %v", action, err)
	}
	for name, payload := range map[string][]byte{"audit": auditPayload, "outbox": outboxPayload} {
		var details map[string]any
		if err := json.Unmarshal(payload, &details); err != nil {
			t.Fatalf("decode %s %s payload: %v", name, action, err)
		}
		if details["repositoryId"] != repositoryID || details["organizationId"] != fixture.orgID ||
			details["action"] != action || details["targetType"] == "" || details["targetId"] == "" {
			t.Fatalf("%s %s metadata = %#v", name, action, details)
		}
	}
}

func compactDiscussionUUID(value string) string {
	result := make([]byte, 0, 32)
	for _, character := range []byte(value) {
		if character != '-' {
			result = append(result, character)
		}
	}
	return string(result)
}

func boolPointer(value bool) *bool { return &value }

func boolString(value string) *string { return &value }
