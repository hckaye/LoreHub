package platform

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const notificationRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestRevisionCommentEventsCreateRepositoryNotifications(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	owner := platformTestUser("revision-owner-" + suffix)
	viewer := platformTestUser("revision-viewer-" + suffix)
	organizationID := uuid.NewString()
	repositoryID := uuid.NewString()
	organizationSlug := "revision-notifications-" + suffix
	repositorySlug := "comments-" + suffix
	for _, user := range []User{owner, viewer} {
		mustIdentityExec(t, pool, `
			INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)
		`, user.ID, user.Username, user.DisplayName)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Revision notifications', 'private', $3)
	`, organizationID, organizationSlug, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, organizationID, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO repositories (
			id, organization_id, slug, display_name, visibility,
			lore_repository_id, lore_url, default_branch, created_by
		) VALUES ($1, $2, $3, 'Comments', 'private', $4, $5, 'main', $6)
	`, repositoryID, organizationID, repositorySlug,
		canonicalTestLoreID(repositoryID), "lore://"+repositorySlug, owner.ID)
	mustIdentityExec(t, pool, `INSERT INTO repository_counters (repository_id) VALUES ($1)`, repositoryID)
	mustIdentityExec(t, pool, `
		INSERT INTO repository_memberships (repository_id, user_id, role)
		VALUES ($1, $2, 'read')
	`, repositoryID, viewer.ID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, organizationID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id IN ($1, $2)`, owner.ID, viewer.ID)
	})

	insertRevisionCommentNotificationEvent(t, pool, repositoryID, "created", "Check the shader change")
	page, err := store.ListNotifications(ctx, viewer, false, 30)
	if err != nil {
		t.Fatalf("list revision comment notifications: %v", err)
	}
	created := findNotificationByTopic(page.Items, "revision_comment.created")
	if created == nil {
		t.Fatalf("revision comment notification not found: %+v", page.Items)
	}
	wantHref := "/" + organizationSlug + "/" + repositorySlug + "/commit?revision=" + notificationRevision
	if created.Title != "Revision "+notificationRevision[:12] ||
		created.Body != "Check the shader change" || created.Href != wantHref {
		t.Fatalf("revision comment notification = %+v", *created)
	}

	insertRevisionCommentNotificationEvent(t, pool, repositoryID, "deleted", "Removed comment")
	page, err = store.ListNotifications(ctx, viewer, false, 30)
	if err != nil {
		t.Fatalf("list deleted revision comment notification: %v", err)
	}
	deleted := findNotificationByTopic(page.Items, "revision_comment.deleted")
	if deleted == nil || deleted.Href != wantHref {
		t.Fatalf("deleted revision comment notification = %+v", deleted)
	}
}

func insertRevisionCommentNotificationEvent(
	t *testing.T,
	pool *pgxpool.Pool,
	repositoryID string,
	operation string,
	body string,
) {
	t.Helper()
	payload := `{"repositoryId":"` + repositoryID + `","comment":{"revision":"` +
		notificationRevision + `","body":"` + body + `"}}`
	mustIdentityExec(t, pool, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, $2, $3, $4)
	`, uuid.NewString(), "revision_comment."+operation, uuid.NewString(), payload)
}

func findNotificationByTopic(items []Notification, topic string) *Notification {
	for index := range items {
		if items[index].Topic == topic {
			return &items[index]
		}
	}
	return nil
}
