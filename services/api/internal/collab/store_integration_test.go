package collab

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/database"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

// integrationEnv returns a connection pool when DATABASE_URL is configured and
// the schema can be migrated. Tests that require PostgreSQL skip otherwise.
func integrationEnv(t *testing.T) (*pgxpool.Pool, *store) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set; skipping PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, url, 5*time.Second)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool, &store{pool: pool, ensurer: platform.NewStore(pool)}
}

// integrationFixture seeds an organization, repository and users with adjustable
// membership roles, returning identifiers plus a cleanup that removes the org
// (cascading to all dependent rows).
type integrationFixture struct {
	pool      *pgxpool.Pool
	orgID     string
	repoID    string
	ownerSlug string
	repoSlug  string
	alice     platform.User
	bob       platform.User
	carol     platform.User
}

func setupFixture(t *testing.T, pool *pgxpool.Pool, visibility string, repoRole string) integrationFixture {
	t.Helper()
	ctx := context.Background()
	orgID := uuidNew()
	ownerSlug := "it-" + orgID[:8]
	repoSlug := "repo-" + orgID[:8]
	repoID := uuidNew()
	alice := platform.User{ID: uuidNew(), Username: "alice-" + orgID[:8], DisplayName: "Alice"}
	bob := platform.User{ID: uuidNew(), Username: "bob-" + orgID[:8], DisplayName: "Bob"}
	carol := platform.User{ID: uuidNew(), Username: "carol-" + orgID[:8], DisplayName: "Carol"}
	mustExec(t, ctx, pool, `
		INSERT INTO users (id, username, display_name) VALUES ($1,$2,$3),($4,$5,$6),($7,$8,$9)
	`, alice.ID, alice.Username, alice.DisplayName,
		bob.ID, bob.Username, bob.DisplayName,
		carol.ID, carol.Username, carol.DisplayName)
	mustExec(t, ctx, pool, `
		INSERT INTO organizations (id, slug, display_name, description, visibility, created_by)
		VALUES ($1, $2, 'IT Org', '', 'public', $3)
	`, orgID, ownerSlug, alice.ID)
	mustExec(t, ctx, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role) VALUES
		($1,$2,'owner'),($1,$3,'member')
	`, orgID, alice.ID, carol.ID)
	mustExec(t, ctx, pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, description, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1,$2,$3,'Repo','', $4, $5, $6, 'main', $7)
	`, repoID, orgID, repoSlug, visibility, "lore-"+orgID[:8], "http://lore.test/"+orgID, alice.ID)
	mustExec(t, ctx, pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, repoID)
	if repoRole != "" {
		mustExec(t, ctx, pool, `
			INSERT INTO repository_memberships (repository_id, user_id, role) VALUES ($1,$2,$3)
		`, repoID, bob.ID, repoRole)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM organizations WHERE id = $1`, orgID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id IN ($1,$2,$3)`,
			alice.ID, bob.ID, carol.ID)
	})
	return integrationFixture{
		pool: pool, orgID: orgID, repoID: repoID,
		ownerSlug: ownerSlug, repoSlug: repoSlug,
		alice: alice, bob: bob, carol: carol,
	}
}

func uuidNew() string {
	return uuidArg()
}

func mustExec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec fixture sql: %v", err)
	}
}

func TestIntegrationLookupRepositoryVisibility(t *testing.T) {
	pool, s := integrationEnv(t)
	ctx := context.Background()
	pub := setupFixture(t, pool, "public", "")
	priv := setupFixture(t, pool, "private", "read")

	if _, err := s.LookupRepository(ctx, nil, pub.ownerSlug, pub.repoSlug); err != nil {
		t.Fatalf("anonymous public lookup: %v", err)
	}
	if _, err := s.LookupRepository(ctx, nil, priv.ownerSlug, priv.repoSlug); err == nil {
		t.Fatal("anonymous private lookup should fail")
	}
	if _, err := s.LookupRepository(ctx, &priv.bob, priv.ownerSlug, priv.repoSlug); err != nil {
		t.Fatalf("member private lookup: %v", err)
	}
	stranger := platform.User{ID: uuidNew(), Username: "stranger-" + priv.orgID[:8]}
	if _, err := s.LookupRepository(ctx, &stranger, priv.ownerSlug, priv.repoSlug); err == nil {
		t.Fatal("stranger private lookup should fail")
	}
}

