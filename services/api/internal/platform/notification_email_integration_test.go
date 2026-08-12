package platform

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNotificationEmailDeliveryLifecycleIntegration(t *testing.T) {
	pool, _ := identityIntegrationStore(t)
	store := NewStoreWithNotificationEmail(pool, true)
	ctx := context.Background()
	suffix := uuid.NewString()[:8]
	owner := platformTestUser("email-owner-" + suffix)
	organizationID := uuid.NewString()
	mustIdentityExec(t, pool, `
		INSERT INTO users (id, username, display_name, email, locale)
		VALUES ($1, $2, $3, $4, 'ja')
	`, owner.ID, owner.Username, owner.DisplayName, "alice-"+suffix+"@example.com")
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Email organization', 'public', $3)
	`, organizationID, "email-"+uuid.NewString()[:8], owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, organizationID, owner.ID)
	inAppEnabled := false
	emailEnabled := true
	preferences, err := store.UpdateNotificationPreferences(ctx, owner, UpdateNotificationPreferencesInput{
		InAppEnabled: &inAppEnabled, EmailEnabled: &emailEnabled,
	})
	if err != nil || !preferences.EmailAvailable || !preferences.EmailEnabled {
		t.Fatalf("enable notification email: preferences=%+v err=%v", preferences, err)
	}
	eventID := insertNotificationEmailEvent(t, pool, organizationID, "created")
	projected, err := store.ProjectNotifications(ctx)
	if err != nil || projected < 1 {
		t.Fatalf("project notification email: projected=%d err=%v", projected, err)
	}
	page, err := store.ListNotifications(ctx, owner, false, 10)
	if err != nil || page.Total != 0 || len(page.Items) != 0 {
		t.Fatalf("email-only notification appeared in inbox: page=%+v err=%v", page, err)
	}
	assertNotificationEmailQueued(t, pool, eventID, owner.ID)
	workerID := uuid.NewString()
	claim, err := store.ClaimNotificationEmail(ctx, workerID, 30*time.Second)
	if err != nil || claim == nil {
		t.Fatalf("claim notification email: claim=%+v err=%v", claim, err)
	}
	if claim.Recipient != "alice-"+suffix+"@example.com" || claim.Locale != "ja" || claim.Attempt != 1 {
		t.Fatalf("unexpected notification email claim: %+v", claim)
	}
	second, err := store.ClaimNotificationEmail(ctx, uuid.NewString(), 30*time.Second)
	if err != nil || second != nil {
		t.Fatalf("active lease was claimed twice: claim=%+v err=%v", second, err)
	}
	if err := store.CompleteNotificationEmail(
		ctx, workerID, *claim, 3, false, time.Now().Add(-time.Second), "temporary failure",
	); err != nil {
		t.Fatal(err)
	}
	claim, err = store.ClaimNotificationEmail(ctx, workerID, 30*time.Second)
	if err != nil || claim == nil || claim.Attempt != 2 {
		t.Fatalf("reclaim failed notification email: claim=%+v err=%v", claim, err)
	}
	if err := store.CompleteNotificationEmail(
		ctx, workerID, *claim, 3, true, time.Now(), "",
	); err != nil {
		t.Fatal(err)
	}
	var status string
	var sentAt *time.Time
	if err := pool.QueryRow(ctx, `
		SELECT status, sent_at FROM notification_email_deliveries WHERE id = $1
	`, claim.DeliveryID).Scan(&status, &sentAt); err != nil {
		t.Fatal(err)
	}
	if status != "sent" || sentAt == nil {
		t.Fatalf("completed email status=%q sentAt=%v", status, sentAt)
	}
}

func TestNotificationEmailExpiredLeaseAndExhaustionIntegration(t *testing.T) {
	pool, _ := identityIntegrationStore(t)
	store := NewStoreWithNotificationEmail(pool, true)
	ctx := context.Background()
	suffix := uuid.NewString()[:8]
	owner := platformTestUser("email-expired-" + suffix)
	organizationID := uuid.NewString()
	mustIdentityExec(t, pool, `
		INSERT INTO users (id, username, display_name, email)
		VALUES ($1, $2, $3, $4)
	`, owner.ID, owner.Username, owner.DisplayName, "expired-"+suffix+"@example.com")
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Expired lease organization', 'public', $3)
	`, organizationID, "email-expired-"+uuid.NewString()[:8], owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, organizationID, owner.ID)
	emailEnabled := true
	if _, err := store.UpdateNotificationPreferences(ctx, owner, UpdateNotificationPreferencesInput{
		EmailEnabled: &emailEnabled,
	}); err != nil {
		t.Fatal(err)
	}
	eventID := insertNotificationEmailEvent(t, pool, organizationID, "expired")
	if _, err := store.ProjectNotifications(ctx); err != nil {
		t.Fatal(err)
	}
	firstWorkerID := uuid.NewString()
	firstClaim, err := store.ClaimNotificationEmail(ctx, firstWorkerID, 30*time.Second)
	if err != nil || firstClaim == nil || firstClaim.Attempt != 1 {
		t.Fatalf("first notification email claim: claim=%+v err=%v", firstClaim, err)
	}
	mustIdentityExec(t, pool, `
		UPDATE notification_email_deliveries
		SET lease_expires_at = now() - interval '1 second'
		WHERE id = $1
	`, firstClaim.DeliveryID)
	secondWorkerID := uuid.NewString()
	secondClaim, err := store.ClaimNotificationEmail(ctx, secondWorkerID, 30*time.Second)
	if err != nil || secondClaim == nil || secondClaim.Attempt != 2 {
		t.Fatalf("expired notification email claim: claim=%+v err=%v", secondClaim, err)
	}
	if err := store.CompleteNotificationEmail(
		ctx, firstWorkerID, *firstClaim, 2, true, time.Now(), "",
	); err == nil {
		t.Fatal("previous notification email worker completed a replaced lease")
	}
	if err := store.CompleteNotificationEmail(
		ctx, secondWorkerID, *secondClaim, 2, false, time.Now(), "permanent failure",
	); err != nil {
		t.Fatal(err)
	}
	assertNotificationEmailStatus(t, pool, eventID, owner.ID, "exhausted")
	var attemptCount int
	var leaseOwner *string
	var leaseExpiresAt *time.Time
	if err := pool.QueryRow(ctx, `
		SELECT attempt_count, lease_owner::text, lease_expires_at
		FROM notification_email_deliveries
		WHERE id = $1
	`, secondClaim.DeliveryID).Scan(&attemptCount, &leaseOwner, &leaseExpiresAt); err != nil {
		t.Fatal(err)
	}
	if attemptCount != 2 || leaseOwner != nil || leaseExpiresAt != nil {
		t.Fatalf(
			"exhausted email attemptCount=%d leaseOwner=%v leaseExpiresAt=%v",
			attemptCount,
			leaseOwner,
			leaseExpiresAt,
		)
	}
}

