package collab

import (
	"context"
	"errors"
	"os"
	"strings"
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
		($1,$2,'owner'),($1,$3,'member'),($1,$4,'member')
	`, orgID, alice.ID, carol.ID, bob.ID)
	mustExec(t, ctx, pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, description, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1,$2,$3,'Repo','', $4, $5, $6, 'main', $7)
	`, repoID, orgID, repoSlug, visibility, strings.ReplaceAll(orgID, "-", ""), "http://lore.test/"+orgID, alice.ID)
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
	if carolAccess.Permission != PermNone {
		t.Errorf("carol (org member without repository grant) permission = %v, want PermNone", carolAccess.Permission)
	}
	mustExec(t, ctx, pool, `
		UPDATE organization_memberships
		SET active = false
		WHERE organization_id = $1 AND user_id = $2
	`, fix.orgID, fix.bob.ID)
	if _, err := s.RepositoryPermission(ctx, fix.bob, repo); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("repository role after organization membership revoke = %v, want forbidden", err)
	}
	mustExec(t, ctx, pool, `
		UPDATE organization_memberships
		SET active = true
		WHERE organization_id = $1 AND user_id = $2
	`, fix.orgID, fix.bob.ID)
}

func TestIntegrationIssueUpdatePermissionAndPrecondition(t *testing.T) {
	pool, s := integrationEnv(t)
	ctx := context.Background()
	fix := setupFixture(t, pool, "public", "triage")
	number := seedIssue(t, ctx, pool, fix, fix.alice.ID, "open")

	// An issue author without triage cannot edit the issue.
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
	if closed.ClosedBy == nil || *closed.ClosedBy != fix.bob.Username {
		t.Fatalf("closed by = %v, want %q", closed.ClosedBy, fix.bob.Username)
	}
	reopened, err := s.UpdateIssue(ctx, fix.bob, fix.repoID, number, UpdateIssueInput{
		State: ptrString("open"),
	})
	if err != nil {
		t.Fatalf("reopen issue: %v", err)
	}
	if reopened.State != "open" || reopened.ClosedAt != nil || reopened.ClosedBy != nil {
		t.Fatalf("reopened issue retained close metadata: %+v", reopened)
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
	// The issue author still needs triage permission to edit.
	edited, err := s.UpdateIssueComment(ctx, fix.alice, fix.repoID, number, comment.ID, "edited body")
	if err != nil {
		t.Fatalf("edit own comment: %v", err)
	}
	if edited.Body != "edited body" || edited.EditedAt == nil {
		t.Fatalf("edited comment = %+v", edited)
	}
	if _, err := s.UpdateIssueComment(ctx, fix.alice, fix.repoID, number, comment.ID, "edited again"); err != nil {
		t.Fatalf("second comment edit: %v", err)
	}
	other := setupFixture(t, pool, "public", "write")
	if _, err := s.UpdateIssueComment(
		ctx, fix.alice, other.repoID, number, comment.ID, "cross-repository",
	); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("cross-repository comment edit expected ErrNotFound, got %v", err)
	}
	if err := s.DeleteIssueComment(
		ctx, fix.alice, other.repoID, number, comment.ID,
	); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("cross-repository comment delete expected ErrNotFound, got %v", err)
	}
	// Carol (not author, read only) cannot edit.
	_, err = s.UpdateIssueComment(ctx, fix.carol, fix.repoID, number, comment.ID, "hacked")
	if !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("carol edit comment expected ErrForbidden, got %v", err)
	}
	// Author can delete.
	if err := s.DeleteIssueComment(ctx, fix.alice, fix.repoID, number, comment.ID); err != nil {
		t.Fatalf("delete comment: %v", err)
	}
	if err := s.DeleteIssueComment(ctx, fix.alice, fix.repoID, number, comment.ID); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("second delete expected ErrNotFound, got %v", err)
	}
	if got := countTopic(t, ctx, pool, "issue_comment.updated"); got < 2 {
		t.Fatalf("comment updates outbox count = %d, want at least 2", got)
	}
	if got := countTopic(t, ctx, pool, "issue_comment.deleted"); got < 1 {
		t.Fatalf("comment delete outbox count = %d, want at least 1", got)
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
	japanese, err := s.CreateLabel(ctx, fix.alice, fix.repoID, LabelInput{
		Name: "障害対応", Description: "日本語のラベル", Color: "123abc",
	})
	if err != nil {
		t.Fatalf("create Japanese label: %v", err)
	}
	labels, err := s.ListLabels(ctx, fix.repoID, Page{Limit: maxPageLimit})
	if err != nil {
		t.Fatalf("list labels after Japanese label: %v", err)
	}
	foundJapanese := false
	for _, listed := range labels.Items {
		if listed.ID == japanese.ID {
			foundJapanese = true
			if listed.Name != "障害対応" || listed.Description != "日本語のラベル" {
				t.Fatalf("Japanese label round-trip: got %+v", listed)
			}
		}
	}
	if !foundJapanese {
		t.Fatalf("Japanese label %s was not returned by list", japanese.ID)
	}
	// Duplicate name conflicts.
	_, err = s.CreateLabel(ctx, fix.alice, fix.repoID, LabelInput{Name: "bug", Color: "00ff00"})
	if !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("duplicate label expected ErrConflict, got %v", err)
	}
	if _, err := s.UpdateLabel(ctx, fix.alice, fix.repoID, label.ID,
		LabelInput{Name: "bug", Description: "first", Color: "ff0000"}); err != nil {
		t.Fatalf("first label update: %v", err)
	}
	if _, err := s.UpdateLabel(ctx, fix.alice, fix.repoID, label.ID,
		LabelInput{Name: "bug", Description: "second", Color: "00ff00"}); err != nil {
		t.Fatalf("second label update: %v", err)
	}
	if got := countTopic(t, ctx, pool, "label.updated"); got < 2 {
		t.Fatalf("label update outbox count = %d, want at least 2", got)
	}
	other := setupFixture(t, pool, "public", "triage")
	if _, err := s.UpdateLabel(ctx, fix.alice, other.repoID, label.ID,
		LabelInput{Name: "bug", Color: "ffffff"}); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("cross-repository label update expected ErrNotFound, got %v", err)
	}
	if err := s.DeleteLabel(ctx, fix.alice, other.repoID, label.ID); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("cross-repository label delete expected ErrNotFound, got %v", err)
	}
	// Apply label to issue (bob triage).
	number := seedIssue(t, ctx, pool, fix, fix.alice.ID, "open")
	appliedBefore := countTopic(t, ctx, pool, "issue_label.applied")
	appliedAuditBefore := countAuditAction(t, ctx, pool, "issue_label.apply")
	_, applied, err := s.ApplyLabel(ctx, fix.bob, fix.repoID, number, label.ID)
	if err != nil {
		t.Fatalf("apply label: %v", err)
	}
	if !applied {
		t.Fatal("first apply should report applied=true")
	}
	if got := countTopic(t, ctx, pool, "issue_label.applied"); got != appliedBefore+1 {
		t.Fatalf("first label apply outbox count = %d, want %d", got, appliedBefore+1)
	}
	if got := countAuditAction(t, ctx, pool, "issue_label.apply"); got != appliedAuditBefore+1 {
		t.Fatalf("first label apply audit count = %d, want %d", got, appliedAuditBefore+1)
	}
	_, appliedAgain, err := s.ApplyLabel(ctx, fix.bob, fix.repoID, number, label.ID)
	if err != nil {
		t.Fatalf("reapply label: %v", err)
	}
	if appliedAgain {
		t.Fatal("duplicate apply should report applied=false")
	}
	if got := countTopic(t, ctx, pool, "issue_label.applied"); got != appliedBefore+1 {
		t.Fatalf("duplicate label apply outbox count = %d, want unchanged %d", got, appliedBefore+1)
	}
	if got := countAuditAction(t, ctx, pool, "issue_label.apply"); got != appliedAuditBefore+1 {
		t.Fatalf("duplicate label apply audit count = %d, want unchanged %d", got, appliedAuditBefore+1)
	}
	// Carol (read) cannot apply labels.
	_, _, err = s.ApplyLabel(ctx, fix.carol, fix.repoID, number, label.ID)
	if !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("carol apply label expected ErrForbidden, got %v", err)
	}
	// Remove label idempotent.
	removedBefore := countTopic(t, ctx, pool, "issue_label.removed")
	removedAuditBefore := countAuditAction(t, ctx, pool, "issue_label.remove")
	if err := s.RemoveLabel(ctx, fix.bob, fix.repoID, number, label.ID); err != nil {
		t.Fatalf("remove label: %v", err)
	}
	if got := countTopic(t, ctx, pool, "issue_label.removed"); got != removedBefore+1 {
		t.Fatalf("first label remove outbox count = %d, want %d", got, removedBefore+1)
	}
	if got := countAuditAction(t, ctx, pool, "issue_label.remove"); got != removedAuditBefore+1 {
		t.Fatalf("first label remove audit count = %d, want %d", got, removedAuditBefore+1)
	}
	if err := s.RemoveLabel(ctx, fix.bob, fix.repoID, number, label.ID); err != nil {
		t.Fatalf("idempotent remove label: %v", err)
	}
	if got := countTopic(t, ctx, pool, "issue_label.removed"); got != removedBefore+1 {
		t.Fatalf("duplicate label remove outbox count = %d, want unchanged %d", got, removedBefore+1)
	}
	if got := countAuditAction(t, ctx, pool, "issue_label.remove"); got != removedAuditBefore+1 {
		t.Fatalf("duplicate label remove audit count = %d, want unchanged %d", got, removedAuditBefore+1)
	}
	// Apply to missing issue returns 404.
	_, _, err = s.ApplyLabel(ctx, fix.bob, fix.repoID, 9999, label.ID)
	if !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("apply to missing issue expected ErrNotFound, got %v", err)
	}
	deletedLabel, err := s.CreateLabel(ctx, fix.alice, fix.repoID,
		LabelInput{Name: "docs", Color: "abcdef"})
	if err != nil {
		t.Fatalf("create label for delete: %v", err)
	}
	if err := s.DeleteLabel(ctx, fix.alice, fix.repoID, deletedLabel.ID); err != nil {
		t.Fatalf("delete label: %v", err)
	}
	if got := countTopic(t, ctx, pool, "label.deleted"); got < 1 {
		t.Fatalf("label delete outbox count = %d, want at least 1", got)
	}
}