func TestIntegrationRepositoryPermission(t *testing.T) {
	pool, s := integrationEnv(t)
	ctx := context.Background()
	fix := setupFixture(t, pool, "private", "write")
	repo, err := s.LookupRepository(ctx, &fix.bob, fix.ownerSlug, fix.repoSlug)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	bobAccess, err := s.RepositoryPermission(ctx, fix.bob, repo)
	if err != nil {
		t.Fatalf("bob permission: %v", err)
	}
	if bobAccess.Permission != PermWrite {
		t.Errorf("bob permission = %v, want PermWrite", bobAccess.Permission)
	}
	aliceAccess, err := s.RepositoryPermission(ctx, fix.alice, repo)
	if err != nil {
		t.Fatalf("alice permission: %v", err)
	}
	if !aliceAccess.OrgOwner || aliceAccess.Permission != PermAdmin {
		t.Errorf("alice access = %+v, want org owner/admin", aliceAccess)
	}
	carolAccess, err := s.RepositoryPermission(ctx, fix.carol, repo)
	if err != nil {
		t.Fatalf("carol permission: %v", err)
	}
	if carolAccess.Permission != PermRead {
		t.Errorf("carol (org member) permission = %v, want PermRead", carolAccess.Permission)
	}
}

func TestIntegrationIssueUpdatePermissionAndPrecondition(t *testing.T) {
	pool, s := integrationEnv(t)
	ctx := context.Background()
	fix := setupFixture(t, pool, "public", "triage")
	number := seedIssue(t, ctx, pool, fix, fix.alice.ID, "open")

	// Author can edit even without triage (carol has no repo role, only org member read).
	_, err := s.UpdateIssue(ctx, fix.alice, fix.repoID, number, UpdateIssueInput{
		Title: ptrString("Edited by author"),
	})
	if err != nil {
		t.Fatalf("author edit: %v", err)
	}
	// Bob (triage) can close.
	closed, err := s.UpdateIssue(ctx, fix.bob, fix.repoID, number, UpdateIssueInput{State: ptrString("closed")})
	if err != nil {
		t.Fatalf("triage close: %v", err)
	}
	if closed.State != "closed" || closed.ClosedAt == nil {
		t.Fatalf("expected closed issue, got %+v", closed)
	}
	// Carol (read only, not author) cannot edit.
	_, err = s.UpdateIssue(ctx, fix.carol, fix.repoID, number, UpdateIssueInput{Title: ptrString("nope")})
	if !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("carol edit expected ErrForbidden, got %v", err)
	}
	// Optimistic concurrency: stale IfMatch fails.
	current, err := s.GetIssue(ctx, fix.repoID, number)
	if err != nil {
		t.Fatalf("get issue: %v", err)
	}
	stale := current.UpdatedAt.Add(-time.Minute)
	_, err = s.UpdateIssue(ctx, fix.alice, fix.repoID, number, UpdateIssueInput{
		Title: ptrString("stale"), IfMatch: &stale,
	})
	if !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("stale if-match expected ErrPreconditionFailed, got %v", err)
	}
}

