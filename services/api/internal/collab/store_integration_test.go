package collab

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"sync"
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
	`, orgID, alice.ID, bob.ID, carol.ID)
	mustExec(t, ctx, pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, description, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1,$2,$3,'Repo','', $4, $5, $6, 'main', $7)
	`, repoID, orgID, repoSlug, visibility, loreFixtureID(orgID), "http://lore.test/"+orgID, alice.ID)
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

func loreFixtureID(value string) string {
	return strings.ReplaceAll(value, "-", "")
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
	mustExec(t, ctx, pool, `
		INSERT INTO repository_topics (repository_id, topic, created_by) VALUES ($1, 'game-development', $2)
	`, pub.repoID, pub.alice.ID)

	publicRepository, err := s.LookupRepository(ctx, nil, pub.ownerSlug, pub.repoSlug)
	if err != nil {
		t.Fatalf("anonymous public lookup: %v", err)
	}
	if !slices.Equal(publicRepository.Topics, []string{"game-development"}) {
		t.Fatalf("anonymous public topics = %v", publicRepository.Topics)
	}
	memberRepository, err := s.LookupRepository(ctx, &pub.bob, pub.ownerSlug, pub.repoSlug)
	if err != nil || !slices.Equal(memberRepository.Topics, publicRepository.Topics) {
		t.Fatalf("authenticated public topics = %v, err=%v", memberRepository.Topics, err)
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
		t.Errorf("carol (org member) permission = %v, want PermNone", carolAccess.Permission)
	}
	mustExec(t, ctx, pool, `
		UPDATE repositories SET archived_at = now(), archived_by = $2 WHERE id = $1
	`, fix.repoID, fix.alice.ID)
	repo, err = s.LookupRepository(ctx, &fix.bob, fix.ownerSlug, fix.repoSlug)
	if err != nil || repo.ArchivedAt == nil {
		t.Fatalf("lookup archived repository = %+v, err=%v", repo, err)
	}
	bobAccess, err = s.RepositoryPermission(ctx, fix.bob, repo)
	if err != nil || bobAccess.Permission != PermRead {
		t.Fatalf("archived repository permission = %+v, err=%v", bobAccess, err)
	}
	issueID := uuidNew()
	mustExec(t, ctx, pool, `
		INSERT INTO issues (id, repository_id, number, title, author_id)
		VALUES ($1, $2, 1, 'Archived issue', $3)
	`, issueID, fix.repoID, fix.bob.ID)
	if _, err := s.CreateIssueComment(ctx, fix.bob, fix.repoID, 1, "comment"); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("archived repository comment error = %v", err)
	}
	updatedTitle := "Changed"
	_, err = s.UpdateIssue(
		ctx, fix.bob, fix.repoID, 1, UpdateIssueInput{Title: &updatedTitle},
	)
	if !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("archived repository issue update error = %v", err)
	}
}

