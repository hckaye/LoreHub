package projects

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/database"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type projectFixture struct {
	pool   *pgxpool.Pool
	actor  platform.User
	reader platform.User
	repo   RepositoryRef
	repoID string
	orgID  string
}

func TestPostgresProjectBoardLifecycleAndTenantBoundary(t *testing.T) {
	fixture := openProjectFixture(t)
	ctx := context.Background()
	store := NewStore(fixture.pool)
	input := ProjectInput{Title: "Release board", Description: "Prepare the release", State: "open"}

	if _, err := store.Create(ctx, fixture.reader, fixture.repo, input); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("reader create error = %v, want forbidden", err)
	}
	project, err := store.Create(ctx, fixture.actor, fixture.repo, input)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if project.Number != 1 || len(project.Columns) != 3 || !project.ViewerCanWrite {
		t.Fatalf("created project = %+v", project)
	}
	issueID, otherIssueID := seedProjectContent(t, fixture)
	issueNumber := int64(1)
	project, err = store.CreateItem(ctx, fixture.actor, fixture.repo, project.Number, ItemInput{
		ColumnID: project.Columns[0].ID, Kind: "issue", IssueNumber: &issueNumber,
	})
	if err != nil {
		t.Fatalf("add issue: %v", err)
	}
	if len(project.Columns[0].Items) != 1 || project.Columns[0].Items[0].Title != "Repository issue" {
		t.Fatalf("issue card = %+v", project.Columns[0].Items)
	}
	mergeRequestNumber := int64(1)
	project, err = store.CreateItem(ctx, fixture.actor, fixture.repo, project.Number, ItemInput{
		ColumnID: project.Columns[0].ID, Kind: "merge_request", MergeRequestNumber: &mergeRequestNumber,
	})
	if err != nil {
		t.Fatalf("add pull request: %v", err)
	}
	if len(project.Columns[0].Items) != 2 || project.Columns[0].Items[1].Title != "Repository pull request" {
		t.Fatalf("pull request cards = %+v", project.Columns[0].Items)
	}
	if _, err := store.CreateItem(ctx, fixture.actor, fixture.repo, project.Number, ItemInput{
		ColumnID: project.Columns[1].ID, Kind: "issue", IssueNumber: &issueNumber,
	}); !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("duplicate issue error = %v, want conflict", err)
	}

	project, err = store.CreateItem(ctx, fixture.actor, fixture.repo, project.Number, ItemInput{
		ColumnID: project.Columns[1].ID, Kind: "draft", Title: "Publish release notes", Body: "Review first",
	})
	if err != nil {
		t.Fatalf("add draft: %v", err)
	}
	draftID := project.Columns[1].Items[0].ID
	targetColumn := project.Columns[2].ID
	project, err = store.UpdateItem(ctx, fixture.actor, fixture.repo, project.Number, draftID,
		ItemUpdate{ColumnID: &targetColumn})
	if err != nil {
		t.Fatalf("move draft: %v", err)
	}
	if len(project.Columns[1].Items) != 0 || len(project.Columns[2].Items) != 1 {
		t.Fatalf("moved project columns = %+v", project.Columns)
	}
	if err := store.DeleteColumn(
		ctx, fixture.actor, fixture.repo, project.Number, project.Columns[0].ID,
	); !errors.Is(err, ErrColumnNotEmpty) {
		t.Fatalf("delete non-empty column error = %v", err)
	}
	if err := store.DeleteColumn(ctx, fixture.actor, fixture.repo, project.Number, project.Columns[1].ID); err != nil {
		t.Fatalf("delete empty column: %v", err)
	}

	assertProjectTenantConstraint(t, fixture, project, issueID, otherIssueID)
	assertProjectAudit(t, fixture, project.ID)
	if _, err := store.Get(ctx, uuid.NewString(), project.Number); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("cross-repository get error = %v", err)
	}
	mustProjectExec(t, fixture.pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, fixture.actor.ID)
	projectTitle := input.Title
	if _, err := store.Update(ctx, fixture.actor, fixture.repo, project.Number, ProjectUpdate{
		Title: &projectTitle,
	}); !errors.Is(
		err, platform.ErrForbidden,
	) {
		t.Fatalf("suspended writer update error = %v, want forbidden", err)
	}
	mustProjectExec(t, fixture.pool, `UPDATE users SET status = 'active' WHERE id = $1`, fixture.actor.ID)
	if err := store.Delete(ctx, fixture.actor, fixture.repo, project.Number); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	if _, err := store.Get(ctx, fixture.repoID, project.Number); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("deleted project get error = %v, want not found", err)
	}
}

