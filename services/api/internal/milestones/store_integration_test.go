package milestones

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/database"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type milestoneFixture struct {
	pool     *pgxpool.Pool
	writer   platform.User
	triager  platform.User
	reader   platform.User
	repo     RepositoryRef
	orgID    string
	repoID   string
	issueID  string
	otherID  string
	otherRef RepositoryRef
}

func TestPostgresMilestoneLifecycleAndTenantBoundary(t *testing.T) {
	fixture := openMilestoneFixture(t)
	ctx := context.Background()
	store := NewStore(fixture.pool)
	dueOn := "2026-12-31"
	input := CreateInput{Title: "Version 2", Description: "Release scope", DueOn: &dueOn}
	if _, err := store.Create(ctx, fixture.reader, fixture.repo, input); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("reader create error = %v, want forbidden", err)
	}
	milestone, err := store.Create(ctx, fixture.writer, fixture.repo, input)
	if err != nil {
		t.Fatalf("create milestone: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fixture.pool.Exec(context.Background(), `
			DELETE FROM outbox_events WHERE event_key LIKE $1 OR event_key LIKE $2
		`, milestone.ID+"%", fixture.issueID+"%")
	})
	if milestone.Number != 1 || milestone.State != "open" || milestone.Version != 1 || !milestone.ViewerCanWrite {
		t.Fatalf("created milestone = %#v", milestone)
	}
	if _, err := store.Create(ctx, fixture.writer, fixture.repo,
		CreateInput{Title: "version 2"}); !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("duplicate title error = %v, want conflict", err)
	}

	assigned, err := store.AssignIssue(ctx, fixture.triager, fixture.repo, 1, milestone.Number)
	if err != nil || assigned.ID != milestone.ID {
		t.Fatalf("assign issue result = %#v, error = %v", assigned, err)
	}
	if _, err := store.AssignIssue(ctx, fixture.reader, fixture.repo, 1, milestone.Number); !errors.Is(
		err, platform.ErrForbidden,
	) {
		t.Fatalf("reader assign error = %v, want forbidden", err)
	}
	loaded, err := store.Get(ctx, fixture.repoID, milestone.Number)
	if err != nil || loaded.OpenIssueCount != 1 || loaded.ClosedIssueCount != 0 {
		t.Fatalf("loaded milestone = %#v, error = %v", loaded, err)
	}
	assertCrossRepositoryMilestoneConstraint(t, fixture, milestone.ID)

	closed := "closed"
	if _, err := store.Update(ctx, fixture.writer, fixture.repo, milestone.Number, UpdateInput{
		State: &closed, ExpectedVersion: 99,
	}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale close error = %v, want version conflict", err)
	}
	milestone, err = store.Update(ctx, fixture.writer, fixture.repo, milestone.Number, UpdateInput{
		State: &closed, ExpectedVersion: milestone.Version,
	})
	if err != nil || milestone.State != "closed" || milestone.ClosedBy == nil || milestone.ClosedAt == nil {
		t.Fatalf("closed milestone = %#v, error = %v", milestone, err)
	}
	closedBy := *milestone.ClosedBy
	closedAt := *milestone.ClosedAt
	updatedTitle := "Version 2.0"
	milestone, err = store.Update(ctx, fixture.writer, fixture.repo, milestone.Number, UpdateInput{
		Title: &updatedTitle, ExpectedVersion: milestone.Version,
	})
	if err != nil || milestone.ClosedBy == nil || *milestone.ClosedBy != closedBy ||
		milestone.ClosedAt == nil || !milestone.ClosedAt.Equal(closedAt) {
		t.Fatalf("closed metadata changed: milestone = %#v, error = %v", milestone, err)
	}

	open := "open"
	milestone, err = store.Update(ctx, fixture.writer, fixture.repo, milestone.Number, UpdateInput{
		State: &open, DueOnSet: true, ExpectedVersion: milestone.Version,
	})
	if err != nil || milestone.State != "open" || milestone.DueOn != nil ||
		milestone.ClosedBy != nil || milestone.ClosedAt != nil {
		t.Fatalf("reopened milestone = %#v, error = %v", milestone, err)
	}
	mustMilestoneExec(t, fixture.pool, `UPDATE issues SET state = 'closed' WHERE id = $1`, fixture.issueID)
	loaded, err = store.Get(ctx, fixture.repoID, milestone.Number)
	if err != nil || loaded.OpenIssueCount != 0 || loaded.ClosedIssueCount != 1 {
		t.Fatalf("closed issue counts = %#v, error = %v", loaded, err)
	}

	mustMilestoneExec(t, fixture.pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, fixture.writer.ID)
	if err := store.Delete(
		ctx, fixture.writer, fixture.repo, milestone.Number, milestone.Version,
	); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("suspended delete error = %v, want forbidden", err)
	}
	mustMilestoneExec(t, fixture.pool, `UPDATE users SET status = 'active' WHERE id = $1`, fixture.writer.ID)
	if err := store.Delete(ctx, fixture.writer, fixture.repo, milestone.Number, milestone.Version); err != nil {
		t.Fatalf("delete milestone: %v", err)
	}
	var milestoneID *string
	if err := fixture.pool.QueryRow(ctx, `SELECT milestone_id FROM issues WHERE id = $1`,
		fixture.issueID).Scan(&milestoneID); err != nil || milestoneID != nil {
		t.Fatalf("issue milestone after delete = %v, error = %v", milestoneID, err)
	}
	if _, err := store.Get(ctx, fixture.repoID, milestone.Number); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("deleted milestone error = %v, want not found", err)
	}
	assertMilestoneAuditAndOutbox(t, fixture, milestone.ID)
}

func openMilestoneFixture(t *testing.T) milestoneFixture {
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
	fixture := seedMilestoneFixture(t, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, fixture.orgID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id IN ($1, $2, $3)`,
			fixture.writer.ID, fixture.triager.ID, fixture.reader.ID)
	})
	return fixture
}