func TestIntegrationReviews(t *testing.T) {
	pool, s := integrationEnv(t)
	ctx := context.Background()
	fix := setupFixture(t, pool, "public", "read")
	number := seedMergeRequest(t, ctx, pool, fix, fix.alice.ID, "rev-1")
	closed, err := s.UpdateMergeRequest(ctx, fix.alice, fix.repoID, number,
		UpdateMergeRequestInput{State: ptrString("closed")})
	if err != nil || closed.State != "closed" || closed.ClosedAt == nil {
		t.Fatalf("close merge request: result=%+v err=%v", closed, err)
	}
	reopened, err := s.UpdateMergeRequest(ctx, fix.alice, fix.repoID, number,
		UpdateMergeRequestInput{State: ptrString("open")})
	if err != nil || reopened.State != "open" || reopened.ClosedAt != nil {
		t.Fatalf("reopen merge request: result=%+v err=%v", reopened, err)
	}
	mustExec(t, ctx, pool, `UPDATE merge_requests SET state = 'merged' WHERE repository_id = $1 AND number = $2`,
		fix.repoID, number)
	if _, err := s.UpdateMergeRequest(ctx, fix.alice, fix.repoID, number,
		UpdateMergeRequestInput{Title: ptrString("must stay merged")}); !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("merged request edit expected ErrConflict, got %v", err)
	}
	mustExec(t, ctx, pool,
		`UPDATE merge_requests SET state = 'open', closed_at = NULL WHERE repository_id = $1 AND number = $2`,
		fix.repoID, number)

	// Author cannot review own MR.
	_, _, err = s.CreateReview(ctx, fix.alice, fix.repoID, number, ReviewInput{Decision: "approved"})
	if !errors.Is(err, ErrCannotReviewOwn) {
		t.Fatalf("self review expected ErrCannotReviewOwn, got %v", err)
	}
	// Bob can review.
	review, _, err := s.CreateReview(ctx, fix.bob, fix.repoID, number,
		ReviewInput{Decision: "changes_requested", Body: "please fix"})
	if err != nil {
		t.Fatalf("create review: %v", err)
	}
	if review.SourceRevision != "rev-1" {
		t.Fatalf("review revision = %q, want rev-1", review.SourceRevision)
	}
	// Upsert: bob reviews again on same revision updates the decision.
	review2, created, err := s.CreateReview(ctx, fix.bob, fix.repoID, number, ReviewInput{Decision: "approved"})
	if err != nil {
		t.Fatalf("upsert review: %v", err)
	}
	if created {
		t.Fatal("repeated review should update the existing decision")
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
	if review2.ID != review.ID {
		t.Fatalf("upsert returned ID %q, want persisted ID %q", review2.ID, review.ID)
	}
	if got := countTopic(t, ctx, pool, "merge_request_review.created"); got < 1 {
		t.Fatalf("review create outbox count = %d, want at least 1", got)
	}
	if got := countTopic(t, ctx, pool, "merge_request_review.updated"); got < 1 {
		t.Fatalf("review update outbox count = %d, want at least 1", got)
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
	updated, err := s.UpdateBranchRule(ctx, fix.alice, fix.repoID, rule.ID,
		BranchRuleInput{Pattern: "release/*", RequiredApprovals: 3})
	if err != nil {
		t.Fatalf("update rule: %v", err)
	}
	if updated.Pattern != "release/*" || updated.RequiredApprovals != 3 {
		t.Fatalf("updated rule = %+v", updated)
	}
	if _, err := s.UpdateBranchRule(ctx, fix.alice, fix.repoID, rule.ID,
		BranchRuleInput{Pattern: "release/*", RequiredApprovals: 4}); err != nil {
		t.Fatalf("second rule update: %v", err)
	}
	other := setupFixture(t, pool, "public", "write")
	if _, err := s.UpdateBranchRule(ctx, fix.alice, other.repoID, rule.ID,
		BranchRuleInput{Pattern: "wrong", RequiredApprovals: 0}); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("cross-repository branch rule update expected ErrNotFound, got %v", err)
	}
	if err := s.DeleteBranchRule(ctx, fix.alice, other.repoID, rule.ID); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("cross-repository branch rule delete expected ErrNotFound, got %v", err)
	}
	if err := s.DeleteBranchRule(ctx, fix.alice, fix.repoID, rule.ID); err != nil {
		t.Fatalf("delete rule: %v", err)
	}
	if got := countTopic(t, ctx, pool, "branch_rule.updated"); got < 2 {
		t.Fatalf("branch rule update outbox count = %d, want at least 2", got)
	}
	if got := countTopic(t, ctx, pool, "branch_rule.deleted"); got < 1 {
		t.Fatalf("branch rule delete outbox count = %d, want at least 1", got)
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

func countTopic(t *testing.T, ctx context.Context, pool *pgxpool.Pool, topic string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE topic = $1`, topic).Scan(&count); err != nil {
		t.Fatalf("count outbox topic %q: %v", topic, err)
	}
	return count
}

func countAuditAction(t *testing.T, ctx context.Context, pool *pgxpool.Pool, action string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = $1`, action).Scan(&count); err != nil {
		t.Fatalf("count audit action %q: %v", action, err)
	}
	return count
}
