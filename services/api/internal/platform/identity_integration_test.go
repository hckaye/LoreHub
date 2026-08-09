package platform

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lorehub/lorehub/services/api/internal/database"
)

func identityIntegrationStore(t *testing.T) (*pgxpool.Pool, *Store) {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set; skipping PostgreSQL identity integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, databaseURL, 5*time.Second)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.Migrate(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("migrate database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool, NewStore(pool)
}

func TestIdentitySearchFiltersPrivateResources(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	alice := platformTestUser("alice-" + suffix)
	bob := platformTestUser("bob-" + suffix)
	orgID := uuid.NewString()
	privateID := uuid.NewString()
	publicID := uuid.NewString()
	for _, user := range []User{alice, bob} {
		mustIdentityExec(t, pool, `INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)`,
			user.ID, user.Username, user.DisplayName)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Search organization', 'public', $3)
	`, orgID, "search-"+suffix, alice.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, 'owner')
	`, orgID, alice.ID)
	for _, fixture := range []struct {
		id         string
		slug       string
		visibility string
	}{
		{id: publicID, slug: "public-" + suffix, visibility: "public"},
		{id: privateID, slug: "private-" + suffix, visibility: "private"},
	} {
		mustIdentityExec(t, pool, `
			INSERT INTO repositories (
				id, organization_id, slug, display_name, visibility,
				lore_repository_id, lore_url, default_branch, created_by
			) VALUES ($1, $2, $3, 'Search repository', $4, $5, $6, 'main', $7)
		`, fixture.id, orgID, fixture.slug, fixture.visibility, "lore-"+fixture.slug,
			"lore://"+fixture.slug, alice.ID)
		mustIdentityExec(t, pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, fixture.id)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role) VALUES ($1, $2, 'read')
	`, privateID, bob.ID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id IN ($1, $2)`, alice.ID, bob.ID)
	})

	anonymous, err := store.Search(ctx, nil, "Search", "repositories", 20)
	if err != nil {
		t.Fatalf("anonymous search: %v", err)
	}
	if len(anonymous.Repositories) != 1 || anonymous.Repositories[0].Slug != "public-"+suffix {
		t.Fatalf("anonymous search leaked private repository: %+v", anonymous.Repositories)
	}
	member, err := store.Search(ctx, &bob, "Search", "repositories", 20)
	if err != nil {
		t.Fatalf("member search: %v", err)
	}
	if len(member.Repositories) != 2 {
		t.Fatalf("member search did not include private repository: %+v", member.Repositories)
	}
}

func TestIdentityNotificationsProjectRealOutboxEvent(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	alice := platformTestUser("notify-" + suffix)
	orgID := uuid.NewString()
	repositoryID := uuid.NewString()
	issueID := uuid.NewString()
	eventID := uuid.NewString()
	orgSlug := "notify-org-" + suffix
	repoSlug := "notify-repo-" + suffix
	mustIdentityExec(t, pool, `INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)`,
		alice.ID, alice.Username, alice.DisplayName)
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Notify organization', 'private', $3)
	`, orgID, orgSlug, alice.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, 'owner')
	`, orgID, alice.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, $3, 'Notify repository', 'private', $4, $5, 'main', $6)
	`, repositoryID, orgID, repoSlug, "lore-"+repoSlug, "lore://"+repoSlug, alice.ID)
	mustIdentityExec(t, pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, repositoryID)
	mustIdentityExec(t, pool, `
		INSERT INTO issues (id, repository_id, number, title, author_id)
		VALUES ($1, $2, 1, 'A real notification', $3)
	`, issueID, repositoryID, alice.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, 'issue.created', $2, '{"title":"A real notification","body":"From outbox"}')
	`, eventID, issueID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, alice.ID)
	})
	page, err := store.ListNotifications(ctx, alice, false, 10)
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("unexpected notification page: %+v", page)
	}
	if page.Items[0].Title != "A real notification" || page.Items[0].ReadAt != nil {
		t.Fatalf("notification did not preserve outbox data: %+v", page.Items[0])
	}
	if err := store.MarkNotificationRead(ctx, alice, page.Items[0].ID); err != nil {
		t.Fatalf("mark notification read: %v", err)
	}
	unread, err := store.UnreadNotificationCount(ctx, alice)
	if err != nil {
		t.Fatalf("count unread notifications: %v", err)
	}
	if unread != 0 {
		t.Fatalf("unread count = %d, want 0", unread)
	}
	preferences, err := store.NotificationPreferences(ctx, alice)
	if err != nil || !preferences.InAppEnabled {
		t.Fatalf("default notification preferences = %+v, err=%v", preferences, err)
	}
	repositoryNotificationsOff := false
	preferences, err = store.UpdateNotificationPreferences(ctx, alice, UpdateNotificationPreferencesInput{
		RepositoryEnabled: &repositoryNotificationsOff,
	})
	if err != nil || preferences.RepositoryEnabled {
		t.Fatalf("updated notification preferences = %+v, err=%v", preferences, err)
	}
}

func TestIdentityOrganizationAndTeamAccess(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	owner := platformTestUser("owner-" + suffix)
	member := platformTestUser("member-" + suffix)
	orgID := uuid.NewString()
	orgSlug := "access-org-" + suffix
	for _, user := range []User{owner, member} {
		mustIdentityExec(t, pool, `INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)`,
			user.ID, user.Username, user.DisplayName)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Access organization', 'public', $3)
	`, orgID, orgSlug, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, 'owner')
	`, orgID, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, 'member')
	`, orgID, member.ID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id IN ($1, $2)`, owner.ID, member.ID)
	})

	organization, err := store.Organization(ctx, nil, orgSlug)
	if err != nil || organization.Role != "" {
		t.Fatalf("anonymous organization = %+v, err=%v", organization, err)
	}
	if _, err := store.OrganizationRepositories(ctx, nil, orgSlug); err != nil {
		t.Fatalf("anonymous organization repositories: %v", err)
	}
	if _, err := store.Teams(ctx, nil, orgSlug); err != nil {
		t.Fatalf("anonymous teams: %v", err)
	}
	team, err := store.CreateTeam(ctx, owner, orgSlug, CreateTeamInput{
		Slug: "reviewers-" + suffix, DisplayName: "Reviewers", Description: "Review team",
	})
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	if team.ViewerRole != "maintainer" {
		t.Fatalf("team viewer role = %q, want maintainer", team.ViewerRole)
	}
	if _, err := store.AddTeamMember(ctx, owner, orgSlug, team.Slug, member.Username, "member"); err != nil {
		t.Fatalf("add team member: %v", err)
	}
	members, err := store.TeamMembers(ctx, &owner, orgSlug, team.Slug)
	if err != nil || len(members) != 2 {
		t.Fatalf("team members = %+v, err=%v", members, err)
	}
}

func platformTestUser(username string) User {
	return User{ID: uuid.NewString(), Username: username, DisplayName: username}
}

func mustIdentityExec(t *testing.T, pool *pgxpool.Pool, query string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), query, args...); err != nil {
		t.Fatalf("identity fixture SQL: %v", err)
	}
}