func TestIntegrationIssueComments(t *testing.T) {
	pool, s := integrationEnv(t)
	ctx := context.Background()
	fix := setupFixture(t, pool, "public", "")
	number := seedIssue(t, ctx, pool, fix, fix.alice.ID, "open")

	comment, err := s.CreateIssueComment(ctx, fix.alice, fix.repoID, number, "first comment")
	if err != nil {
		t.Fatalf("create comment: %v", err)
	}
	if comment.Author != fix.alice.Username {
		t.Fatalf("comment author = %q, want %q", comment.Author, fix.alice.Username)
	}
	// Author can edit.
	edited, err := s.UpdateIssueComment(ctx, fix.alice, comment.ID, "edited body")
	if err != nil {
		t.Fatalf("edit own comment: %v", err)
	}
	if edited.Body != "edited body" || edited.EditedAt == nil {
		t.Fatalf("edited comment = %+v", edited)
	}
	// Carol (not author, read only) cannot edit.
	_, err = s.UpdateIssueComment(ctx, fix.carol, comment.ID, "hacked")
	if !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("carol edit comment expected ErrForbidden, got %v", err)
	}
	// Author can delete.
	if err := s.DeleteIssueComment(ctx, fix.alice, comment.ID); err != nil {
		t.Fatalf("delete comment: %v", err)
	}
	if err := s.DeleteIssueComment(ctx, fix.alice, comment.ID); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("second delete expected ErrNotFound, got %v", err)
	}
}

func TestIntegrationLabels(t *testing.T) {
	pool, s := integrationEnv(t)
	ctx := context.Background()
	fix := setupFixture(t, pool, "public", "triage")

	// Carol (read) cannot create labels.
	_, err := s.CreateLabel(ctx, fix.carol, fix.repoID, LabelInput{Name: "bug", Color: "ff0000"})
	if !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("carol create label expected ErrForbidden, got %v", err)
	}
	// Alice (org owner/admin) can create.
	label, err := s.CreateLabel(ctx, fix.alice, fix.repoID, LabelInput{Name: "bug", Color: "ff0000"})
	if err != nil {
		t.Fatalf("create label: %v", err)
	}
	// Duplicate name conflicts.
	_, err = s.CreateLabel(ctx, fix.alice, fix.repoID, LabelInput{Name: "bug", Color: "00ff00"})
	if !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("duplicate label expected ErrConflict, got %v", err)
	}
	// Apply label to issue (bob triage).
	number := seedIssue(t, ctx, pool, fix, fix.alice.ID, "open")
	_, applied, err := s.ApplyLabel(ctx, fix.bob, fix.repoID, number, label.ID)
	if err != nil {
		t.Fatalf("apply label: %v", err)
	}
	if !applied {
		t.Fatal("first apply should report applied=true")
	}
	_, appliedAgain, err := s.ApplyLabel(ctx, fix.bob, fix.repoID, number, label.ID)
	if err != nil {
		t.Fatalf("reapply label: %v", err)
	}
	if appliedAgain {
		t.Fatal("duplicate apply should report applied=false")
	}
	// Carol (read) cannot apply labels.
	_, _, err = s.ApplyLabel(ctx, fix.carol, fix.repoID, number, label.ID)
	if !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("carol apply label expected ErrForbidden, got %v", err)
	}
	// Remove label idempotent.
	if err := s.RemoveLabel(ctx, fix.bob, fix.repoID, number, label.ID); err != nil {
		t.Fatalf("remove label: %v", err)
	}
	if err := s.RemoveLabel(ctx, fix.bob, fix.repoID, number, label.ID); err != nil {
		t.Fatalf("idempotent remove label: %v", err)
	}
	// Apply to missing issue returns 404.
	_, _, err = s.ApplyLabel(ctx, fix.bob, fix.repoID, 9999, label.ID)
	if !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("apply to missing issue expected ErrNotFound, got %v", err)
	}
}