func TestIntegrationOutsideDirectCollaboratorAccess(t *testing.T) {
	pool, s := integrationEnv(t)
	ctx := context.Background()
	fix := setupFixture(t, pool, "private", "")
	outsider := platform.User{ID: uuidNew(), Username: "outside-" + fix.orgID[:8], DisplayName: "Outside"}
	mustExec(t, ctx, pool, `
		INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)
	`, outsider.ID, outsider.Username, outsider.DisplayName)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, outsider.ID)
	})
	mustExec(t, ctx, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role, active)
		VALUES ($1, $2, 'write', true)
	`, fix.repoID, outsider.ID)

	repository, err := s.LookupRepository(ctx, &outsider, fix.ownerSlug, fix.repoSlug)
	if err != nil {
		t.Fatalf("outside private lookup: %v", err)
	}
	access, err := s.RepositoryPermission(ctx, outsider, repository)
	if err != nil {
		t.Fatalf("outside private permission: %v", err)
	}
	if access.Permission != PermWrite {
		t.Fatalf("outside permission = %v, want PermWrite", access.Permission)
	}

	mustExec(t, ctx, pool, `UPDATE repositories SET visibility = 'public' WHERE id = $1`, fix.repoID)
	mustExec(t, ctx, pool, `UPDATE repository_memberships SET active = false WHERE repository_id = $1 AND user_id = $2`,
		fix.repoID, outsider.ID)
	repository, err = s.LookupRepository(ctx, &outsider, fix.ownerSlug, fix.repoSlug)
	if err != nil {
		t.Fatalf("outside public lookup: %v", err)
	}
	access, err = s.RepositoryPermission(ctx, outsider, repository)
	if err != nil {
		t.Fatalf("outside public permission: %v", err)
	}
	if access.Permission != PermRead {
		t.Fatalf("outside public permission = %v, want PermRead", access.Permission)
	}

	mustExec(t, ctx, pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, outsider.ID)
	if _, err := s.LookupRepository(ctx, &outsider, fix.ownerSlug, fix.repoSlug); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("suspended outside lookup error = %v, want not found", err)
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
	issueWithLabel, err := s.GetIssue(ctx, fix.repoID, number)
	if err != nil {
		t.Fatalf("get labelled issue: %v", err)
	}
	if issueWithLabel.LabelCount != 1 || len(issueWithLabel.Labels) != 1 ||
		issueWithLabel.Labels[0].ID != label.ID {
		t.Fatalf("issue labels = %+v, count = %d", issueWithLabel.Labels, issueWithLabel.LabelCount)
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
	rule, err := s.CreateBranchRule(ctx, fix.alice, fix.repoID, BranchRuleInput{
		Pattern:              "main",
		RequiredApprovals:    2,
		RequiredStatusChecks: []string{" lint ", "CI/Test"},
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if strings.Join(rule.RequiredStatusChecks, ",") != "CI/Test,lint" {
		t.Fatalf("created rule status checks = %#v", rule.RequiredStatusChecks)
	}
	rules, err := s.ListBranchRules(ctx, fix.repoID)
	if err != nil || len(rules) != 1 || strings.Join(rules[0].RequiredStatusChecks, ",") != "CI/Test,lint" {
		t.Fatalf("listed rule status checks = %#v, err=%v", rules, err)
	}
	// Duplicate pattern conflicts.
	_, err = s.CreateBranchRule(ctx, fix.alice, fix.repoID, BranchRuleInput{Pattern: "main", RequiredApprovals: 0})
	if !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("duplicate rule expected ErrConflict, got %v", err)
	}
	// Update and delete.
	updated, err := s.UpdateBranchRule(ctx, fix.alice, fix.repoID, rule.ID,
		BranchRuleInput{Pattern: "release/*", RequiredApprovals: 3,
			RequiredStatusChecks: []string{"security", "Build"}})
	if err != nil {
		t.Fatalf("update rule: %v", err)
	}
	if updated.Pattern != "release/*" || updated.RequiredApprovals != 3 ||
		strings.Join(updated.RequiredStatusChecks, ",") != "Build,security" {
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

func TestIntegrationMergeOperationLeaseFinalizationAndIdempotency(t *testing.T) {
	pool, s := integrationEnv(t)
	ctx := context.Background()
	fix := setupFixture(t, pool, "public", "write")
	number := seedMergeRequest(t, ctx, pool, fix, fix.alice.ID, "source-rev")

	operation, err := s.AcquireMergeOperation(ctx, fix.alice.ID, fix.repoID, number,
		"source-rev", "main-rev", fix.alice.ID, time.Minute)
	if err != nil {
		t.Fatalf("acquire merge operation: %v", err)
	}
	if operation.State != "created" || operation.LeaseOwner != fix.alice.ID {
		t.Fatalf("initial operation = %+v", operation)
	}
	if _, err := s.AcquireMergeOperation(ctx, fix.bob.ID, fix.repoID, number,
		"source-rev", "main-rev", fix.bob.ID, time.Minute); !errors.Is(err, ErrMergeBusy) {
		t.Fatalf("second owner error = %v, want ErrMergeBusy", err)
	}

	operation.State = "ready_to_push"
	operation.StagedRevision = "staged-rev"
	operation = mustUpdateMergeOperation(t, ctx, s, operation)
	operation.State = "pushed"
	operation.PushedRevision = "remote-rev"
	operation = mustUpdateMergeOperation(t, ctx, s, operation)

	auditBefore := countAuditAction(t, ctx, pool, "merge_request.merge")
	outboxBefore := countTopic(t, ctx, pool, "merge_request.merged")
	merged, err := s.FinalizeMerged(ctx, fix.alice, fix.repoID, number, operation.ID, "remote-rev")
	if err != nil {
		t.Fatalf("finalize merge request: %v", err)
	}
	if merged.State != "merged" || merged.MergedRevision == nil || *merged.MergedRevision != "remote-rev" ||
		merged.MergedBy == nil || *merged.MergedBy != fix.alice.Username {
		t.Fatalf("merged request = %+v", merged)
	}
	if got := countAuditAction(t, ctx, pool, "merge_request.merge"); got != auditBefore+1 {
		t.Fatalf("merge audit count = %d, want %d", got, auditBefore+1)
	}
	if got := countTopic(t, ctx, pool, "merge_request.merged"); got != outboxBefore+1 {
		t.Fatalf("merge outbox count = %d, want %d", got, outboxBefore+1)
	}

	reconciled, err := s.FinalizeMerged(ctx, fix.alice, fix.repoID, number, operation.ID, "remote-rev")
	if err != nil {
		t.Fatalf("repeat finalization: %v", err)
	}
	if reconciled.ID != merged.ID || reconciled.State != "merged" {
		t.Fatalf("repeat finalization = %+v", reconciled)
	}
	if got := countAuditAction(t, ctx, pool, "merge_request.merge"); got != auditBefore+1 {
		t.Fatalf("repeat merge audit count = %d, want %d", got, auditBefore+1)
	}
	if got := countTopic(t, ctx, pool, "merge_request.merged"); got != outboxBefore+1 {
		t.Fatalf("repeat merge outbox count = %d, want %d", got, outboxBefore+1)
	}

	finalOperation, err := s.AcquireMergeOperation(ctx, fix.bob.ID, fix.repoID, number,
		"source-rev", "main-rev", fix.bob.ID, time.Minute)
	if err != nil {
		t.Fatalf("acquire completed operation: %v", err)
	}
	if finalOperation.State != "merged" || finalOperation.PushedRevision != "remote-rev" {
		t.Fatalf("completed operation = %+v", finalOperation)
	}
}

func TestIntegrationMergeReadinessUsesExactSourceRevisionForReviewsAndCI(t *testing.T) {
	pool, s := integrationEnv(t)
	ctx := context.Background()
	fix := setupFixture(t, pool, "public", "write")
	number := seedMergeRequest(t, ctx, pool, fix, fix.alice.ID, "source-rev-1")
	if _, _, err := s.CreateReview(ctx, fix.bob, fix.repoID, number,
		ReviewInput{Decision: "approved"}); err != nil {
		t.Fatalf("create source revision approval: %v", err)
	}
	mustExec(t, ctx, pool, `
		INSERT INTO ci_runs (
			id, repository_id, run_number, event_name, branch, revision, status, conclusion, event_payload
		) VALUES ($1, $2, 1, 'push', 'feature', 'source-rev-1', 'completed', 'success', '{}'),
		($3, $2, 2, 'push', 'main', 'main-rev', 'completed', 'success', '{}')
	`, uuidNew(), fix.repoID, uuidNew())
	if ok, err := s.ListSuccessfulCI(ctx, fix.repoID, "feature", "source-rev-1"); err != nil || !ok {
		t.Fatalf("exact source CI = %v, %v; want true", ok, err)
	}
	mustExec(t, ctx, pool, `
		UPDATE merge_requests SET source_revision = 'source-rev-2'
		WHERE repository_id = $1 AND number = $2
	`, fix.repoID, number)
	summary, err := s.ListReviews(ctx, fix.repoID, number)
	if err != nil {
		t.Fatalf("list reviews after source revision change: %v", err)
	}
	if summary.Approvals != 0 || len(summary.CurrentReviews) != 0 {
		t.Fatalf("old approval was reused after source revision change: %+v", summary)
	}
	if ok, err := s.ListSuccessfulCI(ctx, fix.repoID, "feature", "source-rev-2"); err != nil || ok {
		t.Fatalf("old CI was reused after source revision change = %v, %v; want false", ok, err)
	}
}

func TestIntegrationMergeOperationResolutionLeaseAndRestartRaces(t *testing.T) {
	pool, s := integrationEnv(t)
	ctx := context.Background()
	fix := setupFixture(t, pool, "public", "write")
	number := seedMergeRequest(t, ctx, pool, fix, fix.alice.ID, "source-race")
	operation, err := s.AcquireMergeOperation(ctx, fix.alice.ID, fix.repoID, number,
		"source-race", "main-race", "initial-owner", time.Minute)
	if err != nil {
		t.Fatalf("acquire operation: %v", err)
	}
	operation.State = "started"
	operation.ConflictPaths = []string{"conflict.txt"}
	operation = mustUpdateMergeOperation(t, ctx, s, operation)

	type resolutionResult struct {
		operation MergeOperation
		err       error
	}
	resolutionResults := make(chan resolutionResult, 2)
	var resolutionGroup sync.WaitGroup
	for _, actor := range []platform.User{fix.alice, fix.bob} {
		actor := actor
		resolutionGroup.Add(1)
		go func() {
			defer resolutionGroup.Done()
			updated, resolveErr := s.RecordMergeResolutions(ctx, actor, operation,
				[]string{"conflict.txt"}, "theirs")
			resolutionResults <- resolutionResult{operation: updated, err: resolveErr}
		}()
	}
	resolutionGroup.Wait()
	close(resolutionResults)
	resolutionSuccesses := 0
	resolutionConflicts := 0
	for result := range resolutionResults {
		if result.err == nil {
			resolutionSuccesses++
			continue
		}
		if errors.Is(result.err, ErrMergeOperationConflict) {
			resolutionConflicts++
			continue
		}
		t.Fatalf("concurrent resolution error = %v", result.err)
	}
	if resolutionSuccesses != 1 || resolutionConflicts != 1 {
		t.Fatalf("concurrent resolutions = successes %d conflicts %d, want one each",
			resolutionSuccesses, resolutionConflicts)
	}
	resolved, err := s.GetMergeOperation(ctx, fix.repoID, number)
	if err != nil {
		t.Fatalf("get resolved operation: %v", err)
	}
	if resolved.State != "conflicts" || len(resolved.Resolutions) != 1 || resolved.Resolutions[0].Strategy != "theirs" {
		t.Fatalf("durable resolution = %#v", resolved.Resolutions)
	}
	if resolved.LeaseExpiresAt == nil || !resolved.LeaseExpiresAt.After(nowUTC()) {
		t.Fatalf("resolution transaction did not retain an active lease: %v", resolved.LeaseExpiresAt)
	}

	mustExec(t, ctx, pool, `UPDATE merge_operations SET lease_expires_at = now() - interval '1 second' WHERE id = $1`,
		resolved.ID)
	type acquireResult struct {
		operation MergeOperation
		err       error
	}
	acquireResults := make(chan acquireResult, 2)
	var acquireGroup sync.WaitGroup
	for _, owner := range []string{"lease-owner-b", "lease-owner-c"} {
		owner := owner
		acquireGroup.Add(1)
		go func() {
			defer acquireGroup.Done()
			acquired, acquireErr := s.AcquireMergeOperation(ctx, fix.alice.ID, fix.repoID, number,
				resolved.SourceRevision, resolved.TargetRevision, owner, time.Minute)
			acquireResults <- acquireResult{operation: acquired, err: acquireErr}
		}()
	}
	acquireGroup.Wait()
	close(acquireResults)
	var leaseWinner MergeOperation
	leaseSuccesses := 0
	leaseConflicts := 0
	for result := range acquireResults {
		if result.err == nil {
			leaseSuccesses++
			leaseWinner = result.operation
			continue
		}
		if errors.Is(result.err, ErrMergeBusy) {
			leaseConflicts++
			continue
		}
		t.Fatalf("concurrent lease acquisition error = %v", result.err)
	}
	if leaseSuccesses != 1 || leaseConflicts != 1 {
		t.Fatalf("concurrent lease acquisition = successes %d conflicts %d, want one each",
			leaseSuccesses, leaseConflicts)
	}

	abortState := leaseWinner
	abortState.State = "aborted"
	abortState.LeaseOwner = ""
	abortState.LeaseExpiresAt = nil
	now := nowUTC()
	abortState.CompletedAt = &now
	restartResults := make(chan error, 2)
	var stateGroup sync.WaitGroup
	stateGroup.Add(2)
	go func() {
		defer stateGroup.Done()
		_, updateErr := s.UpdateMergeOperationOwned(ctx, abortState, leaseWinner.LeaseOwner)
		restartResults <- updateErr
	}()
	go func() {
		defer stateGroup.Done()
		_, restartErr := s.RestartMergeOperation(ctx, fix.alice, fix.repoID, number,
			leaseWinner.SourceRevision, leaseWinner.TargetRevision, leaseWinner.LeaseOwner, time.Minute)
		restartResults <- restartErr
	}()
	stateGroup.Wait()
	close(restartResults)
	stateSuccesses := 0
	stateConflicts := 0
	for stateErr := range restartResults {
		if stateErr == nil {
			stateSuccesses++
			continue
		}
		if errors.Is(stateErr, ErrMergeOperationConflict) || errors.Is(stateErr, ErrMergeBusy) {
			stateConflicts++
			continue
		}
		t.Fatalf("abort/restart race error = %v", stateErr)
	}
	if stateSuccesses != 1 || stateConflicts != 1 {
		t.Fatalf("abort/restart race = successes %d conflicts %d, want one each",
			stateSuccesses, stateConflicts)
	}
	final, err := s.GetMergeOperation(ctx, fix.repoID, number)
	if err != nil {
		t.Fatalf("get raced operation: %v", err)
	}
	if final.State != "aborted" && final.State != "started" {
		t.Fatalf("raced operation state = %q, want aborted or started", final.State)
	}
	if final.State == "started" && len(final.Resolutions) != 0 {
		t.Fatalf("restart reused stale conflict resolutions: %#v", final.Resolutions)
	}
	if got := countAuditAction(t, ctx, pool, "merge_operation.resolution_recorded"); got < 1 {
		t.Fatalf("resolution audit count = %d, want at least 1", got)
	}
	if got := countTopic(t, ctx, pool, "merge_operation.updated"); got < 1 {
		t.Fatalf("operation update outbox count = %d, want at least 1", got)
	}
}

func TestIntegrationMergeOperationRetriesStaleStartWithNewRevisions(t *testing.T) {
	pool, s := integrationEnv(t)
	ctx := context.Background()
	fix := setupFixture(t, pool, "public", "write")
	number := seedMergeRequest(t, ctx, pool, fix, fix.alice.ID, "source-old")
	operation, err := s.AcquireMergeOperation(ctx, fix.alice.ID, fix.repoID, number,
		"source-old", "target-old", "stale-start-owner", time.Minute)
	if err != nil {
		t.Fatalf("acquire operation: %v", err)
	}
	operation.State = "created"
	operation.ErrorCode = "stale_revision"
	operation.LeaseOwner = ""
	operation.LeaseExpiresAt = nil
	operation = mustUpdateMergeOperation(t, ctx, s, operation)
	mustExec(t, ctx, pool, `
		INSERT INTO merge_operation_resolutions (operation_id, path, strategy, actor_id)
		VALUES ($1, 'old-conflict.txt', 'theirs', $2)
	`, operation.ID, fix.alice.ID)

	retried, err := s.AcquireMergeOperation(ctx, fix.alice.ID, fix.repoID, number,
		"source-new", "target-new", "retry-owner", time.Minute)
	if err != nil {
		t.Fatalf("retry stale start: %v", err)
	}
	if retried.SourceRevision != "source-new" || retried.TargetRevision != "target-new" ||
		retried.State != "created" || retried.ErrorCode != "" || len(retried.Resolutions) != 0 {
		t.Fatalf("stale start was not reset for exact new revisions: %#v", retried)
	}
}

func TestIntegrationMergeOperationResolveAndPushRace(t *testing.T) {
	pool, s := integrationEnv(t)
	ctx := context.Background()
	fix := setupFixture(t, pool, "public", "write")
	number := seedMergeRequest(t, ctx, pool, fix, fix.alice.ID, "source-resolve-push")
	operation, err := s.AcquireMergeOperation(ctx, fix.alice.ID, fix.repoID, number,
		"source-resolve-push", "target-resolve-push", "resolve-push-owner", time.Minute)
	if err != nil {
		t.Fatalf("acquire operation: %v", err)
	}
	operation.State = "started"
	operation.ConflictPaths = []string{"conflict.txt"}
	operation = mustUpdateMergeOperation(t, ctx, s, operation)

	results := make(chan error, 2)
	go func() {
		_, resolveErr := s.RecordMergeResolutions(ctx, fix.alice, operation,
			[]string{"conflict.txt"}, "mine")
		results <- resolveErr
	}()
	pushOperation := operation
	pushOperation.State = "pushing"
	go func() {
		_, pushErr := s.UpdateMergeOperationOwned(ctx, pushOperation, operation.LeaseOwner)
		results <- pushErr
	}()

	successes := 0
	conflicts := 0
	for range 2 {
		raceErr := <-results
		if raceErr == nil {
			successes++
			continue
		}
		if errors.Is(raceErr, ErrMergeOperationConflict) {
			conflicts++
			continue
		}
		t.Fatalf("concurrent resolve/push error = %v", raceErr)
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent resolve/push = successes %d conflicts %d, want one each", successes, conflicts)
	}
	final, err := s.GetMergeOperation(ctx, fix.repoID, number)
	if err != nil {
		t.Fatalf("get raced operation: %v", err)
	}
	if final.State != "conflicts" && final.State != "pushing" {
		t.Fatalf("raced operation state = %q, want conflicts or pushing", final.State)
	}
	if final.State == "conflicts" && len(final.Resolutions) != 1 {
		t.Fatalf("resolved operation lost its durable choice: %#v", final.Resolutions)
	}
	if got := countAuditAction(t, ctx, pool, "merge_operation.updated"); got < 1 {
		t.Fatalf("resolve/push audit count = %d, want at least 1", got)
	}
	if got := countTopic(t, ctx, pool, "merge_operation.updated"); got < 1 {
		t.Fatalf("resolve/push outbox count = %d, want at least 1", got)
	}
}

func mustUpdateMergeOperation(t *testing.T, ctx context.Context, s *store, operation MergeOperation) MergeOperation {
	t.Helper()
	updated, err := s.UpdateMergeOperation(ctx, operation)
	if err != nil {
		t.Fatalf("update merge operation: %v", err)
	}
	return updated
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
