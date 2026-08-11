package platform

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type globalWorkItemFixture struct {
	pool             *pgxpool.Pool
	store            *Store
	viewer           User
	owner            User
	organizationID   string
	teamID           string
	publicRepository string
	internalRepo     string
	teamRepository   string
	hiddenRepository string
	archivedRepo     string
	issueIDs         map[string]string
	pullIDs          map[string]string
}

func TestGlobalWorkItemPatternEscapesLikeWildcards(t *testing.T) {
	if got := globalWorkItemPattern(`50%_\path`); got != `%50\%\_\\path%` {
		t.Fatalf("escaped search pattern = %q", got)
	}
}

func TestGlobalIssuesRespectAccessScopeSearchAndCursor(t *testing.T) {
	fixture := seedGlobalWorkItems(t)
	ctx := context.Background()

	page, err := fixture.store.ListGlobalIssues(ctx, fixture.viewer, GlobalWorkItemFilter{
		State: "all", Scope: "involved", Limit: 25,
	})
	if err != nil {
		t.Fatalf("list involved issues: %v", err)
	}
	assertGlobalWorkItemIDs(t, page.Items,
		fixture.issueIDs["public"], fixture.issueIDs["internal"], fixture.issueIDs["team"])
	for _, item := range page.Items {
		if item.Repository.ID == fixture.hiddenRepository || item.Repository.ID == fixture.archivedRepo {
			t.Fatalf("inaccessible issue leaked from repository %s", item.Repository.ID)
		}
	}

	created, err := fixture.store.ListGlobalIssues(ctx, fixture.viewer, GlobalWorkItemFilter{
		State: "all", Scope: "created", Limit: 25,
	})
	if err != nil {
		t.Fatalf("list created issues: %v", err)
	}
	assertGlobalWorkItemIDs(t, created.Items, fixture.issueIDs["public"])

	searched, err := fixture.store.ListGlobalIssues(ctx, fixture.viewer, GlobalWorkItemFilter{
		State: "open", Scope: "all", Query: "renderer crash", Limit: 25,
	})
	if err != nil {
		t.Fatalf("search issues: %v", err)
	}
	assertGlobalWorkItemIDs(t, searched.Items, fixture.issueIDs["public"])
	if len(searched.Items[0].Labels) != 1 || searched.Items[0].Labels[0].Name != "bug" ||
		len(searched.Items[0].Assignees) != 0 || searched.Items[0].CommentCount != 1 {
		t.Fatalf("issue list metadata = %+v", searched.Items[0])
	}
	japanese, err := fixture.store.ListGlobalIssues(ctx, fixture.viewer, GlobalWorkItemFilter{
		State: "open", Scope: "all", Query: "レンダラー", Limit: 25,
	})
	if err != nil {
		t.Fatalf("search Japanese issues: %v", err)
	}
	assertGlobalWorkItemIDs(t, japanese.Items, fixture.issueIDs["public"])

	first, err := fixture.store.ListGlobalIssues(ctx, fixture.viewer, GlobalWorkItemFilter{
		State: "all", Scope: "all", Limit: 1,
	})
	if err != nil || len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatalf("first issue page = %+v, err=%v", first, err)
	}
	second, err := fixture.store.ListGlobalIssues(ctx, fixture.viewer, GlobalWorkItemFilter{
		State: "all", Scope: "all", Cursor: first.NextCursor, Limit: 1,
	})
	if err != nil || len(second.Items) != 1 || second.Items[0].ID == first.Items[0].ID {
		t.Fatalf("second issue page = %+v, err=%v", second, err)
	}

	_, err = fixture.store.ListGlobalIssues(ctx, fixture.viewer, GlobalWorkItemFilter{
		State: "open", Scope: "all", Cursor: "invalid", Limit: 25,
	})
	if err != ErrInvalidInput {
		t.Fatalf("invalid cursor error = %v, want ErrInvalidInput", err)
	}
}

