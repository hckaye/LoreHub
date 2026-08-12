package platform

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type crossSearchFixture struct {
	store          *Store
	pool           databaseExecutor
	owner          User
	member         User
	direct         User
	teamUser       User
	suspended      User
	outsider       User
	orgID          string
	orgSlug        string
	repositories   map[string]string
	issueIDs       map[string]string
	pullRequestIDs map[string]string
}

type databaseExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func TestCrossResourceSearchVisibilityAndWorkItemShape(t *testing.T) {
	fixture := seedCrossSearchFixture(t)
	ctx := context.Background()
	assertSearchItems(t, fixture, nil, "public", "archived")
	assertSearchItems(t, fixture, &fixture.member, "public", "internal", "archived")
	assertSearchItems(t, fixture, &fixture.direct, "public", "internal", "private", "archived")
	assertSearchItems(t, fixture, &fixture.teamUser, "public", "internal", "private", "archived")
	assertSearchItems(t, fixture, &fixture.owner, "public", "internal", "private", "archived")
	assertSearchItems(t, fixture, &fixture.suspended)
	assertSearchItems(t, fixture, &fixture.outsider, "public", "archived")
	mustCrossSearchExec(t, fixture.pool, `
		UPDATE repository_memberships SET active = false
		WHERE repository_id = $1 AND user_id = $2
	`, fixture.repositories["private"], fixture.direct.ID)
	assertSearchItems(t, fixture, &fixture.direct, "public", "internal", "archived")
	mustCrossSearchExec(t, fixture.pool, `
		UPDATE team_repository_roles SET active = false WHERE repository_id = $1
	`, fixture.repositories["private"])
	assertSearchItems(t, fixture, &fixture.teamUser, "public", "internal", "archived")
	mustCrossSearchExec(t, fixture.pool, `
		UPDATE repository_memberships SET active = true
		WHERE repository_id = $1 AND user_id = $2
	`, fixture.repositories["private"], fixture.direct.ID)
	mustCrossSearchExec(t, fixture.pool, `
		UPDATE team_repository_roles SET active = true WHERE repository_id = $1
	`, fixture.repositories["private"])

	result, err := fixture.store.Search(ctx, &fixture.owner, "CrossSearch", "all", 1, 50)
	if err != nil {
		t.Fatalf("all search: %v", err)
	}
	if result.Counts.Issues != 4 || result.Counts.PullRequests != 4 ||
		len(result.Issues) != 4 || len(result.PullRequests) != 4 {
		t.Fatalf("all search counts/items = %#v", result)
	}
	if result.Issues[0].Kind != WorkItemKindIssue || result.Issues[0].Author.ID == "" ||
		result.Issues[0].Assignees == nil || result.Issues[0].Labels == nil {
		t.Fatalf("issue shape = %#v", result.Issues[0])
	}
	if result.PullRequests[0].Kind != WorkItemKindPullRequest || result.PullRequests[0].SourceBranch == "" ||
		result.PullRequests[0].TargetBranch == "" {
		t.Fatalf("pull request shape = %#v", result.PullRequests[0])
	}
	publicPull := findSearchItem(t, result.PullRequests, fixture.pullRequestIDs["public"])
	if publicPull.ApprovalCount != 1 || publicPull.CommentCount != 1 {
		t.Fatalf("public pull counts = %#v", publicPull)
	}
}

