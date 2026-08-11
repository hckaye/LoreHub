package releases

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

type releaseFixture struct {
	pool   *pgxpool.Pool
	actor  platform.User
	reader platform.User
	repo   RepositoryRef
	orgID  string
	repoID string
}

func TestPostgresReleaseLifecycleAndTenantBoundary(t *testing.T) {
	fixture := openReleaseFixture(t)
	ctx := context.Background()
	store := NewStore(fixture.pool)
	input := CreateInput{
		TagName: "v1.0.0", Title: "Version 1", Notes: "First stable release",
		SourceBranch: "main", Revision: testRevision, State: "draft",
	}
	if _, err := store.Create(ctx, fixture.reader, fixture.repo, input); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("reader create error = %v, want forbidden", err)
	}
	release, err := store.Create(ctx, fixture.actor, fixture.repo, input)
	if err != nil {
		t.Fatalf("create release: %v", err)
	}
	if release.State != "draft" || release.Version != 1 || !release.ViewerCanWrite {
		t.Fatalf("created release = %#v", release)
	}
	publicPage, err := store.List(ctx, fixture.repoID, false, 1, 20)
	if err != nil || len(publicPage.Releases) != 0 {
		t.Fatalf("public draft list = %#v, error = %v", publicPage, err)
	}
	writerPage, err := store.List(ctx, fixture.repoID, true, 1, 20)
	if err != nil || len(writerPage.Releases) != 1 {
		t.Fatalf("writer draft list = %#v, error = %v", writerPage, err)
	}

	assetInput := AssetInput{
		Name: "game.zip", ExternalURL: "https://downloads.example.test/game.zip",
		ExpectedVersion: release.Version,
	}
	release, err = store.AddAsset(ctx, fixture.actor, fixture.repo, release.ID, assetInput)
	if err != nil {
		t.Fatalf("add asset: %v", err)
	}
	if release.Version != 2 || len(release.Assets) != 1 || release.Assets[0].Name != "game.zip" {
		t.Fatalf("release after asset = %#v", release)
	}
	if _, err := store.Publish(
		ctx, fixture.actor, fixture.repo, release.ID, 1,
	); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale publish error = %v, want version conflict", err)
	}
	release, err = store.Publish(ctx, fixture.actor, fixture.repo, release.ID, release.Version)
	if err != nil {
		t.Fatalf("publish release: %v", err)
	}
	if release.State != "published" || release.Version != 3 || release.PublishedAt == nil {
		t.Fatalf("published release = %#v", release)
	}
	publicPage, err = store.List(ctx, fixture.repoID, false, 1, 20)
	if err != nil || len(publicPage.Releases) != 1 {
		t.Fatalf("published page = %#v, error = %v", publicPage, err)
	}

	updatedTitle := "Version 1.0"
	release, err = store.Update(ctx, fixture.actor, fixture.repo, release.ID, UpdateInput{
		Title: &updatedTitle, ExpectedVersion: release.Version,
	})
	if err != nil || release.Title != updatedTitle || release.Version != 4 {
		t.Fatalf("updated release = %#v, error = %v", release, err)
	}
	assetID := release.Assets[0].ID
	release, err = store.DeleteAsset(
		ctx, fixture.actor, fixture.repo, release.ID, assetID, release.Version,
	)
	if err != nil || len(release.Assets) != 0 || release.Version != 5 {
		t.Fatalf("release after asset deletion = %#v, error = %v", release, err)
	}
	assertReleaseTenantConstraint(t, fixture, release.ID)
	assertReleaseAuditAndOutbox(t, fixture, release.ID)

	mustReleaseExec(t, fixture.pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, fixture.actor.ID)
	if err := store.Delete(
		ctx, fixture.actor, fixture.repo, release.ID, release.Version,
	); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("suspended delete error = %v, want forbidden", err)
	}
	mustReleaseExec(t, fixture.pool, `UPDATE users SET status = 'active' WHERE id = $1`, fixture.actor.ID)
	if err := store.Delete(ctx, fixture.actor, fixture.repo, release.ID, release.Version); err != nil {
		t.Fatalf("delete release: %v", err)
	}
	if _, err := store.Get(ctx, fixture.repoID, release.ID, true); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("deleted release error = %v, want not found", err)
	}
}

