package platform

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
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
	mustIdentityExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, 'member')
	`, orgID, bob.ID)
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
		`, fixture.id, orgID, fixture.slug, fixture.visibility, canonicalTestLoreID(fixture.id),
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

func TestIdentityVisibilityMatrixCoversEveryRepositoryProjection(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	owner := platformTestUser("matrix-owner-" + suffix)
	maintainer := platformTestUser("matrix-maintainer-" + suffix)
	member := platformTestUser("matrix-member-" + suffix)
	grantee := platformTestUser("matrix-grantee-" + suffix)
	suspendedOwner := platformTestUser("matrix-suspended-owner-" + suffix)
	orgID := uuid.NewString()
	orgSlug := "matrix-org-" + suffix
	repositories := map[string]string{
		"public":   uuid.NewString(),
		"internal": uuid.NewString(),
		"private":  uuid.NewString(),
		"archived": uuid.NewString(),
	}
	for _, user := range []User{owner, maintainer, member, grantee, suspendedOwner} {
		mustIdentityExec(t, pool, `INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)`,
			user.ID, user.Username, user.DisplayName)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Visibility matrix organization', 'public', $3)
	`, orgID, orgSlug, owner.ID)
	for _, membership := range []struct {
		user string
		role string
	}{
		{owner.ID, "owner"}, {maintainer.ID, "maintainer"}, {member.ID, "member"},
		{grantee.ID, "member"},
		{suspendedOwner.ID, "owner"},
	} {
		mustIdentityExec(t, pool, `
			INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, $3)
		`, orgID, membership.user, membership.role)
	}
	mustIdentityExec(t, pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, suspendedOwner.ID)
	for visibility, repositoryID := range repositories {
		repositoryName := visibility
		storageVisibility := visibility
		archivedAt := any(nil)
		if visibility == "archived" {
			archivedAt = time.Now().UTC()
			storageVisibility = "public"
		}
		repositorySlug := "visibility-" + repositoryName + "-" + suffix
		mustIdentityExec(t, pool, `
			INSERT INTO repositories (
				id, organization_id, slug, display_name, visibility, archived_at,
				lore_repository_id, lore_url, default_branch, created_by
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'main', $9)
		`, repositoryID, orgID, repositorySlug, "Visibility "+repositoryName, storageVisibility, archivedAt,
			canonicalTestLoreID(repositoryID), "lore://"+repositorySlug, owner.ID)
		mustIdentityExec(t, pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, repositoryID)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role) VALUES ($1, $2, 'read')
	`, repositories["private"], grantee.ID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id IN ($1, $2, $3, $4, $5)`, owner.ID, maintainer.ID,
			member.ID, grantee.ID, suspendedOwner.ID)
	})

	assertVisibleRepositories := func(label string, repositories []Repository, expected ...string) {
		t.Helper()
		seen := make(map[string]bool, len(repositories))
		for _, repository := range repositories {
			seen[repository.Slug] = true
		}
		if len(seen) != len(expected) {
			t.Fatalf("%s returned %d repositories, want %d: %+v", label, len(seen), len(expected), repositories)
		}
		for _, visibility := range expected {
			slug := "visibility-" + visibility + "-" + suffix
			if !seen[slug] {
				t.Fatalf("%s omitted %s: %+v", label, slug, repositories)
			}
		}
	}

	search, err := store.Search(ctx, nil, "visibility-", "repositories", 20)
	if err != nil {
		t.Fatalf("anonymous search: %v", err)
	}
	assertVisibleRepositories("anonymous search", search.Repositories, "public")
	search, err = store.Search(ctx, &member, "visibility-", "repositories", 20)
	if err != nil {
		t.Fatalf("organization member search: %v", err)
	}
	assertVisibleRepositories("organization member search", search.Repositories, "public", "internal")
	search, err = store.Search(ctx, &grantee, "visibility-", "repositories", 20)
	if err != nil {
		t.Fatalf("explicit grant search: %v", err)
	}
	assertVisibleRepositories("explicit grant search", search.Repositories, "public", "internal", "private")
	search, err = store.Search(ctx, &owner, "visibility-", "repositories", 20)
	if err != nil {
		t.Fatalf("organization owner search: %v", err)
	}
	assertVisibleRepositories("organization owner search", search.Repositories, "public", "internal", "private")
	search, err = store.Search(ctx, &maintainer, "visibility-", "repositories", 20)
	if err != nil {
		t.Fatalf("organization maintainer search: %v", err)
	}
	assertVisibleRepositories("organization maintainer search", search.Repositories, "public", "internal")
	search, err = store.Search(ctx, &suspendedOwner, "visibility-", "repositories", 20)
	if err != nil {
		t.Fatalf("suspended owner search: %v", err)
	}
	assertVisibleRepositories("suspended owner search", search.Repositories)

	profile, err := store.UserProfile(ctx, nil, owner.Username)
	if err != nil || profile.RepositoryCount != 1 {
		t.Fatalf("anonymous profile count = %d, err=%v; want 1", profile.RepositoryCount, err)
	}
	profile, err = store.UserProfile(ctx, &member, owner.Username)
	if err != nil || profile.RepositoryCount != 2 {
		t.Fatalf("organization member profile count = %d, err=%v; want 2", profile.RepositoryCount, err)
	}
	profile, err = store.UserProfile(ctx, &grantee, owner.Username)
	if err != nil || profile.RepositoryCount != 3 {
		t.Fatalf("explicit grant profile count = %d, err=%v; want 3", profile.RepositoryCount, err)
	}
	profile, err = store.UserProfile(ctx, &owner, owner.Username)
	if err != nil || profile.RepositoryCount != 3 {
		t.Fatalf("organization owner profile count = %d, err=%v; want 3", profile.RepositoryCount, err)
	}
	profile, err = store.UserProfile(ctx, &maintainer, owner.Username)
	if err != nil || profile.RepositoryCount != 2 {
		t.Fatalf("organization maintainer profile count = %d, err=%v; want 2", profile.RepositoryCount, err)
	}
	profile, err = store.UserProfile(ctx, &suspendedOwner, owner.Username)
	if err != nil || profile.RepositoryCount != 0 {
		t.Fatalf("suspended owner profile count = %d, err=%v; want 0", profile.RepositoryCount, err)
	}
	userRepositories, err := store.UserRepositories(ctx, nil, owner.Username)
	if err != nil {
		t.Fatalf("anonymous profile repositories: %v", err)
	}
	assertVisibleRepositories("anonymous profile repositories", userRepositories, "public")
	userRepositories, err = store.UserRepositories(ctx, &member, owner.Username)
	if err != nil {
		t.Fatalf("organization member profile repositories: %v", err)
	}
	assertVisibleRepositories("organization member profile repositories", userRepositories, "public", "internal")
	userRepositories, err = store.UserRepositories(ctx, &grantee, owner.Username)
	if err != nil {
		t.Fatalf("explicit grant profile repositories: %v", err)
	}
	assertVisibleRepositories("explicit grant profile repositories", userRepositories, "public", "internal", "private")
	userRepositories, err = store.UserRepositories(ctx, &owner, owner.Username)
	if err != nil {
		t.Fatalf("organization owner profile repositories: %v", err)
	}
	assertVisibleRepositories("organization owner profile repositories", userRepositories, "public", "internal", "private")
	userRepositories, err = store.UserRepositories(ctx, &maintainer, owner.Username)
	if err != nil {
		t.Fatalf("organization maintainer profile repositories: %v", err)
	}
	assertVisibleRepositories("organization maintainer profile repositories", userRepositories, "public", "internal")
	userRepositories, err = store.UserRepositories(ctx, &suspendedOwner, owner.Username)
	if err != nil {
		t.Fatalf("suspended owner profile repositories: %v", err)
	}
	assertVisibleRepositories("suspended owner profile repositories", userRepositories)

	organization, err := store.Organization(ctx, nil, orgSlug)
	if err != nil || organization.RepositoryCount != 1 {
		t.Fatalf("anonymous organization count = %d, err=%v; want 1", organization.RepositoryCount, err)
	}
	organization, err = store.Organization(ctx, &member, orgSlug)
	if err != nil || organization.RepositoryCount != 2 {
		t.Fatalf("organization member count = %d, err=%v; want 2", organization.RepositoryCount, err)
	}
	organization, err = store.Organization(ctx, &owner, orgSlug)
	if err != nil || organization.RepositoryCount != 3 {
		t.Fatalf("organization owner count = %d, err=%v; want 3", organization.RepositoryCount, err)
	}
	organization, err = store.Organization(ctx, &maintainer, orgSlug)
	if err != nil || organization.RepositoryCount != 2 {
		t.Fatalf("organization maintainer count = %d, err=%v; want 2", organization.RepositoryCount, err)
	}
	organization, err = store.Organization(ctx, &suspendedOwner, orgSlug)
	if err != nil || organization.RepositoryCount != 0 {
		t.Fatalf("suspended owner organization count = %d, err=%v; want 0", organization.RepositoryCount, err)
	}
	organizationRepositories, err := store.OrganizationRepositories(ctx, nil, orgSlug)
	if err != nil {
		t.Fatalf("anonymous organization repositories: %v", err)
	}
	assertVisibleRepositories("anonymous organization repositories", organizationRepositories, "public")
	organizationRepositories, err = store.OrganizationRepositories(ctx, &member, orgSlug)
	if err != nil {
		t.Fatalf("organization member repositories: %v", err)
	}
	assertVisibleRepositories("organization member repositories", organizationRepositories, "public", "internal")
	organizationRepositories, err = store.OrganizationRepositories(ctx, &grantee, orgSlug)
	if err != nil {
		t.Fatalf("explicit grant organization repositories: %v", err)
	}
	assertVisibleRepositories(
		"explicit grant organization repositories", organizationRepositories, "public", "internal", "private",
	)
	organizationRepositories, err = store.OrganizationRepositories(ctx, &owner, orgSlug)
	if err != nil {
		t.Fatalf("organization owner repositories: %v", err)
	}
	assertVisibleRepositories("organization owner repositories", organizationRepositories, "public", "internal", "private")
	organizationRepositories, err = store.OrganizationRepositories(ctx, &maintainer, orgSlug)
	if err != nil {
		t.Fatalf("organization maintainer repositories: %v", err)
	}
	assertVisibleRepositories("organization maintainer repositories", organizationRepositories, "public", "internal")
	organizationRepositories, err = store.OrganizationRepositories(ctx, &suspendedOwner, orgSlug)
	if err != nil {
		t.Fatalf("suspended owner repositories: %v", err)
	}
	assertVisibleRepositories("suspended owner repositories", organizationRepositories)

	dashboard, err := store.Dashboard(ctx, member)
	if err != nil {
		t.Fatalf("member dashboard: %v", err)
	}
	dashboardRepositories := make([]Repository, 0, len(dashboard.Repositories))
	for _, repository := range dashboard.Repositories {
		if strings.HasSuffix(repository.Slug, "-"+suffix) {
			dashboardRepositories = append(dashboardRepositories, repository)
		}
	}
	assertVisibleRepositories("member dashboard", dashboardRepositories, "public", "internal")
	for _, dashboardUser := range []struct {
		user     User
		expected []string
	}{
		{owner, []string{"public", "internal", "private"}},
		{maintainer, []string{"public", "internal"}},
		{suspendedOwner, []string{}},
	} {
		dashboard, err = store.Dashboard(ctx, dashboardUser.user)
		if err != nil {
			t.Fatalf("%s dashboard: %v", dashboardUser.user.Username, err)
		}
		dashboardRepositories = make([]Repository, 0, len(dashboard.Repositories))
		for _, repository := range dashboard.Repositories {
			if strings.HasSuffix(repository.Slug, "-"+suffix) {
				dashboardRepositories = append(dashboardRepositories, repository)
			}
		}
		assertVisibleRepositories(dashboardUser.user.Username+" dashboard", dashboardRepositories,
			dashboardUser.expected...)
	}
}

func TestIdentityNotificationsRespectRepositoryVisibilityAndActiveRecipients(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	owner := platformTestUser("notification-owner-" + suffix)
	member := platformTestUser("notification-member-" + suffix)
	grantee := platformTestUser("notification-grantee-" + suffix)
	teamUser := platformTestUser("notification-team-" + suffix)
	suspended := platformTestUser("notification-suspended-" + suffix)
	orgID := uuid.NewString()
	orgSlug := "notification-org-" + suffix
	teamID := uuid.NewString()
	repositoryIDs := map[string]string{
		"public":   uuid.NewString(),
		"internal": uuid.NewString(),
		"private":  uuid.NewString(),
	}
	users := []User{owner, member, grantee, teamUser, suspended}
	for _, user := range users {
		mustIdentityExec(t, pool, `INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)`,
			user.ID, user.Username, user.DisplayName)
	}
	mustIdentityExec(t, pool, `
		UPDATE users SET status = 'suspended' WHERE id = $1
	`, suspended.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Notification organization', 'public', $3)
	`, orgID, orgSlug, owner.ID)
	for _, userID := range []string{owner.ID, member.ID, grantee.ID, teamUser.ID, suspended.ID} {
		mustIdentityExec(t, pool, `
			INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, 'member')
		`, orgID, userID)
	}
	mustIdentityExec(t, pool, `
		UPDATE organization_memberships SET role = 'owner' WHERE organization_id = $1 AND user_id = $2
	`, orgID, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO teams (id, organization_id, slug, display_name, created_by)
		VALUES ($1, $2, $3, 'Notification team', $4)
	`, teamID, orgID, "notification-team-"+suffix, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO team_memberships (team_id, user_id, role) VALUES ($1, $2, 'member'), ($1, $3, 'member')
	`, teamID, teamUser.ID, suspended.ID)

	eventIDs := make([]string, 0, len(repositoryIDs))
	for _, visibility := range []string{"public", "internal", "private"} {
		repositoryID := repositoryIDs[visibility]
		repositorySlug := "notification-" + visibility + "-" + suffix
		mustIdentityExec(t, pool, `
			INSERT INTO repositories (
				id, organization_id, slug, display_name, visibility,
				lore_repository_id, lore_url, default_branch, created_by
			) VALUES ($1, $2, $3, $4, $5, $6, $7, 'main', $8)
		`, repositoryID, orgID, repositorySlug, "Notification "+visibility, visibility,
			canonicalTestLoreID(repositoryID), "lore://"+repositorySlug, owner.ID)
		mustIdentityExec(t, pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, repositoryID)
		issueID := uuid.NewString()
		eventID := uuid.NewString()
		eventIDs = append(eventIDs, eventID)
		mustIdentityExec(t, pool, `
			INSERT INTO issues (id, repository_id, number, title, author_id)
			VALUES ($1, $2, 1, $3, $4)
		`, issueID, repositoryID, "Notification "+visibility+" "+suffix, owner.ID)
		mustIdentityExec(t, pool, `
			INSERT INTO outbox_events (id, topic, event_key, payload)
			VALUES ($1, 'issue.created', $2, $3)
		`, eventID, issueID, `{"title":"Notification `+visibility+` `+suffix+`"}`)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role)
		VALUES ($1, $2, 'read'), ($3, $4, 'read'), ($5, $6, 'read'), ($5, $7, 'read')
	`, repositoryIDs["public"], owner.ID, repositoryIDs["internal"], grantee.ID, repositoryIDs["private"],
		grantee.ID, suspended.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO team_repository_roles (team_id, repository_id, role, created_by, active)
		VALUES ($1, $2, 'read', $3, true)
	`, teamID, repositoryIDs["private"], owner.ID)
	t.Cleanup(func() {
		for _, eventID := range eventIDs {
			_, _ = pool.Exec(ctx, `DELETE FROM outbox_events WHERE id = $1`, eventID)
		}
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, orgID)
		for _, user := range users {
			_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, user.ID)
		}
	})

	if _, err := store.ListNotifications(ctx, owner, false, 20); err != nil {
		t.Fatalf("project repository notifications: %v", err)
	}
	countFor := func(userID, eventID string) int64 {
		t.Helper()
		var count int64
		if err := pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM notifications WHERE recipient_id = $1 AND source_event_id = $2
		`, userID, eventID).Scan(&count); err != nil {
			t.Fatalf("count notification for %s: %v", userID, err)
		}
		return count
	}
	for _, user := range users {
		if _, err := store.ListNotifications(ctx, user, false, 20); err != nil {
			t.Fatalf("list notifications for %s: %v", user.Username, err)
		}
	}
	want := map[string]map[string]bool{
		owner.ID:     {"public": true, "internal": true, "private": true},
		member.ID:    {"internal": true},
		grantee.ID:   {"internal": true, "private": true},
		teamUser.ID:  {"internal": true, "private": true},
		suspended.ID: {},
	}
	for index, visibility := range []string{"public", "internal", "private"} {
		for _, user := range users {
			if got := countFor(user.ID, eventIDs[index]); got != boolCount(want[user.ID][visibility]) {
				t.Fatalf("%s received %s notification count %d, want %d", user.Username, visibility,
					got, boolCount(want[user.ID][visibility]))
			}
		}
	}
	assertNoNotificationContent := func(user User, page NotificationPage, content string) {
		t.Helper()
		for _, item := range page.Items {
			if strings.Contains(item.Title, content) || strings.Contains(item.Body, content) {
				t.Fatalf("%s notification still disclosed %q: %+v", user.Username, content, item)
			}
		}
	}
	assertRevoked := func(user User, expectedUnread int64, hideInternal bool) {
		t.Helper()
		page, err := store.ListNotifications(ctx, user, false, 20)
		if err != nil {
			t.Fatalf("list notifications after revoke for %s: %v", user.Username, err)
		}
		assertNoNotificationContent(user, page, "private "+suffix)
		if hideInternal {
			assertNoNotificationContent(user, page, "internal "+suffix)
		}
		if hideInternal {
			if count := countFor(user.ID, eventIDs[1]); count != 0 {
				t.Fatalf("%s retained inaccessible internal notification %s", user.Username, eventIDs[1])
			}
		}
		if count := countFor(user.ID, eventIDs[2]); count != 0 {
			t.Fatalf("%s retained inaccessible private notification %s", user.Username, eventIDs[2])
		}
		unread, err := store.UnreadNotificationCount(ctx, user)
		if err != nil {
			t.Fatalf("unread count after revoke for %s: %v", user.Username, err)
		}
		if unread != expectedUnread {
			t.Fatalf("unread count after revoke for %s = %d, want %d", user.Username, unread, expectedUnread)
		}
	}
	mustIdentityExec(t, pool, `
		DELETE FROM repository_memberships WHERE repository_id = $1 AND user_id = $2
	`, repositoryIDs["private"], grantee.ID)
	assertRevoked(grantee, 1, false)
	mustIdentityExec(t, pool, `
		DELETE FROM team_repository_roles WHERE team_id = $1 AND repository_id = $2
	`, teamID, repositoryIDs["private"])
	assertRevoked(teamUser, 1, false)
	mustIdentityExec(t, pool, `
		DELETE FROM organization_memberships WHERE organization_id = $1 AND user_id = $2
	`, orgID, member.ID)
	assertRevoked(member, 0, true)
	mustIdentityExec(t, pool, `
		DELETE FROM organization_memberships WHERE organization_id = $1 AND user_id = $2
	`, orgID, owner.ID)
	assertRevoked(owner, 1, true)
}

func TestRepositorySettingsRequireExplicitAdminOrOrganizationOwner(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	owner := platformTestUser("settings-owner-" + suffix)
	maintainer := platformTestUser("settings-maintainer-" + suffix)
	repositoryAdmin := platformTestUser("settings-admin-" + suffix)
	orgID := uuid.NewString()
	repositoryID := uuid.NewString()
	orgSlug := "settings-org-" + suffix
	repositorySlug := "settings-repository-" + suffix
	for _, user := range []User{owner, maintainer, repositoryAdmin} {
		mustIdentityExec(t, pool, `INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)`,
			user.ID, user.Username, user.DisplayName)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Settings organization', 'public', $3)
	`, orgID, orgSlug, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'owner'), ($1, $3, 'maintainer')
	`, orgID, owner.ID, maintainer.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, $3, 'Original settings', 'private', $4, $5, 'main', $6)
	`, repositoryID, orgID, repositorySlug, canonicalTestLoreID(repositoryID), "lore://"+repositorySlug, owner.ID)
	mustIdentityExec(t, pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, repositoryID)
	mustIdentityExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role) VALUES ($1, $2, 'admin')
	`, repositoryID, repositoryAdmin.ID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id IN ($1, $2, $3)`, owner.ID, maintainer.ID, repositoryAdmin.ID)
	})

	maintainerName := "Maintainer must not change repository"
	if _, err := store.UpdateRepositorySettings(ctx, maintainer, orgSlug, repositorySlug,
		UpdateRepositorySettingsInput{DisplayName: &maintainerName}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("organization maintainer private update error = %v, want not found", err)
	}
	var displayName string
	if err := pool.QueryRow(ctx, `SELECT display_name FROM repositories WHERE id = $1`, repositoryID).
		Scan(&displayName); err != nil {
		t.Fatalf("read repository after denied update: %v", err)
	}
	if displayName != "Original settings" {
		t.Fatalf("denied update changed display name to %q", displayName)
	}
	adminName := "Repository admin update"
	updated, err := store.UpdateRepositorySettings(ctx, repositoryAdmin, orgSlug, repositorySlug,
		UpdateRepositorySettingsInput{DisplayName: &adminName})
	if err != nil || updated.DisplayName != adminName {
		t.Fatalf("repository admin update = %+v, err=%v", updated, err)
	}
	ownerName := "Organization owner update"
	updated, err = store.UpdateRepositorySettings(ctx, owner, orgSlug, repositorySlug,
		UpdateRepositorySettingsInput{DisplayName: &ownerName})
	if err != nil || updated.DisplayName != ownerName {
		t.Fatalf("organization owner update = %+v, err=%v", updated, err)
	}
}

