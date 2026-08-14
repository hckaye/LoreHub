package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestInstanceSettingsAndHostedLoreServerEnforcementIntegration(t *testing.T) {
	pool, store := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	owner := platformTestUser("instance-settings-owner-" + suffix)
	organizationID := uuid.NewString()
	organizationSlug := "instance-settings-" + suffix

	mustIdentityExec(t, pool, `
		DELETE FROM instance_settings WHERE key = $1
	`, hostedLoreServerEnabledSettingKey)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM instance_settings WHERE key = $1`, hostedLoreServerEnabledSettingKey)
	})

	override, err := store.GetHostedLoreServerOverride(ctx)
	if err != nil {
		t.Fatalf("read missing hosted Lore server override: %v", err)
	}
	if override != nil {
		t.Fatalf("missing hosted Lore server override = %v, want nil", *override)
	}
	if enabled, err := store.HostedLoreServerEnabled(ctx, false); err != nil || enabled {
		t.Fatalf("disabled default result = %t, error=%v", enabled, err)
	}
	if enabled, err := store.HostedLoreServerEnabled(ctx, true); err != nil || !enabled {
		t.Fatalf("enabled default result = %t, error=%v", enabled, err)
	}

	disabled := false
	if err := store.SetHostedLoreServerOverride(ctx, owner, &disabled); err != nil {
		t.Fatalf("set hosted Lore server override: %v", err)
	}
	override, err = store.GetHostedLoreServerOverride(ctx)
	if err != nil || override == nil || *override {
		t.Fatalf("stored disabled override = %v, error=%v", override, err)
	}
	var updatedBy string
	if err := pool.QueryRow(ctx, `
		SELECT updated_by FROM instance_settings WHERE key = $1
	`, hostedLoreServerEnabledSettingKey).Scan(&updatedBy); err != nil {
		t.Fatalf("read setting actor: %v", err)
	}
	if updatedBy != owner.ID {
		t.Fatalf("setting updated_by = %q, want %q", updatedBy, owner.ID)
	}
	if enabled, err := store.HostedLoreServerEnabled(ctx, true); err != nil || enabled {
		t.Fatalf("disabled override result = %t, error=%v", enabled, err)
	}

	mustIdentityExec(t, pool, `
		INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)
	`, owner.ID, owner.Username, owner.DisplayName)
	mustIdentityExec(t, pool, `
		INSERT INTO organizations (id, slug, display_name, visibility, created_by)
		VALUES ($1, $2, 'Instance settings organization', 'private', $3)
	`, organizationID, organizationSlug, owner.ID)
	mustIdentityExec(t, pool, `
		INSERT INTO entitlements (organization_id, feature, granted_by, grant_source)
		VALUES ($1, $2, $3, 'admin')
	`, organizationID, EntitlementHostedLoreServer, owner.ID)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, organizationID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, owner.ID)
	})

	instance, err := store.EnsureInstanceLoreServer(ctx, "lores://instance-settings-"+suffix+".example:41337")
	if err != nil {
		t.Fatalf("ensure instance Lore server: %v", err)
	}
	_, err = store.ResolveServerForNewRepository(ctx, organizationID, "")
	var selectionError *LoreServerSelectionError
	if !errors.As(err, &selectionError) || selectionError.Reason != LoreServerSelectionHostedDisabled {
		t.Fatalf("disabled hosted Lore server resolution = %v, want %q", err,
			LoreServerSelectionHostedDisabled)
	}
	if err == nil || err.Error() != "the hosted Lore server is disabled on this instance" {
		t.Fatalf("disabled hosted Lore server message = %v", err)
	}

	enabled := true
	if err := store.SetHostedLoreServerOverride(ctx, owner, &enabled); err != nil {
		t.Fatalf("enable hosted Lore server override: %v", err)
	}
	selected, err := store.ResolveServerForNewRepository(ctx, organizationID, "")
	if err != nil || selected.ID != instance.ID {
		t.Fatalf("enabled hosted Lore server resolution = %+v, error=%v", selected, err)
	}

	if err := store.SetHostedLoreServerOverride(ctx, owner, nil); err != nil {
		t.Fatalf("clear hosted Lore server override: %v", err)
	}
	override, err = store.GetHostedLoreServerOverride(ctx)
	if err != nil || override != nil {
		t.Fatalf("cleared hosted Lore server override = %v, error=%v", override, err)
	}
}