func TestCrossResourceSearchPaginationCountsAndMatching(t *testing.T) {
	fixture := seedCrossSearchFixture(t)
	ctx := context.Background()
	first, err := fixture.store.Search(ctx, &fixture.owner, "CrossSearch", "issues", 1, 2)
	if err != nil {
		t.Fatalf("page one: %v", err)
	}
	second, err := fixture.store.Search(ctx, &fixture.owner, "CrossSearch", "issues", 2, 2)
	if err != nil {
		t.Fatalf("page two: %v", err)
	}
	beyond, err := fixture.store.Search(ctx, &fixture.owner, "CrossSearch", "issues", 3, 2)
	if err != nil {
		t.Fatalf("out-of-range page: %v", err)
	}
	if first.Counts.Issues != 4 || second.Counts.Issues != 4 || beyond.Counts.Issues != 4 ||
		len(first.Issues) != 2 || len(second.Issues) != 2 || len(beyond.Issues) != 0 {
		t.Fatalf("pages = first %#v second %#v beyond %#v", first, second, beyond)
	}
	seen := map[string]bool{}
	for _, item := range append(append([]GlobalWorkItem{}, first.Issues...), second.Issues...) {
		if seen[item.ID] {
			t.Fatalf("pagination repeated work item %q", item.ID)
		}
		seen[item.ID] = true
	}
	if len(seen) != 4 {
		t.Fatalf("pagination returned %d unique work items, want 4", len(seen))
	}
	caseMatch, err := fixture.store.Search(ctx, nil, "crosssearch PUBLIC", "issues", 1, 20)
	if err != nil || len(caseMatch.Issues) != 1 {
		t.Fatalf("case/FTS search = %#v, error = %v", caseMatch, err)
	}
	substring, err := fixture.store.Search(ctx, nil, "ssSea", "issues", 1, 20)
	if err != nil || len(substring.Issues) != 2 {
		t.Fatalf("substring search = %#v, error = %v", substring, err)
	}
	wildcard, err := fixture.store.Search(ctx, nil, "%_", "issues", 1, 20)
	if err != nil || len(wildcard.Issues) != 0 {
		t.Fatalf("escaped wildcard search = %#v, error = %v", wildcard, err)
	}
	issuesOnly, err := fixture.store.Search(ctx, &fixture.owner, "CrossSearch", "issues", 1, 20)
	if err != nil || len(issuesOnly.PullRequests) != 0 || issuesOnly.PullRequests == nil ||
		issuesOnly.Counts.PullRequests != 4 {
		t.Fatalf("type-specific search = %#v, error = %v", issuesOnly, err)
	}
}

func TestCrossResourceSearchRejectsInactiveOrganization(t *testing.T) {
	fixture := seedCrossSearchFixture(t)
	ctx := context.Background()
	mustCrossSearchExec(t, fixture.pool, `UPDATE organizations SET active = false WHERE id = $1`, fixture.orgID)
	result, err := fixture.store.Search(ctx, &fixture.owner, "CrossSearch", "all", 1, 50)
	if err != nil {
		t.Fatalf("inactive organization search: %v", err)
	}
	if result.Counts.Repositories != 0 || result.Counts.Organizations != 0 ||
		result.Counts.Issues != 0 || result.Counts.PullRequests != 0 ||
		len(result.Repositories) != 0 || len(result.Issues) != 0 || len(result.PullRequests) != 0 {
		t.Fatalf("inactive organization leaked resources: %#v", result)
	}
}

func TestCrossResourceSearchVisibilityTransitions(t *testing.T) {
	fixture := seedCrossSearchFixture(t)
	directRepo := fixture.repositories["private"]
	teamRepo := fixture.repositories["private"]
	mustCrossSearchExec(t, fixture.pool, `
		UPDATE repository_memberships SET active = false
		WHERE repository_id = $1 AND user_id = $2
	`, directRepo, fixture.direct.ID)
	assertSearchItems(t, fixture, &fixture.direct, "public", "internal", "archived")
	mustCrossSearchExec(t, fixture.pool, `
		UPDATE team_repository_roles SET active = false WHERE repository_id = $1
	`, teamRepo)
	assertSearchItems(t, fixture, &fixture.teamUser, "public", "internal", "archived")
	mustCrossSearchExec(t, fixture.pool, `
		UPDATE organization_memberships SET active = false
		WHERE organization_id = $1 AND user_id = $2
	`, fixture.orgID, fixture.member.ID)
	assertSearchItems(t, fixture, &fixture.member, "public", "archived")
}