func boolCount(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func TestIdentityNotificationProjectionLedgerIsBoundedIdempotentAndConcurrent(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	owner := platformTestUser("ledger-owner-" + suffix)
	orgID := uuid.NewString()
	repositoryID := uuid.NewString()
	orgSlug := "ledger-org-" + suffix
	repositorySlug := "ledger-repository-" + suffix
	zeroIssueID := uuid.NewString()
	zeroEventID := uuid.NewString()
	eventIDs := []string{zeroEventID}
	mustIdentityExec(t, pool, `INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)`,
		owner.ID, owner.Username, owner.DisplayName)
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Ledger organization', 'public', $3)
	`, orgID, orgSlug, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, 'owner')
	`, orgID, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, $3, 'Ledger repository', 'public', $4, $5, 'main', $6)
	`, repositoryID, orgID, repositorySlug, canonicalTestLoreID(repositoryID), "lore://"+repositorySlug, owner.ID)
	mustIdentityExec(t, pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, repositoryID)
	mustIdentityExec(t, pool, `
		INSERT INTO issues (id, repository_id, number, title, author_id)
		VALUES ($1, $2, 1, 'Zero recipient event', $3)
	`, zeroIssueID, repositoryID, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO outbox_events (id, topic, event_key, payload, created_at)
		VALUES ($1, 'issue.created', $2, '{}', to_timestamp(0))
	`, zeroEventID, zeroIssueID)
	for index := 0; index < 101; index++ {
		eventID := uuid.NewString()
		eventIDs = append(eventIDs, eventID)
		mustIdentityExec(t, pool, `
			INSERT INTO outbox_events (id, topic, event_key, payload, created_at)
			VALUES ($1, 'issue.created', $2, '{}', to_timestamp(0))
		`, eventID, uuid.NewString())
	}
	t.Cleanup(func() {
		for _, eventID := range eventIDs {
			_, _ = pool.Exec(ctx, `DELETE FROM outbox_events WHERE id = $1`, eventID)
		}
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, owner.ID)
	})
	ledgerCount := func() int {
		t.Helper()
		count := 0
		for _, eventID := range eventIDs {
			var found bool
			if err := pool.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM notification_projection_ledger WHERE source_event_id = $1
				)
			`, eventID).Scan(&found); err != nil {
				t.Fatalf("check notification ledger: %v", err)
			}
			if found {
				count++
			}
		}
		return count
	}
	if _, err := store.ListNotifications(ctx, owner, false, 10); err != nil {
		t.Fatalf("first bounded projection: %v", err)
	}
	firstCount := ledgerCount()
	if firstCount == len(eventIDs) || firstCount > notificationProjectionBatchSize {
		t.Fatalf("first projection processed %d of %d events", firstCount, len(eventIDs))
	}
	if _, err := store.ListNotifications(ctx, owner, false, 10); err != nil {
		t.Fatalf("second bounded projection: %v", err)
	}
	if got := ledgerCount(); got != len(eventIDs) {
		t.Fatalf("second projection ledger count = %d, want %d", got, len(eventIDs))
	}
	var zeroStatus string
	if err := pool.QueryRow(ctx, `
		SELECT status FROM notification_projection_ledger WHERE source_event_id = $1
	`, zeroEventID).Scan(&zeroStatus); err != nil {
		t.Fatalf("read zero-recipient ledger status: %v", err)
	}
	if zeroStatus != "processed" {
		t.Fatalf("zero-recipient status = %q, want processed", zeroStatus)
	}
	var zeroNotifications int64
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM notifications WHERE source_event_id = $1
	`, zeroEventID).Scan(&zeroNotifications); err != nil {
		t.Fatalf("count zero-recipient notifications: %v", err)
	}
	if zeroNotifications != 0 {
		t.Fatalf("zero-recipient event created %d notifications", zeroNotifications)
	}
	if _, err := store.ListNotifications(ctx, owner, false, 10); err != nil {
		t.Fatalf("repeat projection: %v", err)
	}
	if got := ledgerCount(); got != len(eventIDs) {
		t.Fatalf("repeat projection ledger count = %d, want %d", got, len(eventIDs))
	}

	concurrentIssueID := uuid.NewString()
	concurrentEventID := uuid.NewString()
	eventIDs = append(eventIDs, concurrentEventID)
	mustIdentityExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role) VALUES ($1, $2, 'read')
	`, repositoryID, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO issues (id, repository_id, number, title, author_id)
		VALUES ($1, $2, 2, 'Concurrent event', $3)
	`, concurrentIssueID, repositoryID, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, 'issue.created', $2, '{"title":"Concurrent event"}')
	`, concurrentEventID, concurrentIssueID)
	projectionErrors := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for index := 0; index < 2; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			_, err := store.ListNotifications(ctx, owner, false, 10)
			projectionErrors <- err
		}()
	}
	waitGroup.Wait()
	close(projectionErrors)
	for err := range projectionErrors {
		if err != nil {
			t.Fatalf("concurrent projection: %v", err)
		}
	}
	var concurrentNotifications int64
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM notifications WHERE source_event_id = $1 AND recipient_id = $2
	`, concurrentEventID, owner.ID).Scan(&concurrentNotifications); err != nil {
		t.Fatalf("count concurrent notifications: %v", err)
	}
	if concurrentNotifications != 1 {
		t.Fatalf("concurrent projection created %d notifications, want 1", concurrentNotifications)
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
	`, repositoryID, orgID, repoSlug, canonicalTestLoreID(repositoryID), "lore://"+repoSlug, alice.ID)
	mustIdentityExec(t, pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, repositoryID)
	mustIdentityExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role) VALUES ($1, $2, 'admin')
	`, repositoryID, alice.ID)
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

func canonicalTestLoreID(repositoryID string) string {
	return strings.ReplaceAll(repositoryID, "-", "")
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