func TestNotificationEmailRechecksPreferencesAndAccessIntegration(t *testing.T) {
	pool, _ := identityIntegrationStore(t)
	store := NewStoreWithNotificationEmail(pool, true)
	ctx := context.Background()
	suffix := uuid.NewString()[:8]
	owner := platformTestUser("email-recheck-" + suffix)
	organizationID := uuid.NewString()
	mustIdentityExec(t, pool, `
		INSERT INTO users (id, username, display_name, email)
		VALUES ($1, $2, $3, $4)
	`, owner.ID, owner.Username, owner.DisplayName, "recheck-"+suffix+"@example.com")
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Recheck organization', 'public', $3)
	`, organizationID, "recheck-"+uuid.NewString()[:8], owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO organization_memberships (organization_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, organizationID, owner.ID)
	emailEnabled := true
	if _, err := store.UpdateNotificationPreferences(ctx, owner, UpdateNotificationPreferencesInput{
		EmailEnabled: &emailEnabled,
	}); err != nil {
		t.Fatal(err)
	}
	disabledEventID := insertNotificationEmailEvent(t, pool, organizationID, "disabled")
	if _, err := store.ProjectNotifications(ctx); err != nil {
		t.Fatal(err)
	}
	emailEnabled = false
	if _, err := store.UpdateNotificationPreferences(ctx, owner, UpdateNotificationPreferencesInput{
		EmailEnabled: &emailEnabled,
	}); err != nil {
		t.Fatal(err)
	}
	assertNotificationEmailStatus(t, pool, disabledEventID, owner.ID, "cancelled")
	emailEnabled = true
	if _, err := store.UpdateNotificationPreferences(ctx, owner, UpdateNotificationPreferencesInput{
		EmailEnabled: &emailEnabled,
	}); err != nil {
		t.Fatal(err)
	}
	revokedEventID := insertNotificationEmailEvent(t, pool, organizationID, "revoked")
	if _, err := store.ProjectNotifications(ctx); err != nil {
		t.Fatal(err)
	}
	mustIdentityExec(t, pool, `
		UPDATE organization_memberships SET active = false
		WHERE organization_id = $1 AND user_id = $2
	`, organizationID, owner.ID)
	claim, err := store.ClaimNotificationEmail(ctx, uuid.NewString(), 30*time.Second)
	if err != nil || claim != nil {
		t.Fatalf("revoked notification email was claimable: claim=%+v err=%v", claim, err)
	}
	assertNotificationEmailStatus(t, pool, revokedEventID, owner.ID, "cancelled")
}

func insertNotificationEmailEvent(
	t *testing.T,
	pool *pgxpool.Pool,
	organizationID string,
	label string,
) string {
	t.Helper()
	eventID := uuid.NewString()
	mustIdentityExec(t, pool, `
		INSERT INTO outbox_events (id, topic, event_key, payload)
		VALUES ($1, 'organization.updated', $2, $3::jsonb)
	`, eventID, organizationID+":"+label+":"+uuid.NewString(), `{"title":"Organization updated"}`)
	return eventID
}

func assertNotificationEmailQueued(t *testing.T, pool *pgxpool.Pool, eventID string, userID string) {
	t.Helper()
	assertNotificationEmailStatus(t, pool, eventID, userID, "queued")
}

func assertNotificationEmailStatus(
	t *testing.T,
	pool *pgxpool.Pool,
	eventID string,
	userID string,
	want string,
) {
	t.Helper()
	var status string
	err := pool.QueryRow(context.Background(), `
		SELECT delivery.status
		FROM notification_email_deliveries delivery
		JOIN notifications notification ON notification.id = delivery.notification_id
		WHERE notification.source_event_id = $1 AND notification.recipient_id = $2
	`, eventID, userID).Scan(&status)
	if err != nil {
		t.Fatal(err)
	}
	if status != want {
		t.Fatalf("notification email status=%q, want %q", status, want)
	}
}
