package wiki

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

type wikiFixture struct {
	pool       *pgxpool.Pool
	writer     platform.User
	reader     platform.User
	teamWriter platform.User
	suspended  platform.User
	repository RepositoryRef
	orgID      string
	repoID     string
}

func TestPostgresWikiLifecycleHistoryAndTenantBoundary(t *testing.T) {
	fixture := openWikiFixture(t)
	ctx := context.Background()
	store := NewStore(fixture.pool)
	input := CreateInput{
		Title: "Release guide", Body: "# Release\n\nPrepare a build.", EditSummary: "Create guide",
	}
	if _, err := store.Create(ctx, fixture.reader, fixture.repository, input); !errors.Is(
		err, platform.ErrForbidden,
	) {
		t.Fatalf("reader create error = %v, want forbidden", err)
	}
	page, err := store.Create(ctx, fixture.writer, fixture.repository, input)
	if err != nil {
		t.Fatalf("create wiki page: %v", err)
	}
	if page.Slug != "release-guide" || page.Version != 1 || !page.ViewerCanWrite {
		t.Fatalf("created page = %+v", page)
	}
	if _, err := store.Create(ctx, fixture.writer, fixture.repository, input); !errors.Is(
		err, platform.ErrConflict,
	) {
		t.Fatalf("duplicate create error = %v, want conflict", err)
	}
	pages, err := store.List(ctx, fixture.repoID, "Release", 50)
	if err != nil || len(pages) != 1 {
		t.Fatalf("list wiki pages = %+v, error = %v", pages, err)
	}
	newSlug := "deployment-guide"
	newTitle := "Deployment guide"
	newBody := "# Deployment\n\nPublish the build."
	page, err = store.Update(ctx, fixture.writer, fixture.repository, page.Slug, UpdateInput{
		Slug: &newSlug, Title: &newTitle, Body: &newBody,
		EditSummary: "Rename and expand", ExpectedVersion: 1,
	})
	if err != nil {
		t.Fatalf("update wiki page: %v", err)
	}
	if page.Slug != newSlug || page.Version != 2 || page.Body != newBody {
		t.Fatalf("updated page = %+v", page)
	}
	if _, err := store.Get(ctx, fixture.repoID, "release-guide"); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("old slug get error = %v, want not found", err)
	}
	history, err := store.History(ctx, fixture.repoID, newSlug, 100)
	if err != nil || len(history) != 2 || history[0].Version != 2 || history[1].Version != 1 {
		t.Fatalf("history = %+v, error = %v", history, err)
	}
	revision, err := store.Revision(ctx, fixture.repoID, newSlug, 1)
	if err != nil || revision.Body != input.Body || revision.Slug != "release-guide" {
		t.Fatalf("revision = %+v, error = %v", revision, err)
	}
	if _, err := store.Update(ctx, fixture.writer, fixture.repository, newSlug, UpdateInput{
		Body: &newBody, ExpectedVersion: 1,
	}); !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("stale update error = %v, want conflict", err)
	}
	wrongTenant := RepositoryRef{ID: fixture.repoID, OrganizationID: uuid.NewString()}
	if _, err := store.Update(ctx, fixture.writer, wrongTenant, newSlug, UpdateInput{
		Body: &newBody, ExpectedVersion: 2,
	}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("cross-tenant update error = %v, want forbidden", err)
	}
	if err := store.Delete(ctx, fixture.writer, fixture.repository, newSlug, 1); !errors.Is(
		err, platform.ErrConflict,
	) {
		t.Fatalf("stale delete error = %v, want conflict", err)
	}
	if err := store.Delete(ctx, fixture.writer, fixture.repository, newSlug, 2); err != nil {
		t.Fatalf("delete wiki page: %v", err)
	}
	if _, err := store.Get(ctx, fixture.repoID, newSlug); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("deleted page get error = %v, want not found", err)
	}
	recreated, err := store.Create(ctx, fixture.writer, fixture.repository, CreateInput{
		Slug: newSlug, Title: "New deployment guide", Body: "Replacement",
	})
	if err != nil || recreated.ID == page.ID {
		t.Fatalf("recreated page = %+v, error = %v", recreated, err)
	}
	assertWikiAuditAndOutbox(t, fixture, page.ID)
	assertWikiDatabaseTenantConstraint(t, fixture, recreated)
}

func TestPostgresWikiConcurrentUpdateAndActivePermissions(t *testing.T) {
	fixture := openWikiFixture(t)
	ctx := context.Background()
	store := NewStore(fixture.pool)
	page, err := store.Create(ctx, fixture.teamWriter, fixture.repository, CreateInput{
		Title: "Operations", Body: "Initial",
	})
	if err != nil {
		t.Fatalf("team writer create: %v", err)
	}
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	start := make(chan struct{})
	for _, body := range []string{"First update", "Second update"} {
		go func(body string) {
			ready.Done()
			<-start
			_, updateErr := store.Update(ctx, fixture.teamWriter, fixture.repository, page.Slug, UpdateInput{
				Body: &body, ExpectedVersion: page.Version,
			})
			results <- updateErr
		}(body)
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
			t.Fatalf("concurrent update error = %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes = %d, conflicts = %d", successes, conflicts)
	}
	if _, err := store.Update(ctx, fixture.suspended, fixture.repository, page.Slug, UpdateInput{
		ExpectedVersion: 2,
	}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("suspended update error = %v, want forbidden", err)
	}
}