func TestGlobalPullRequestsIncludeDirectAndTeamReviewRequests(t *testing.T) {
	fixture := seedGlobalWorkItems(t)
	ctx := context.Background()

	reviewRequested, err := fixture.store.ListGlobalPullRequests(ctx, fixture.viewer, GlobalWorkItemFilter{
		State: "open", Scope: "review_requested", Limit: 25,
	})
	if err != nil {
		t.Fatalf("list review requested pull requests: %v", err)
	}
	assertGlobalWorkItemIDs(t, reviewRequested.Items,
		fixture.pullIDs["public"], fixture.pullIDs["internal"])

	involved, err := fixture.store.ListGlobalPullRequests(ctx, fixture.viewer, GlobalWorkItemFilter{
		State: "all", Scope: "involved", Limit: 25,
	})
	if err != nil {
		t.Fatalf("list involved pull requests: %v", err)
	}
	assertGlobalWorkItemIDs(t, involved.Items,
		fixture.pullIDs["public"], fixture.pullIDs["internal"], fixture.pullIDs["team"])

	created, err := fixture.store.ListGlobalPullRequests(ctx, fixture.viewer, GlobalWorkItemFilter{
		State: "merged", Scope: "created", Limit: 25,
	})
	if err != nil {
		t.Fatalf("list created pull requests: %v", err)
	}
	assertGlobalWorkItemIDs(t, created.Items, fixture.pullIDs["team"])

	var publicItem *GlobalWorkItem
	for index := range involved.Items {
		if involved.Items[index].ID == fixture.pullIDs["public"] {
			publicItem = &involved.Items[index]
		}
	}
	if publicItem == nil || len(publicItem.Assignees) != 1 || publicItem.CommentCount != 1 ||
		publicItem.ApprovalCount != 1 || publicItem.SourceBranch != "feature/public" ||
		publicItem.TargetBranch != "main" {
		t.Fatalf("pull request list metadata = %+v", publicItem)
	}
}