func openReleaseFixture(t *testing.T) releaseFixture {
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
	fixture := seedReleaseFixture(t, pool)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, fixture.orgID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id IN ($1, $2)`,
			fixture.actor.ID, fixture.reader.ID)
	})
	return fixture
}

func seedReleaseFixture(t *testing.T, pool *pgxpool.Pool) releaseFixture {
	t.Helper()
	orgID := uuid.NewString()
	repoID := uuid.NewString()
	actor := platform.User{ID: uuid.NewString(), Username: "release-writer-" + orgID[:8]}
	reader := platform.User{ID: uuid.NewString(), Username: "release-reader-" + orgID[:8]}
	mustReleaseExec(t, pool, `
		INSERT INTO users (id, username, display_name)
		VALUES ($1, $2, 'Writer'), ($3, $4, 'Reader')
	`, actor.ID, actor.Username, reader.ID, reader.Username)
	mustReleaseExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, description, visibility, created_by)
		VALUES ($1, $2, 'Release Org', '', 'private', $3)
	`, orgID, "release-org-"+orgID[:8], actor.ID)
	mustReleaseExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'member'), ($1, $3, 'member')
	`, orgID, actor.ID, reader.ID)
	mustReleaseExec(t, pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, description, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, 'game', 'Game', '', 'private', $3, $4, 'main', $5)
	`, repoID, orgID, compactReleaseUUID(repoID), "https://lore.invalid/"+repoID, actor.ID)
	mustReleaseExec(t, pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, repoID)
	mustReleaseExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role)
		VALUES ($1, $2, 'write'), ($1, $3, 'read')
	`, repoID, actor.ID, reader.ID)
	return releaseFixture{
		pool: pool, actor: actor, reader: reader,
		repo: RepositoryRef{ID: repoID, OrganizationID: orgID}, orgID: orgID, repoID: repoID,
	}
}

func assertReleaseTenantConstraint(t *testing.T, fixture releaseFixture, releaseID string) {
	t.Helper()
	otherRepoID := uuid.NewString()
	mustReleaseExec(t, fixture.pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, description, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, 'other', 'Other', '', 'private', $3, $4, 'main', $5)
	`, otherRepoID, fixture.orgID, compactReleaseUUID(otherRepoID),
		"https://lore.invalid/"+otherRepoID, fixture.actor.ID)
	_, err := fixture.pool.Exec(context.Background(), `
		INSERT INTO release_asset_links (
			id, release_id, repository_id, name, external_url, created_by
		) VALUES ($1, $2, $3, 'cross.zip', 'https://example.test/cross.zip', $4)
	`, uuid.NewString(), releaseID, otherRepoID, fixture.actor.ID)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "23503" {
		t.Fatalf("cross-repository asset error = %v, want foreign key violation", err)
	}
}

func assertReleaseAuditAndOutbox(t *testing.T, fixture releaseFixture, releaseID string) {
	t.Helper()
	var auditCount, outboxCount int
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM audit_events
		WHERE repository_id = $1 AND target_id = $2
	`, fixture.repoID, releaseID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM outbox_events
		WHERE topic LIKE 'release.%' AND payload::text LIKE $1
	`, "%"+releaseID+"%").Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if auditCount < 3 || outboxCount < 3 {
		t.Fatalf("audit count = %d, outbox count = %d", auditCount, outboxCount)
	}
}

func mustReleaseExec(t *testing.T, pool *pgxpool.Pool, query string, arguments ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), query, arguments...); err != nil {
		t.Fatal(err)
	}
}

func compactReleaseUUID(value string) string {
	result := make([]byte, 0, 32)
	for _, character := range []byte(value) {
		if character != '-' {
			result = append(result, character)
		}
	}
	return string(result)
}