func TestIntegrationReviews(t *testing.T) {
	pool, s := integrationEnv(t)
	ctx := context.Background()
	fix := setupFixture(t, pool, "public", "read")
	number := seedMergeRequest(t, ctx, pool, fix, fix.alice.ID, "rev-1")

	// Author cannot review own MR.
	_, err := s.CreateReview(ctx, fix.alice, fix.repoID, number, ReviewInput{Decision: "approved"})
	if !errors.Is(err, ErrCannotReviewOwn) {
		t.Fatalf("self review expected ErrCannotReviewOwn, got %v", err)
	}
	// Bob can review.
	review, err := s.CreateReview(ctx, fix.bob, fix.repoID, number,
		ReviewInput{Decision: "changes_requested", Body: "please fix"})
	if err != nil {
		t.Fatalf("create review: %v", err)
	}
	if review.SourceRevision != "rev-1" {
		t.Fatalf("review revision = %q, want rev-1", review.SourceRevision)
	}
	// Upsert: bob reviews again on same revision updates the decision.
	review2, err := s.CreateReview(ctx, fix.bob, fix.repoID, number, ReviewInput{Decision: "approved"})
	if err != nil {
		t.Fatalf("upsert review: %v", err)
	}
	summary, err := s.ListReviews(ctx, fix.repoID, number)
	if err != nil {
		t.Fatalf("list reviews: %v", err)
	}
	if summary.Approvals != 1 || summary.ChangeRequests != 0 {
		t.Fatalf("aggregate = %+v, want 1 approval 0 changes", summary)
	}
	if len(summary.CurrentReviews) != 1 {
		t.Fatalf("current reviews = %d, want 1 (upserted)", len(summary.CurrentReviews))
	}
	if review2.Decision != "approved" {
		t.Fatalf("upserted decision = %q, want approved", review2.Decision)
	}
}

func TestIntegrationBranchRules(t *testing.T) {
	pool, s := integrationEnv(t)
	ctx := context.Background()
	fix := setupFixture(t, pool, "public", "write")

	// Bob (write) cannot create branch rules.
	_, err := s.CreateBranchRule(ctx, fix.bob, fix.repoID, BranchRuleInput{Pattern: "main", RequiredApprovals: 1})
	if !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("bob create rule expected ErrForbidden, got %v", err)
	}
	// Alice (org owner/admin) can.
	rule, err := s.CreateBranchRule(ctx, fix.alice, fix.repoID, BranchRuleInput{Pattern: "main", RequiredApprovals: 2})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	// Duplicate pattern conflicts.
	_, err = s.CreateBranchRule(ctx, fix.alice, fix.repoID, BranchRuleInput{Pattern: "main", RequiredApprovals: 0})
	if !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("duplicate rule expected ErrConflict, got %v", err)
	}
	// Update and delete.
	updated, err := s.UpdateBranchRule(ctx, fix.alice, rule.ID,
		BranchRuleInput{Pattern: "release/*", RequiredApprovals: 3})
	if err != nil {
		t.Fatalf("update rule: %v", err)
	}
	if updated.Pattern != "release/*" || updated.RequiredApprovals != 3 {
		t.Fatalf("updated rule = %+v", updated)
	}
	if err := s.DeleteBranchRule(ctx, fix.alice, rule.ID); err != nil {
		t.Fatalf("delete rule: %v", err)
	}
}

func seedIssue(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fix integrationFixture,
	authorID, state string,
) int64 {
	t.Helper()
	var number int64
	err := pool.QueryRow(ctx, `
		UPDATE repository_counters SET next_issue_number = next_issue_number + 1
		WHERE repository_id = $1 RETURNING next_issue_number - 1
	`, fix.repoID).Scan(&number)
	if err != nil {
		t.Fatalf("allocate issue number: %v", err)
	}
	mustExec(t, ctx, pool, `
		INSERT INTO issues (id, repository_id, number, title, body, state, author_id)
		VALUES ($1,$2,$3,'Seed','seed', $4, $5)
	`, uuidNew(), fix.repoID, number, state, authorID)
	return number
}

func seedMergeRequest(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fix integrationFixture,
	authorID, revision string,
) int64 {
	t.Helper()
	var number int64
	err := pool.QueryRow(ctx, `
		UPDATE repository_counters SET next_merge_request_number = next_merge_request_number + 1
		WHERE repository_id = $1 RETURNING next_merge_request_number - 1
	`, fix.repoID).Scan(&number)
	if err != nil {
		t.Fatalf("allocate mr number: %v", err)
	}
	mustExec(t, ctx, pool, `
		INSERT INTO merge_requests (
			id, repository_id, number, title, body, state,
			source_branch, target_branch, source_revision, target_revision, author_id
		) VALUES ($1,$2,$3,'Seed MR','', 'open', 'feature', 'main', $4, 'main-rev', $5)
	`, uuidNew(), fix.repoID, number, revision, authorID)
	return number
}

func ptrString(value string) *string { return &value }