func openWikiFixture(t *testing.T) wikiFixture {
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
	fixture := seedWikiFixture(t, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, fixture.orgID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id IN ($1, $2, $3, $4)`,
			fixture.writer.ID, fixture.reader.ID, fixture.teamWriter.ID, fixture.suspended.ID)
	})
	return fixture
}

func seedWikiFixture(t *testing.T, pool *pgxpool.Pool) wikiFixture {
	t.Helper()
	orgID := uuid.NewString()
	repoID := uuid.NewString()
	writer := platform.User{ID: uuid.NewString(), Username: "wiki-writer-" + orgID[:8]}
	reader := platform.User{ID: uuid.NewString(), Username: "wiki-reader-" + orgID[:8]}
	teamWriter := platform.User{ID: uuid.NewString(), Username: "wiki-team-" + orgID[:8]}
	suspended := platform.User{ID: uuid.NewString(), Username: "wiki-suspended-" + orgID[:8]}
	mustWikiExec(t, pool, `
		INSERT INTO users (id, username, display_name, status) VALUES
		($1, $2, 'Writer', 'active'), ($3, $4, 'Reader', 'active'),
		($5, $6, 'Team writer', 'active'), ($7, $8, 'Suspended', 'suspended')
	`, writer.ID, writer.Username, reader.ID, reader.Username,
		teamWriter.ID, teamWriter.Username, suspended.ID, suspended.Username)
	mustWikiExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, description, visibility, created_by)
		VALUES ($1, $2, 'Wiki Org', '', 'private', $3)
	`, orgID, "wiki-org-"+orgID[:8], writer.ID)
	mustWikiExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'member'), ($1, $3, 'member'), ($1, $4, 'member'), ($1, $5, 'member')
	`, orgID, writer.ID, reader.ID, teamWriter.ID, suspended.ID)
	mustWikiExec(t, pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, description, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, 'docs', 'Docs', '', 'private', $3, $4, 'main', $5)
	`, repoID, orgID, strings.ReplaceAll(repoID, "-", ""), "https://lore.invalid/"+repoID, writer.ID)
	mustWikiExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role)
		VALUES ($1, $2, 'write'), ($1, $3, 'read'), ($1, $4, 'write')
	`, repoID, writer.ID, reader.ID, suspended.ID)
	teamID := uuid.NewString()
	mustWikiExec(t, pool, `
		INSERT INTO teams (id, organization_id, slug, display_name, description, created_by)
		VALUES ($1, $2, 'docs', 'Docs', '', $3)
	`, teamID, orgID, writer.ID)
	mustWikiExec(t, pool, `
		INSERT INTO team_memberships (team_id, user_id, role) VALUES ($1, $2, 'member')
	`, teamID, teamWriter.ID)
	mustWikiExec(t, pool, `
		INSERT INTO team_repository_roles (team_id, repository_id, role, created_by)
		VALUES ($1, $2, 'write', $3)
	`, teamID, repoID, writer.ID)
	return wikiFixture{
		pool: pool, writer: writer, reader: reader, teamWriter: teamWriter, suspended: suspended,
		repository: RepositoryRef{ID: repoID, OrganizationID: orgID}, orgID: orgID, repoID: repoID,
	}
}

func assertWikiAuditAndOutbox(t *testing.T, fixture wikiFixture, pageID string) {
	t.Helper()
	var auditCount, outboxCount int
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM audit_events
		WHERE repository_id = $1 AND target_type = 'wiki_page' AND target_id = $2
	`, fixture.repoID, pageID).Scan(&auditCount); err != nil {
		t.Fatalf("count wiki audit events: %v", err)
	}
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM outbox_events
		WHERE topic LIKE 'wiki.%' AND payload ->> 'pageId' = $1
	`, pageID).Scan(&outboxCount); err != nil {
		t.Fatalf("count wiki outbox events: %v", err)
	}
	if auditCount != 3 || outboxCount != 3 {
		t.Fatalf("audit count = %d, outbox count = %d, want 3 each", auditCount, outboxCount)
	}
}

func assertWikiDatabaseTenantConstraint(t *testing.T, fixture wikiFixture, page Page) {
	t.Helper()
	otherRepoID := uuid.NewString()
	mustWikiExec(t, fixture.pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, description, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, 'other-docs', 'Other docs', '', 'private', $3, $4, 'main', $5)
	`, otherRepoID, fixture.orgID, strings.ReplaceAll(otherRepoID, "-", ""),
		"https://lore.invalid/"+otherRepoID, fixture.writer.ID)
	_, err := fixture.pool.Exec(context.Background(), `
		INSERT INTO repository_wiki_page_versions (
			page_id, repository_id, version, slug, title, body, edited_by
		) VALUES ($1, $2, 99, 'wrong', 'Wrong', '', $3)
	`, page.ID, otherRepoID, fixture.writer.ID)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "23503" {
		t.Fatalf("cross-repository version error = %v, want foreign key violation", err)
	}
}

func mustWikiExec(t *testing.T, pool *pgxpool.Pool, query string, arguments ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), query, arguments...); err != nil {
		t.Fatalf("execute wiki fixture query: %v", err)
	}
}