func assertSearchItems(t *testing.T, fixture crossSearchFixture, viewer *User, expected ...string) {
	t.Helper()
	result, err := fixture.store.Search(context.Background(), viewer, "CrossSearch", "all", 1, 50)
	if err != nil {
		t.Fatalf("search for viewer %#v: %v", viewer, err)
	}
	wantIssues := map[string]bool{}
	wantPulls := map[string]bool{}
	for _, visibility := range expected {
		wantIssues[fixture.issueIDs[visibility]] = true
		wantPulls[fixture.pullRequestIDs[visibility]] = true
	}
	if int64(len(wantIssues)) != result.Counts.Issues || int64(len(wantPulls)) != result.Counts.PullRequests {
		t.Fatalf("counts for viewer %#v = %#v, want %d/%d", viewer, result.Counts,
			len(wantIssues), len(wantPulls))
	}
	for _, issue := range result.Issues {
		if !wantIssues[issue.ID] {
			t.Fatalf("issue %q leaked for viewer %#v", issue.ID, viewer)
		}
		delete(wantIssues, issue.ID)
	}
	for _, pull := range result.PullRequests {
		if !wantPulls[pull.ID] {
			t.Fatalf("pull %q leaked for viewer %#v", pull.ID, viewer)
		}
		delete(wantPulls, pull.ID)
	}
	if len(wantIssues) != 0 || len(wantPulls) != 0 {
		t.Fatalf("search omitted expected items: issues %v pulls %v", wantIssues, wantPulls)
	}
}

func findSearchItem(t *testing.T, items []GlobalWorkItem, id string) GlobalWorkItem {
	t.Helper()
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("work item %q not found in %#v", id, items)
	return GlobalWorkItem{}
}

func seedCrossSearchFixture(t *testing.T) crossSearchFixture {
	t.Helper()
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	fixture := crossSearchFixture{
		store: store, pool: pool, orgID: uuid.NewString(), orgSlug: "cross-search-" + suffix,
		repositories: map[string]string{}, issueIDs: map[string]string{},
		pullRequestIDs: map[string]string{},
	}
	fixture.owner = platformTestUser("cross-owner-" + suffix)
	fixture.member = platformTestUser("cross-member-" + suffix)
	fixture.direct = platformTestUser("cross-direct-" + suffix)
	fixture.teamUser = platformTestUser("cross-team-" + suffix)
	fixture.suspended = platformTestUser("cross-suspended-" + suffix)
	fixture.outsider = platformTestUser("cross-outsider-" + suffix)
	users := []User{
		fixture.owner, fixture.member, fixture.direct,
		fixture.teamUser, fixture.suspended, fixture.outsider,
	}
	for _, user := range users {
		mustCrossSearchExec(t, pool, `
			INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)
		`, user.ID, user.Username, user.DisplayName)
	}
	mustCrossSearchExec(t, pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, fixture.suspended.ID)
	mustCrossSearchExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, description, visibility, created_by)
		VALUES ($1, $2, 'CrossSearch organization', 'CrossSearch tenant', 'public', $3)
	`, fixture.orgID, fixture.orgSlug, fixture.owner.ID)
	for _, membership := range []struct {
		user User
		role string
	}{
		{fixture.owner, "owner"}, {fixture.member, "member"},
		{fixture.teamUser, "member"}, {fixture.suspended, "owner"},
	} {
		mustCrossSearchExec(t, pool, `
			INSERT INTO organization_memberships (organization_id, user_id, role)
			VALUES ($1, $2, $3)
		`, fixture.orgID, membership.user.ID, membership.role)
	}
	teamID := uuid.NewString()
	mustCrossSearchExec(t, pool, `
		INSERT INTO teams (id, organization_id, slug, display_name, created_by)
		VALUES ($1, $2, $3, 'CrossSearch team', $4)
	`, teamID, fixture.orgID, "cross-team-"+suffix, fixture.owner.ID)
	mustCrossSearchExec(t, pool, `
		INSERT INTO team_memberships (team_id, user_id, role) VALUES ($1, $2, 'member')
	`, teamID, fixture.teamUser.ID)
	for _, visibility := range []string{"public", "internal", "private", "archived", "pending"} {
		seedCrossSearchRepository(t, &fixture, visibility, suffix)
	}
	mustCrossSearchExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role)
		VALUES ($1, $2, 'read')
	`, fixture.repositories["private"], fixture.direct.ID)
	mustCrossSearchExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role)
		VALUES ($1, $2, 'read')
	`, fixture.repositories["internal"], fixture.direct.ID)
	mustCrossSearchExec(t, pool, `
		INSERT INTO team_repository_roles (team_id, repository_id, role, created_by)
		VALUES ($1, $2, 'read', $3)
	`, teamID, fixture.repositories["private"], fixture.owner.ID)
	seedCrossSearchWorkItems(t, &fixture, suffix)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, fixture.orgID)
		for _, user := range users {
			_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, user.ID)
		}
	})
	return fixture
}

func seedCrossSearchRepository(
	t *testing.T,
	fixture *crossSearchFixture,
	visibility string,
	suffix string,
) {
	t.Helper()
	repositoryID := uuid.NewString()
	fixture.repositories[visibility] = repositoryID
	storedVisibility := visibility
	archivedAt := any(nil)
	archivedBy := any(nil)
	lifecycle := "active"
	if visibility == "archived" {
		storedVisibility = "public"
		archivedAt = time.Now().UTC()
		archivedBy = fixture.owner.ID
	}
	if visibility == "pending" {
		storedVisibility = "public"
		lifecycle = "pending"
	}
	slug := "cross-" + visibility + "-" + suffix
	mustCrossSearchExec(t, fixture.pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, description, visibility,
			lifecycle_state, archived_at, archived_by,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, $3, $4, 'CrossSearch repository', $5, $6, $7, $8, $9, $10, 'main', $11)
	`, repositoryID, fixture.orgID, slug, "CrossSearch "+visibility, storedVisibility,
		lifecycle, archivedAt, archivedBy, canonicalTestLoreID(repositoryID),
		"lore://"+slug, fixture.owner.ID)
	mustCrossSearchExec(t, fixture.pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, repositoryID)
}