func TestGlobalWorkItemsRejectInactiveViewer(t *testing.T) {
	fixture := seedGlobalWorkItems(t)
	ctx := context.Background()
	mustIdentityExec(t, fixture.pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, fixture.viewer.ID)
	page, err := fixture.store.ListGlobalIssues(ctx, fixture.viewer, GlobalWorkItemFilter{
		State: "all", Scope: "all", Limit: 25,
	})
	if err != nil {
		t.Fatalf("list issues for inactive viewer: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("inactive viewer received work items: %+v", page.Items)
	}
}

func seedGlobalWorkItems(t *testing.T) globalWorkItemFixture {
	t.Helper()
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	viewer := platformTestUser("work-viewer-" + suffix)
	owner := platformTestUser("work-owner-" + suffix)
	organizationID := uuid.NewString()
	teamID := uuid.NewString()
	for _, user := range []User{viewer, owner} {
		mustIdentityExec(t, pool, `INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)`,
			user.ID, user.Username, user.DisplayName)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Work items', 'public', $3)
	`, organizationID, "work-items-"+suffix, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'owner'), ($1, $3, 'member')
	`, organizationID, owner.ID, viewer.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO teams (id, organization_id, slug, display_name, created_by)
		VALUES ($1, $2, 'reviewers', 'Reviewers', $3)
	`, teamID, organizationID, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO team_memberships (team_id, user_id, role) VALUES ($1, $2, 'member')
	`, teamID, viewer.ID)

	repositories := map[string]string{
		"public": uuid.NewString(), "internal": uuid.NewString(), "team": uuid.NewString(),
		"hidden": uuid.NewString(), "archived": uuid.NewString(),
	}
	for name, repositoryID := range repositories {
		visibility := name
		archivedAt := any(nil)
		if name == "team" || name == "hidden" {
			visibility = "private"
		}
		if name == "archived" {
			visibility = "public"
			archivedAt = time.Now().UTC()
		}
		slug := "work-" + name + "-" + suffix
		mustIdentityExec(t, pool, `
			INSERT INTO repositories (
				id, organization_id, slug, display_name, visibility, archived_at,
				lore_repository_id, lore_url, default_branch, created_by
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'main', $9)
		`, repositoryID, organizationID, slug, "Work "+name, visibility, archivedAt,
			canonicalTestLoreID(repositoryID), "lore://"+slug, owner.ID)
		mustIdentityExec(t, pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, repositoryID)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO team_repository_roles (team_id, repository_id, role, created_by)
		VALUES ($1, $2, 'read', $3)
	`, teamID, repositories["team"], owner.ID)

	issueIDs := map[string]string{}
	seedIssue := func(name, title, state string, author User, assigned bool, updated time.Time) string {
		t.Helper()
		issueID := uuid.NewString()
		issueIDs[name] = issueID
		mustIdentityExec(t, pool, `
			INSERT INTO issues (
				id, repository_id, number, title, body, state, author_id, created_at, updated_at
			) VALUES ($1, $2, 1, $3, $4, $5, $6, $7, $7)
		`, issueID, repositories[name], title, "Body for "+title, state, author.ID, updated)
		if assigned {
			mustIdentityExec(t, pool, `
				INSERT INTO issue_assignees (issue_id, repository_id, user_id, assigned_by)
				VALUES ($1, $2, $3, $4)
			`, issueID, repositories[name], viewer.ID, owner.ID)
		}
		return issueID
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	publicIssue := seedIssue("public", "Renderer crash レンダラー停止", "open", viewer, false, now.Add(-time.Minute))
	seedIssue("internal", "Internal task", "open", owner, true, now.Add(-2*time.Minute))
	seedIssue("team", "Partner task", "closed", owner, true, now.Add(-3*time.Minute))
	seedIssue("hidden", "Hidden task", "open", viewer, false, now.Add(-4*time.Minute))
	seedIssue("archived", "Archived task", "open", viewer, false, now.Add(-5*time.Minute))
	labelID := uuid.NewString()
	mustIdentityExec(t, pool, `
		INSERT INTO labels (id, repository_id, name, description, color)
		VALUES ($1, $2, 'bug', 'Defect', 'D73A4A')
	`, labelID, repositories["public"])
	mustIdentityExec(t, pool, `INSERT INTO issue_labels (issue_id, label_id) VALUES ($1, $2)`,
		publicIssue, labelID)
	mustIdentityExec(t, pool, `
		INSERT INTO issue_comments (id, issue_id, author_id, body) VALUES ($1, $2, $3, 'Reproduced')
	`, uuid.NewString(), publicIssue, owner.ID)

	pullIDs := map[string]string{}
	seedPull := func(name, state string, author User, updated time.Time) string {
		t.Helper()
		pullID := uuid.NewString()
		pullIDs[name] = pullID
		mustIdentityExec(t, pool, `
			INSERT INTO merge_requests (
				id, repository_id, number, title, body, state, source_branch, target_branch,
				source_revision, target_revision, author_id, created_at, updated_at
			) VALUES ($1, $2, 1, $3, 'Pull body', $4, $5, 'main', $6, $7, $8, $9, $9)
		`, pullID, repositories[name], "Pull "+name, state, "feature/"+name,
			strings.Repeat("a", 64), strings.Repeat("b", 64), author.ID, updated)
		return pullID
	}
	publicPull := seedPull("public", "open", owner, now.Add(-time.Minute))
	internalPull := seedPull("internal", "open", owner, now.Add(-2*time.Minute))
	seedPull("team", "merged", viewer, now.Add(-3*time.Minute))
	hiddenPull := seedPull("hidden", "open", owner, now.Add(-4*time.Minute))
	mustIdentityExec(t, pool, `
		INSERT INTO merge_request_assignees (
			merge_request_id, repository_id, user_id, assigned_by
		) VALUES ($1, $2, $3, $4)
	`, publicPull, repositories["public"], viewer.ID, owner.ID)
	for _, request := range []struct {
		pullID     string
		repository string
		userID     any
		teamID     any
	}{
		{publicPull, repositories["public"], viewer.ID, nil},
		{internalPull, repositories["internal"], nil, teamID},
		{hiddenPull, repositories["hidden"], viewer.ID, nil},
	} {
		mustIdentityExec(t, pool, `
			INSERT INTO merge_request_review_requests (
				id, organization_id, repository_id, merge_request_id,
				reviewer_user_id, reviewer_team_id, requested_by
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, uuid.NewString(), organizationID, request.repository, request.pullID,
			request.userID, request.teamID, owner.ID)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO merge_request_comments (id, merge_request_id, author_id, body)
		VALUES ($1, $2, $3, 'Looks good')
	`, uuid.NewString(), publicPull, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO merge_request_reviews (
			id, merge_request_id, reviewer_id, source_revision, decision, body
		) VALUES ($1, $2, $3, $4, 'approved', '')
	`, uuid.NewString(), publicPull, viewer.ID, strings.Repeat("a", 64))

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, organizationID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id IN ($1, $2)`, viewer.ID, owner.ID)
	})
	return globalWorkItemFixture{
		pool: pool, store: store, viewer: viewer, owner: owner,
		organizationID: organizationID, teamID: teamID,
		publicRepository: repositories["public"], internalRepo: repositories["internal"],
		teamRepository: repositories["team"], hiddenRepository: repositories["hidden"],
		archivedRepo: repositories["archived"], issueIDs: issueIDs, pullIDs: pullIDs,
	}
}

func assertGlobalWorkItemIDs(t *testing.T, items []GlobalWorkItem, expected ...string) {
	t.Helper()
	actual := make(map[string]bool, len(items))
	for _, item := range items {
		actual[item.ID] = true
	}
	if len(actual) != len(expected) {
		t.Fatalf("work item ids = %v, want %v", actual, expected)
	}
	for _, id := range expected {
		if !actual[id] {
			t.Fatalf("work item ids = %v, missing %s", actual, id)
		}
	}
}
