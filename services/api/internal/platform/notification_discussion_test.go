package platform

import "testing"

func TestDiscussionNotificationHref(t *testing.T) {
	number := int64(42)
	href := notificationHref(notificationScope{
		OrganizationSlug: "acme",
		RepositorySlug:   "game",
		DiscussionNumber: &number,
	})
	if href != "/acme/game/discussions/42" {
		t.Fatalf("href = %q", href)
	}
}