func seedCrossSearchWorkItems(t *testing.T, fixture *crossSearchFixture, suffix string) {
	t.Helper()
	baseTime := time.Now().UTC().Add(-time.Hour)
	for index, visibility := range []string{"public", "internal", "private", "archived", "pending"} {
		issueID := uuid.NewString()
		pullID := uuid.NewString()
		fixture.issueIDs[visibility] = issueID
		fixture.pullRequestIDs[visibility] = pullID
		updated := baseTime.Add(time.Duration(index) * time.Minute)
		mustCrossSearchExec(t, fixture.pool, `
			INSERT INTO issues (
				id, repository_id, number, title, body, state, author_id, created_at, updated_at
			) VALUES ($1, $2, 1, $3, $4, 'open', $5, $6, $6)
		`, issueID, fixture.repositories[visibility], "CrossSearch "+visibility+" issue",
			"Searchable body "+suffix, fixture.owner.ID, updated)
		mustCrossSearchExec(t, fixture.pool, `
			INSERT INTO merge_requests (
				id, repository_id, number, title, body, state, source_branch, target_branch,
				source_revision, target_revision, author_id, created_at, updated_at
			) VALUES ($1, $2, 1, $3, $4, 'open', 'feature', 'main', $5, $6, $7, $8, $8)
		`, pullID, fixture.repositories[visibility], "CrossSearch "+visibility+" pull",
			"Searchable body "+suffix, strings.Repeat("a", 64), strings.Repeat("b", 64),
			fixture.owner.ID, updated)
	}
	mustCrossSearchExec(t, fixture.pool, `
		INSERT INTO issue_comments (id, issue_id, author_id, body)
		VALUES ($1, $2, $3, 'CrossSearch issue comment')
	`, uuid.NewString(), fixture.issueIDs["public"], fixture.owner.ID)
	mustCrossSearchExec(t, fixture.pool, `
		INSERT INTO merge_request_comments (id, merge_request_id, author_id, body)
		VALUES ($1, $2, $3, 'CrossSearch pull comment')
	`, uuid.NewString(), fixture.pullRequestIDs["public"], fixture.owner.ID)
	mustCrossSearchExec(t, fixture.pool, `
		INSERT INTO merge_request_reviews (
			id, merge_request_id, reviewer_id, source_revision, decision, body
		) VALUES ($1, $2, $3, $4, 'approved', '')
	`, uuid.NewString(), fixture.pullRequestIDs["public"], fixture.member.ID, strings.Repeat("a", 64))
}

func mustCrossSearchExec(t *testing.T, executor databaseExecutor, query string, arguments ...any) {
	t.Helper()
	if _, err := executor.Exec(context.Background(), query, arguments...); err != nil {
		t.Fatal(err)
	}
}