func openProjectFixture(t *testing.T) projectFixture {
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
	fixture := seedProjectFixture(t, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, fixture.orgID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id IN ($1, $2)`,
			fixture.actor.ID, fixture.reader.ID)
	})
	return fixture
}

func seedProjectFixture(t *testing.T, pool *pgxpool.Pool) projectFixture {
	t.Helper()
	orgID := uuid.NewString()
	repoID := uuid.NewString()
	actor := platform.User{ID: uuid.NewString(), Username: "project-writer-" + orgID[:8]}
	reader := platform.User{ID: uuid.NewString(), Username: "project-reader-" + orgID[:8]}
	mustProjectExec(t, pool, `
		INSERT INTO users (id, username, display_name) VALUES ($1, $2, 'Writer'), ($3, $4, 'Reader')
	`, actor.ID, actor.Username, reader.ID, reader.Username)
	mustProjectExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, description, visibility, created_by)
		VALUES ($1, $2, 'Project Org', '', 'private', $3)
	`, orgID, "project-org-"+orgID[:8], actor.ID)
	mustProjectExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'member'), ($1, $3, 'member')
	`, orgID, actor.ID, reader.ID)
	mustProjectExec(t, pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, description, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, 'game', 'Game', '', 'private', $3, $4, 'main', $5)
	`, repoID, orgID, compactUUID(repoID), "https://lore.invalid/"+repoID, actor.ID)
	mustProjectExec(t, pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, repoID)
	mustProjectExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role)
		VALUES ($1, $2, 'write'), ($1, $3, 'read')
	`, repoID, actor.ID, reader.ID)
	return projectFixture{
		pool: pool, actor: actor, reader: reader, repo: RepositoryRef{ID: repoID, OrganizationID: orgID},
		repoID: repoID, orgID: orgID,
	}
}

func seedProjectContent(t *testing.T, fixture projectFixture) (string, string) {
	t.Helper()
	issueID := uuid.NewString()
	mustProjectExec(t, fixture.pool, `
		INSERT INTO issues (id, repository_id, number, title, body, author_id)
		VALUES ($1, $2, 1, 'Repository issue', '', $3)
	`, issueID, fixture.repoID, fixture.actor.ID)
	mustProjectExec(t, fixture.pool, `
		INSERT INTO merge_requests (
			id, repository_id, number, title, body, source_branch, target_branch,
			source_revision, target_revision, author_id
		) VALUES ($1, $2, 1, 'Repository pull request', '', 'feature', 'main', 'source', 'target', $3)
	`, uuid.NewString(), fixture.repoID, fixture.actor.ID)
	otherRepoID := uuid.NewString()
	mustProjectExec(t, fixture.pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, description, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, 'other', 'Other', '', 'private', $3, $4, 'main', $5)
	`, otherRepoID, fixture.orgID, compactUUID(otherRepoID), "https://lore.invalid/"+otherRepoID, fixture.actor.ID)
	mustProjectExec(t, fixture.pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, otherRepoID)
	otherIssueID := uuid.NewString()
	mustProjectExec(t, fixture.pool, `
		INSERT INTO issues (id, repository_id, number, title, body, author_id)
		VALUES ($1, $2, 1, 'Other issue', '', $3)
	`, otherIssueID, otherRepoID, fixture.actor.ID)
	return issueID, otherIssueID
}

func assertProjectTenantConstraint(
	t *testing.T,
	fixture projectFixture,
	project Project,
	issueID string,
	otherIssueID string,
) {
	t.Helper()
	_, err := fixture.pool.Exec(context.Background(), `
		INSERT INTO project_items (
			id, project_id, repository_id, column_id, kind, issue_id, position, created_by
		) VALUES ($1, $2, $3, $4, 'issue', $5, 9000, $6)
	`, uuid.NewString(), project.ID, fixture.repoID, project.Columns[0].ID, otherIssueID, fixture.actor.ID)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "23503" {
		t.Fatalf("cross-repository card error = %v, want foreign key violation", err)
	}
	var storedIssueID string
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT issue_id FROM project_items WHERE project_id = $1 AND kind = 'issue'
	`, project.ID).Scan(&storedIssueID); err != nil {
		t.Fatalf("read stored issue card: %v", err)
	}
	if storedIssueID != issueID {
		t.Fatalf("stored issue id = %s, want %s", storedIssueID, issueID)
	}
}

func assertProjectAudit(t *testing.T, fixture projectFixture, projectID string) {
	t.Helper()
	var auditCount, outboxCount int
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM audit_events
		WHERE repository_id = $1 AND target_id IN ($2, $3)
	`, fixture.repoID, projectID, "").Scan(&auditCount); err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if auditCount < 1 {
		t.Fatalf("audit count = %d, want at least one", auditCount)
	}
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM outbox_events
		WHERE topic = 'project.updated' AND payload ->> 'id' = $1
	`, projectID).Scan(&outboxCount); err != nil {
		t.Fatalf("count outbox events: %v", err)
	}
	if outboxCount < 1 {
		t.Fatalf("outbox count = %d, want at least one", outboxCount)
	}
}

func mustProjectExec(t *testing.T, pool *pgxpool.Pool, statement string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), statement, args...); err != nil {
		t.Fatalf("execute fixture SQL: %v", err)
	}
}

func compactUUID(value string) string {
	return strings.ReplaceAll(value, "-", "")
}
