package platform

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestDiscussionEventsCreateRepositoryNotifications(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	owner := platformTestUser("discussion-owner-" + suffix)
	viewer := platformTestUser("discussion-viewer-" + suffix)
	organizationID := uuid.NewString()
	repositoryID := uuid.NewString()
	discussionID := uuid.NewString()
	eventID := uuid.NewString()
	for _, user := range []User{owner, viewer} {
		mustIdentityExec(t, pool, `
			INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)
		`, user.ID, user.Username, user.DisplayName)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Discussion notifications', 'public', $3)
	`, organizationID, "discussion-notifications-"+suffix, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, organizationID, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, $3, 'Discussion repository', 'public', $4, $5, 'main', $6)
	`, repositoryID, organizationID, "discussions-"+suffix,
		canonicalTestLoreID(repositoryID), "lore://discussions-"+suffix, owner.ID)
	mustIdentityExec(t, pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, repositoryID)
	mustIdentityExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role) VALUES ($1, $2, 'read')
	`, repositoryID, viewer.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO discussions (id, repository_id, category_id, number, author_id, title, body)
		SELECT $1, $2, category.id, 7, $3, 'Renderer performance', 'How can this be improved?'
		FROM discussion_categories category
		WHERE category.repository_id = $2 AND category.slug = 'questions'
	`, discussionID, repositoryID, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, 'discussion.created', $2, $3)
	`, eventID, discussionID+":"+uuid.NewString(), `{"title":"Renderer performance"}`)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM outbox_events WHERE id = $1`, eventID)
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, organizationID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id IN ($1, $2)`, owner.ID, viewer.ID)
	})

	page, err := store.ListNotifications(ctx, viewer, false, 30)
	if err != nil {
		t.Fatalf("list discussion notifications: %v", err)
	}
	var found *Notification
	for index := range page.Items {
		if page.Items[index].Topic == "discussion.created" {
			found = &page.Items[index]
			break
		}
	}
	if found == nil {
		t.Fatalf("discussion notification not found: %+v", page.Items)
	}
	if found.Title != "Renderer performance" ||
		found.Href != "/discussion-notifications-"+suffix+"/discussions-"+suffix+"/discussions/7" {
		t.Fatalf("discussion notification = %+v", *found)
	}
}
