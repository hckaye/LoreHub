package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestIdentityCanonicalLifecycleBoundary(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	owner := platformTestUser("lifecycle-owner-" + suffix)
	maintainer := platformTestUser("lifecycle-maintainer-" + suffix)
	direct := platformTestUser("lifecycle-direct-" + suffix)
	teamUser := platformTestUser("lifecycle-team-" + suffix)
	suspendedOwner := platformTestUser("lifecycle-suspended-" + suffix)
	orgID := uuid.NewString()
	teamID := uuid.NewString()
	orgSlug := "lifecycle-org-" + suffix
	repositoryIDs := map[string]string{
		"public":   uuid.NewString(),
		"internal": uuid.NewString(),
		"private":  uuid.NewString(),
		"inactive": uuid.NewString(),
		"archived": uuid.NewString(),
	}
	users := []User{owner, maintainer, direct, teamUser, suspendedOwner}
	for _, user := range users {
		mustIdentityExec(t, pool, `INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)`,
			user.ID, user.Username, user.DisplayName)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Lifecycle organization', 'public', $3)
	`, orgID, orgSlug, owner.ID)
	for _, membership := range []struct {
		user string
		role string
	}{
		{owner.ID, "owner"}, {maintainer.ID, "maintainer"}, {teamUser.ID, "member"},
		{suspendedOwner.ID, "owner"},
	} {
		mustIdentityExec(t, pool, `
			INSERT INTO organization_memberships (organization_id, user_id, role)
			VALUES ($1, $2, $3)
		`, orgID, membership.user, membership.role)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO teams (id, organization_id, slug, display_name, created_by)
		VALUES ($1, $2, $3, 'Lifecycle team', $4)
	`, teamID, orgID, "lifecycle-team-"+suffix, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO team_memberships (team_id, user_id, role) VALUES ($1, $2, 'member')
	`, teamID, teamUser.ID)
	for visibility, repositoryID := range repositoryIDs {
		archivedAt := any(nil)
		lifecycle := "active"
		storedVisibility := visibility
		if visibility == "archived" {
			archivedAt = time.Now().UTC()
			storedVisibility = "public"
		} else if visibility == "inactive" {
			storedVisibility = "private"
		}
		repositorySlug := "lifecycle-" + visibility + "-" + suffix
		mustIdentityExec(t, pool, `
			INSERT INTO repositories (
				id, organization_id, slug, display_name, visibility, lifecycle_state, archived_at,
				lore_repository_id, lore_url, default_branch, created_by
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'main', $10)
		`, repositoryID, orgID, repositorySlug, "Lifecycle "+visibility, storedVisibility, lifecycle,
			archivedAt, canonicalTestLoreID(repositoryID), "lore://"+repositorySlug, owner.ID)
		mustIdentityExec(t, pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, repositoryID)
	}
	mustIdentityExec(t, pool, `
		UPDATE repositories SET lifecycle_state = 'pending' WHERE id = $1
	`, repositoryIDs["inactive"])
	mustIdentityExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role) VALUES ($1, $2, 'read')
	`, repositoryIDs["private"], direct.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO team_repository_roles (team_id, repository_id, role, created_by, active)
		VALUES ($1, $2, 'read', $3, true)
	`, teamID, repositoryIDs["private"], owner.ID)
	mustIdentityExec(t, pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, suspendedOwner.ID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, orgID)
		for _, user := range users {
			_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, user.ID)
		}
	})

	assertSearch := func(label string, viewer *User, expected ...string) {
		t.Helper()
		results, err := store.Search(ctx, viewer, "Lifecycle", "repositories", 20)
		if err != nil {
			t.Fatalf("%s search: %v", label, err)
		}
		seen := make(map[string]bool, len(results.Repositories))
		for _, repository := range results.Repositories {
			seen[repository.Slug] = true
		}
		if len(seen) != len(expected) {
			t.Fatalf("%s returned %d repositories, want %d: %+v", label, len(seen), len(expected), results.Repositories)
		}
		for _, visibility := range expected {
			if !seen["lifecycle-"+visibility+"-"+suffix] {
				t.Fatalf("%s omitted %s: %+v", label, visibility, results.Repositories)
			}
		}
	}
	assertSearch("anonymous", nil, "public")
	assertSearch("maintainer", &maintainer, "public", "internal")
	assertSearch("direct grant", &direct, "public", "private")
	assertSearch("team grant", &teamUser, "public", "private", "internal")
	assertSearch("owner", &owner, "public", "internal", "private")
	assertSearch("suspended owner", &suspendedOwner)

	mustIdentityExec(t, pool, `UPDATE team_repository_roles SET active = false WHERE team_id = $1`, teamID)
	assertSearch("inactive team grant", &teamUser, "public", "internal")
	mustIdentityExec(t, pool, `
		UPDATE organization_memberships SET active = false WHERE organization_id = $1 AND user_id = $2
	`, orgID, maintainer.ID)
	assertSearch("inactive organization membership", &maintainer, "public")
	mustIdentityExec(t, pool, `UPDATE repository_memberships SET active = false WHERE repository_id = $1 AND user_id = $2`,
		repositoryIDs["private"], direct.ID)
	assertSearch("inactive direct grant", &direct, "public")
	mustIdentityExec(t, pool, `UPDATE organizations SET active = false WHERE id = $1`, orgID)
	assertSearch("inactive organization", &owner)
}

func TestIdentityNotificationCurrentAuthorizationAndUserScope(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	owner := platformTestUser("notification-owner-" + suffix)
	suspendedOwner := platformTestUser("notification-suspended-owner-" + suffix)
	direct := platformTestUser("notification-direct-" + suffix)
	teamUser := platformTestUser("notification-team-" + suffix)
	member := platformTestUser("notification-member-" + suffix)
	orgID := uuid.NewString()
	teamID := uuid.NewString()
	repositoryID := uuid.NewString()
	orgSlug := "notification-access-org-" + suffix
	repositorySlug := "notification-access-repository-" + suffix
	users := []User{owner, suspendedOwner, direct, teamUser, member}
	for _, user := range users {
		mustIdentityExec(t, pool, `INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)`,
			user.ID, user.Username, user.DisplayName)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Notification access organization', 'public', $3)
	`, orgID, orgSlug, owner.ID)
	for _, membership := range []struct {
		user string
		role string
	}{
		{owner.ID, "owner"}, {suspendedOwner.ID, "owner"}, {direct.ID, "member"},
		{teamUser.ID, "member"}, {member.ID, "member"},
	} {
		mustIdentityExec(t, pool, `
			INSERT INTO organization_memberships (organization_id, user_id, role) VALUES ($1, $2, $3)
		`, orgID, membership.user, membership.role)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO teams (id, organization_id, slug, display_name, created_by)
		VALUES ($1, $2, $3, 'Notification access team', $4)
	`, teamID, orgID, "notification-access-team-"+suffix, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO team_memberships (team_id, user_id, role) VALUES ($1, $2, 'member')
	`, teamID, teamUser.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, $3, 'Notification access repository', 'private', $4, $5, 'main', $6)
	`, repositoryID, orgID, repositorySlug, canonicalTestLoreID(repositoryID), "lore://"+repositorySlug, owner.ID)
	mustIdentityExec(t, pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, repositoryID)
	mustIdentityExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role) VALUES ($1, $2, 'read')
	`, repositoryID, direct.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO team_repository_roles (team_id, repository_id, role, created_by, active)
		VALUES ($1, $2, 'read', $3, true)
	`, teamID, repositoryID, owner.ID)
	issueID := uuid.NewString()
	eventID := uuid.NewString()
	mustIdentityExec(t, pool, `
		INSERT INTO issues (id, repository_id, number, title, author_id)
		VALUES ($1, $2, 1, 'Private access event', $3)
	`, issueID, repositoryID, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, 'issue.created', $2, $3)
	`, eventID, issueID, `{"title":"Private access event `+suffix+`"}`)
	userEventID := uuid.NewString()
	userNotificationID := uuid.NewString()
	mustIdentityExec(t, pool, `
		INSERT INTO outbox_events (id, topic, event_key, payload) VALUES ($1, 'user.notice', $2, '{}')
	`, userEventID, userNotificationID)
	mustIdentityExec(t, pool, `
		INSERT INTO notifications (
			id, recipient_id, source_event_id, topic, title, body, href, created_at
		) VALUES ($1, $2, $3, 'user.notice', 'Unscoped notice', 'private body', '/notifications', now())
	`, userNotificationID, member.ID, userEventID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM outbox_events WHERE id IN ($1, $2)`, eventID, userEventID)
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, orgID)
		for _, user := range users {
			_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, user.ID)
		}
	})
	if _, err := store.ListNotifications(ctx, owner, false, 20); err != nil {
		t.Fatalf("project private notification: %v", err)
	}
	notificationID := func(recipient string) string {
		t.Helper()
		var id string
		if err := pool.QueryRow(ctx, `
			SELECT id FROM notifications WHERE recipient_id = $1 AND source_event_id = $2
		`, recipient, eventID).Scan(&id); err != nil {
			t.Fatalf("find materialized notification for %s: %v", recipient, err)
		}
		return id
	}
	assertRevoked := func(label string, user User, revoke func()) {
		t.Helper()
		id := notificationID(user.ID)
		revoke()
		if err := store.MarkNotificationRead(ctx, user, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s mark-one error = %v, want not found", label, err)
		}
		if err := store.MarkAllNotificationsRead(ctx, user); err != nil {
			t.Fatalf("%s mark-all: %v", label, err)
		}
		page, err := store.ListNotifications(ctx, user, false, 20)
		if err != nil {
			t.Fatalf("%s list: %v", label, err)
		}
		for _, item := range page.Items {
			if strings.Contains(item.Title, "Private access event") || strings.Contains(item.Body, "private body") {
				t.Fatalf("%s disclosed revoked notification: %+v", label, item)
			}
		}
		unread, err := store.UnreadNotificationCount(ctx, user)
		if err != nil {
			t.Fatalf("%s unread count: %v", label, err)
		}
		if unread != 0 {
			t.Fatalf("%s unread count = %d, want 0", label, unread)
		}
	}
	assertRevoked("direct grant", direct, func() {
		mustIdentityExec(t, pool, `
			UPDATE repository_memberships SET active = false WHERE repository_id = $1 AND user_id = $2
		`, repositoryID, direct.ID)
	})
	assertRevoked("team grant", teamUser, func() {
		mustIdentityExec(t, pool, `UPDATE team_repository_roles SET active = false WHERE team_id = $1`, teamID)
	})
	assertRevoked("suspended owner", suspendedOwner, func() {
		mustIdentityExec(t, pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, suspendedOwner.ID)
	})
	assertRevoked("organization owner", owner, func() {
		mustIdentityExec(t, pool, `
			UPDATE organization_memberships SET active = false WHERE organization_id = $1 AND user_id = $2
		`, orgID, owner.ID)
	})
	page, err := store.ListNotifications(ctx, member, false, 20)
	if err != nil {
		t.Fatalf("list unscoped user notification: %v", err)
	}
	var found bool
	for _, item := range page.Items {
		if item.ID == userNotificationID {
			found = true
		}
	}
	if !found {
		t.Fatalf("active user-scoped notification was not listed: %+v", page.Items)
	}
	if err := store.MarkNotificationRead(ctx, member, userNotificationID); err != nil {
		t.Fatalf("mark user-scoped notification: %v", err)
	}
	if unread, err := store.UnreadNotificationCount(ctx, member); err != nil || unread != 0 {
		t.Fatalf("user-scoped unread count = %d, err=%v; want 0", unread, err)
	}
}
