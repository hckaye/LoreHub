package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/authz"
)

func TestRepositoryInvitationLifecycleIntegration(t *testing.T) {
	fixture := authorizationIntegrationFixture(t)
	store := NewStoreWithNotificationEmail(fixture.pool, true)
	ctx := context.Background()
	authorizationMustExec(t, fixture.pool, `
		UPDATE users SET email = $2, locale = 'ja' WHERE id = $1
	`, fixture.bob.ID, fixture.bob.Username+"@example.com")
	emailEnabled := true
	if _, err := store.UpdateNotificationPreferences(ctx, fixture.bob, UpdateNotificationPreferencesInput{
		EmailEnabled: &emailEnabled,
	}); err != nil {
		t.Fatal(err)
	}
	invitation, err := store.CreateRepositoryInvitation(
		ctx,
		fixture.manager,
		fixture.orgSlug,
		"a",
		CreateRepositoryInvitationInput{Username: fixture.bob.Username, Role: "read"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if invitation.Status != "pending" || invitation.InviteeUserID != fixture.bob.ID ||
		invitation.Role != "read" || !invitation.ExpiresAt.After(time.Now()) {
		t.Fatalf("unexpected repository invitation: %+v", invitation)
	}
	resource := "urc-" + fixture.loreA
	permissions, err := store.EffectivePermissions(ctx, fixture.bob.ID, resource)
	if err != nil || len(permissions.Permissions) != 0 {
		t.Fatalf("unaccepted invitation permissions=%v err=%v", permissions.Permissions, err)
	}
	if _, err := store.CreateRepositoryInvitation(
		ctx,
		fixture.manager,
		fixture.orgSlug,
		"a",
		CreateRepositoryInvitationInput{Username: fixture.bob.Username, Role: "write"},
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate repository invitation error=%v", err)
	}
	if projected, err := store.ProjectNotifications(ctx); err != nil || projected < 1 {
		t.Fatalf("project repository invitation notification=%d err=%v", projected, err)
	}
	notifications, err := store.ListNotifications(ctx, fixture.bob, false, 20)
	if err != nil || len(notifications.Items) != 1 ||
		!strings.Contains(notifications.Items[0].Title, "への招待") ||
		notifications.Items[0].Href != "/settings#repository-invitations" {
		t.Fatalf("repository invitation notification=%+v err=%v", notifications, err)
	}
	assertRepositoryInvitationEmailStatus(t, fixture, invitation.ID, "queued")
	if _, err := store.RespondRepositoryInvitation(ctx, fixture.alice, invitation.ID, true); !errors.Is(
		err,
		ErrNotFound,
	) {
		t.Fatalf("other user accepted repository invitation: %v", err)
	}
	accepted, err := store.RespondRepositoryInvitation(ctx, fixture.bob, invitation.ID, true)
	if err != nil || accepted.Status != "accepted" || accepted.RespondedAt == nil {
		t.Fatalf("accept repository invitation=%+v err=%v", accepted, err)
	}
	assertRepositoryInvitationEmailStatus(t, fixture, invitation.ID, "cancelled")
	permissions, err = store.EffectivePermissions(ctx, fixture.bob.ID, resource)
	if err != nil || !containsPermission(permissions.Permissions, authz.PermissionRead) ||
		containsPermission(permissions.Permissions, authz.PermissionWrite) {
		t.Fatalf("accepted invitation permissions=%v err=%v", permissions.Permissions, err)
	}
	updated, err := store.UpdateRepositoryCollaboratorRole(
		ctx,
		fixture.manager,
		fixture.orgSlug,
		"a",
		fixture.bob.Username,
		"write",
	)
	if err != nil || updated.Role != "write" || updated.Source != "direct" {
		t.Fatalf("update accepted collaborator=%+v err=%v", updated, err)
	}
	permissions, err = store.EffectivePermissions(ctx, fixture.bob.ID, resource)
	if err != nil || !containsPermission(permissions.Permissions, authz.PermissionWrite) {
		t.Fatalf("updated collaborator permissions=%v err=%v", permissions.Permissions, err)
	}
	if _, err := store.RespondRepositoryInvitation(ctx, fixture.bob, invitation.ID, false); !errors.Is(
		err,
		ErrConflict,
	) {
		t.Fatalf("repository invitation responded twice: %v", err)
	}
	page, err := store.ListRepositoryInvitationsForUser(ctx, fixture.bob, 1, 20)
	if err != nil || page.Total != 1 || len(page.Invitations) != 1 || page.Invitations[0].Status != "accepted" {
		t.Fatalf("account repository invitations=%+v err=%v", page, err)
	}
	assertRepositoryInvitationAudit(t, fixture, invitation.ID, []string{
		"repository.invitation.created",
		"repository.invitation.accepted",
	})
	revokedCollaborator, err := store.RevokeRepositoryCollaborator(
		ctx,
		fixture.manager,
		fixture.orgSlug,
		"a",
		fixture.bob.Username,
	)
	if err != nil || revokedCollaborator.Active {
		t.Fatalf("revoke accepted collaborator=%+v err=%v", revokedCollaborator, err)
	}
	permissions, err = store.EffectivePermissions(ctx, fixture.bob.ID, resource)
	if err != nil || len(permissions.Permissions) != 0 {
		t.Fatalf("revoked collaborator permissions=%v err=%v", permissions.Permissions, err)
	}
}

func TestRepositoryInvitationDeclineRevokeAndExpiryIntegration(t *testing.T) {
	fixture := authorizationIntegrationFixture(t)
	ctx := context.Background()
	invitee := User{
		ID:          uuid.NewString(),
		Username:    "invitee-" + uuid.NewString()[:8],
		DisplayName: "Invitee",
	}
	authorizationMustExec(t, fixture.pool, `
		INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)
	`, invitee.ID, invitee.Username, invitee.DisplayName)
	t.Cleanup(func() {
		_, _ = fixture.pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, invitee.ID)
	})
	if _, err := fixture.store.CreateRepositoryInvitation(
		ctx,
		fixture.alice,
		fixture.orgSlug,
		"a",
		CreateRepositoryInvitationInput{Username: invitee.Username, Role: "read"},
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("repository writer created invitation: %v", err)
	}
	declined, err := fixture.store.CreateRepositoryInvitation(
		ctx,
		fixture.manager,
		fixture.orgSlug,
		"a",
		CreateRepositoryInvitationInput{Username: invitee.Username, Role: "triage"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if response, err := fixture.store.RespondRepositoryInvitation(ctx, invitee, declined.ID, false); err != nil ||
		response.Status != "declined" {
		t.Fatalf("decline repository invitation=%+v err=%v", response, err)
	}
	assertNoDirectRepositoryMembership(t, fixture, invitee.ID)
	revoked, err := fixture.store.CreateRepositoryInvitation(
		ctx,
		fixture.manager,
		fixture.orgSlug,
		"a",
		CreateRepositoryInvitationInput{Username: invitee.Username, Role: "read"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.RevokeRepositoryInvitation(
		ctx,
		fixture.manager,
		fixture.orgSlug,
		"a",
		revoked.ID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.RespondRepositoryInvitation(ctx, invitee, revoked.ID, true); !errors.Is(
		err,
		ErrConflict,
	) {
		t.Fatalf("accepted revoked repository invitation: %v", err)
	}
	expired, err := fixture.store.CreateRepositoryInvitation(
		ctx,
		fixture.manager,
		fixture.orgSlug,
		"a",
		CreateRepositoryInvitationInput{Username: invitee.Username, Role: "read"},
	)
	if err != nil {
		t.Fatal(err)
	}
	authorizationMustExec(t, fixture.pool, `
		UPDATE repository_invitations
		SET created_at = now() - interval '8 days',
		    expires_at = now() - interval '1 day'
		WHERE id = $1
	`, expired.ID)
	replacement, err := fixture.store.CreateRepositoryInvitation(
		ctx,
		fixture.manager,
		fixture.orgSlug,
		"a",
		CreateRepositoryInvitationInput{Username: invitee.Username, Role: "maintain"},
	)
	if err != nil || replacement.Status != "pending" {
		t.Fatalf("replace expired repository invitation=%+v err=%v", replacement, err)
	}
	if _, err := fixture.store.RespondRepositoryInvitation(ctx, invitee, expired.ID, true); !errors.Is(
		err,
		ErrConflict,
	) {
		t.Fatalf("accepted expired repository invitation: %v", err)
	}
	if err := fixture.store.RevokeRepositoryInvitation(
		ctx,
		fixture.manager,
		fixture.orgSlug,
		"a",
		replacement.ID,
	); err != nil {
		t.Fatal(err)
	}
	suspended, err := fixture.store.CreateRepositoryInvitation(
		ctx,
		fixture.manager,
		fixture.orgSlug,
		"a",
		CreateRepositoryInvitationInput{Username: invitee.Username, Role: "read"},
	)
	if err != nil {
		t.Fatal(err)
	}
	authorizationMustExec(t, fixture.pool, `UPDATE users SET status = 'suspended' WHERE id = $1`, invitee.ID)
	if _, err := fixture.store.RespondRepositoryInvitation(ctx, invitee, suspended.ID, true); !errors.Is(
		err,
		ErrNotFound,
	) {
		t.Fatalf("suspended user accepted repository invitation: %v", err)
	}
	assertNoDirectRepositoryMembership(t, fixture, invitee.ID)
	page, err := fixture.store.ListRepositoryInvitations(ctx, fixture.manager, fixture.orgSlug, "a", 1, 20)
	if err != nil || page.Total != 5 || len(page.Invitations) != 5 {
		t.Fatalf("administrator repository invitations=%+v err=%v", page, err)
	}
	wantStatuses := map[string]bool{"declined": false, "revoked": false, "expired": false}
	for _, invitation := range page.Invitations {
		if _, ok := wantStatuses[invitation.Status]; ok {
			wantStatuses[invitation.Status] = true
		}
	}
	for status, found := range wantStatuses {
		if !found {
			t.Fatalf("repository invitation status %q missing from %+v", status, page.Invitations)
		}
	}
}

func assertRepositoryInvitationEmailStatus(
	t *testing.T,
	fixture authorizationFixture,
	invitationID string,
	want string,
) {
	t.Helper()
	var status string
	err := fixture.pool.QueryRow(context.Background(), `
		SELECT delivery.status
		FROM notification_email_deliveries delivery
		JOIN notifications notification ON notification.id = delivery.notification_id
		JOIN outbox_events event ON event.id = notification.source_event_id
		WHERE event.topic = 'repository.invitation.created'
		  AND event.payload ->> 'invitationId' = $1
	`, invitationID).Scan(&status)
	if err != nil {
		t.Fatal(err)
	}
	if status != want {
		t.Fatalf("repository invitation email status=%q want=%q", status, want)
	}
}

func assertNoDirectRepositoryMembership(t *testing.T, fixture authorizationFixture, userID string) {
	t.Helper()
	var count int
	if err := fixture.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM repository_memberships
		WHERE repository_id = $1 AND user_id = $2 AND active
	`, fixture.repositoryA, userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("direct repository membership count=%d want=0", count)
	}
}

func assertRepositoryInvitationAudit(
	t *testing.T,
	fixture authorizationFixture,
	invitationID string,
	want []string,
) {
	t.Helper()
	rows, err := fixture.pool.Query(context.Background(), `
		SELECT action FROM audit_events
		WHERE target_type = 'repository_invitation' AND target_id = $1
	`, invitationID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	found := make(map[string]bool)
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			t.Fatal(err)
		}
		found[action] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, action := range want {
		if !found[action] {
			t.Fatalf("repository invitation audit %q missing from %v", action, found)
		}
	}
}
