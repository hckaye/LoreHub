package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestEntitlementStoreGrantRevokeListAndAudit(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	actor := platformTestUser("entitlement-admin-" + suffix)
	organizationID := uuid.NewString()
	organizationSubject := EntitlementSubject{OrganizationID: organizationID}
	userSubject := EntitlementSubject{UserID: actor.ID}
	mustIdentityExec(t, pool, `
		INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)
	`, actor.ID, actor.Username, actor.DisplayName)
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Entitlement organization', 'private', $3)
	`, organizationID, "entitlement-"+suffix, actor.ID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM audit_events WHERE organization_id = $1`, organizationID)
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, organizationID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, actor.ID)
	})

	granted, err := store.Grant(ctx, actor, organizationSubject, EntitlementHostedRunners)
	if err != nil {
		t.Fatalf("grant organization entitlement: %v", err)
	}
	if granted.OrganizationID == nil || *granted.OrganizationID != organizationID || granted.UserID != nil ||
		granted.Feature != EntitlementHostedRunners || granted.GrantedBy == nil || *granted.GrantedBy != actor.ID ||
		granted.GrantSource != "admin" || granted.CreatedAt.IsZero() || granted.RevokedAt != nil {
		t.Fatalf("granted organization entitlement = %+v", granted)
	}
	if _, err := store.Grant(
		ctx, actor, organizationSubject, EntitlementHostedRunners,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate active grant error = %v, want conflict", err)
	}
	if entitled, err := store.HasEntitlement(
		ctx, organizationSubject, EntitlementHostedRunners,
	); err != nil || !entitled {
		t.Fatalf("active organization entitlement = %t, error=%v", entitled, err)
	}
	if err := store.Revoke(ctx, actor, organizationSubject, EntitlementHostedRunners); err != nil {
		t.Fatalf("revoke organization entitlement: %v", err)
	}
	if entitled, err := store.HasEntitlement(
		ctx, organizationSubject, EntitlementHostedRunners,
	); err != nil || entitled {
		t.Fatalf("revoked organization entitlement = %t, error=%v", entitled, err)
	}
	if err := store.Revoke(
		ctx, actor, organizationSubject, EntitlementHostedRunners,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second revocation error = %v, want not found", err)
	}
	if _, err := store.Grant(ctx, actor, organizationSubject, EntitlementHostedRunners); err != nil {
		t.Fatalf("grant organization entitlement after revocation: %v", err)
	}
	if _, err := store.Grant(ctx, actor, userSubject, EntitlementHostedLoreServer); err != nil {
		t.Fatalf("grant user entitlement: %v", err)
	}
	if _, err := store.Grant(
		ctx, actor, userSubject, EntitlementHostedLoreServer,
	); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate active user grant error = %v, want conflict", err)
	}

	entitlements, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list entitlements: %v", err)
	}
	organizationRows := 0
	activeOrganizationRows := 0
	userRows := 0
	for _, entitlement := range entitlements {
		if entitlement.OrganizationID != nil && *entitlement.OrganizationID == organizationID &&
			entitlement.Feature == EntitlementHostedRunners {
			organizationRows++
			if entitlement.RevokedAt == nil {
				activeOrganizationRows++
			}
		}
		if entitlement.UserID != nil && *entitlement.UserID == actor.ID &&
			entitlement.Feature == EntitlementHostedLoreServer {
			userRows++
		}
	}
	if organizationRows != 2 || activeOrganizationRows != 1 || userRows != 1 {
		t.Fatalf(
			"listed entitlement history: organization=%d active=%d user=%d",
			organizationRows,
			activeOrganizationRows,
			userRows,
		)
	}

	var auditCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM audit_events
		WHERE organization_id = $1
		  AND action IN ('entitlement.grant', 'entitlement.revoke')
		  AND target_type = 'entitlement'
		  AND target_id = 'hosted_runners'
	`, organizationID).Scan(&auditCount); err != nil {
		t.Fatalf("count entitlement audit events: %v", err)
	}
	if auditCount != 3 {
		t.Fatalf("organization entitlement audit count = %d, want 3", auditCount)
	}
}

func TestEntitlementStoreRejectsInvalidSubjectsAndFeatures(t *testing.T) {
	store := &Store{}
	validID := uuid.NewString()
	invalidSubjects := []EntitlementSubject{
		{},
		{OrganizationID: validID, UserID: validID},
		{OrganizationID: "not-a-uuid"},
	}
	for _, subject := range invalidSubjects {
		if _, err := store.HasEntitlement(
			t.Context(), subject, EntitlementHostedRunners,
		); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("subject %+v error = %v, want invalid input", subject, err)
		}
	}
	if _, err := store.HasEntitlement(
		t.Context(), EntitlementSubject{OrganizationID: validID}, "unknown",
	); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unknown feature error = %v, want invalid input", err)
	}
}