func seedMilestoneFixture(t *testing.T, pool *pgxpool.Pool) milestoneFixture {
	t.Helper()
	orgID := uuid.NewString()
	repoID := uuid.NewString()
	otherID := uuid.NewString()
	writer := platform.User{ID: uuid.NewString(), Username: "milestone-writer-" + orgID[:8]}
	triager := platform.User{ID: uuid.NewString(), Username: "milestone-triager-" + orgID[:8]}
	reader := platform.User{ID: uuid.NewString(), Username: "milestone-reader-" + orgID[:8]}
	issueID := uuid.NewString()
	mustMilestoneExec(t, pool, `
		INSERT INTO users (id, username, display_name)
		VALUES ($1, $2, 'Writer'), ($3, $4, 'Triager'), ($5, $6, 'Reader')
	`, writer.ID, writer.Username, triager.ID, triager.Username, reader.ID, reader.Username)
	mustMilestoneExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, description, visibility, created_by)
		VALUES ($1, $2, 'Milestone Org', '', 'private', $3)
	`, orgID, "milestone-org-"+orgID[:8], writer.ID)
	mustMilestoneExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'member'), ($1, $3, 'member'), ($1, $4, 'member')
	`, orgID, writer.ID, triager.ID, reader.ID)
	insertMilestoneRepository(t, pool, repoID, orgID, "game", writer.ID)
	insertMilestoneRepository(t, pool, otherID, orgID, "other", writer.ID)
	mustMilestoneExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role)
		VALUES ($1, $2, 'write'), ($1, $3, 'triage'), ($1, $4, 'read')
	`, repoID, writer.ID, triager.ID, reader.ID)
	mustMilestoneExec(t, pool, `
		INSERT INTO issues (id, repository_id, number, title, body, author_id)
		VALUES ($1, $2, 1, 'Ship version 2', '', $3)
	`, issueID, repoID, writer.ID)
	return milestoneFixture{
		pool: pool, writer: writer, triager: triager, reader: reader,
		repo: RepositoryRef{ID: repoID, OrganizationID: orgID}, orgID: orgID,
		repoID: repoID, issueID: issueID, otherID: otherID,
		otherRef: RepositoryRef{ID: otherID, OrganizationID: orgID},
	}
}

func insertMilestoneRepository(
	t *testing.T,
	pool *pgxpool.Pool,
	repositoryID string,
	organizationID string,
	slug string,
	actorID string,
) {
	t.Helper()
	mustMilestoneExec(t, pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, description, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, $3, $3, '', 'private', $4, $5, 'main', $6)
	`, repositoryID, organizationID, slug, compactMilestoneUUID(repositoryID),
		"https://lore.invalid/"+repositoryID, actorID)
	mustMilestoneExec(t, pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, repositoryID)
}

func assertCrossRepositoryMilestoneConstraint(
	t *testing.T,
	fixture milestoneFixture,
	milestoneID string,
) {
	t.Helper()
	otherIssueID := uuid.NewString()
	mustMilestoneExec(t, fixture.pool, `
		INSERT INTO issues (id, repository_id, number, title, body, author_id)
		VALUES ($1, $2, 1, 'Other issue', '', $3)
	`, otherIssueID, fixture.otherID, fixture.writer.ID)
	_, err := fixture.pool.Exec(context.Background(), `
		UPDATE issues SET milestone_id = $1 WHERE id = $2
	`, milestoneID, otherIssueID)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "23503" {
		t.Fatalf("cross-repository milestone error = %v, want foreign key violation", err)
	}
}

func assertMilestoneAuditAndOutbox(t *testing.T, fixture milestoneFixture, milestoneID string) {
	t.Helper()
	var auditCount, outboxCount int
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM audit_events
		WHERE repository_id = $1 AND target_id IN ($2, $3)
	`, fixture.repoID, milestoneID, fixture.issueID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM outbox_events
		WHERE event_key LIKE $1 OR event_key LIKE $2
	`, milestoneID+"%", fixture.issueID+"%").Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if auditCount < 6 || outboxCount < 6 {
		t.Fatalf("audit count = %d, outbox count = %d", auditCount, outboxCount)
	}
}

func mustMilestoneExec(t *testing.T, pool *pgxpool.Pool, query string, arguments ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), query, arguments...); err != nil {
		t.Fatal(err)
	}
}

func compactMilestoneUUID(value string) string {
	result := make([]byte, 0, 32)
	for _, character := range []byte(value) {
		if character != '-' {
			result = append(result, character)
		}
	}
	return string(result)
}
