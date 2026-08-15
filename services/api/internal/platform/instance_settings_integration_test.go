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

func TestResourceLimitOverridesBeatEnvironmentDefaults(t *testing.T) {
	pool, _ := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	actor := platformTestUser("limit-override-" + suffix)
	mustIdentityExec(t, pool, `INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)`,
		actor.ID, actor.Username, actor.DisplayName)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM instance_settings WHERE key IN ($1, $2, $3)`,
			maxOrganizationsPerUserSettingKey, maxRepositoriesPerOrganizationSettingKey,
			maxRepositorySizeBytesSettingKey)
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE created_by = $1`, actor.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, actor.ID)
	})
	store := NewStoreWithSettings(pool, StoreSettings{
		MaxOrganizationsPerUser:        2,
		MaxRepositoriesPerOrganization: 2,
		MaxRepositorySizeBytes:         10485760,
	})
	mustIdentityExec(t, pool, `DELETE FROM instance_settings WHERE key IN ($1, $2, $3)`,
		maxOrganizationsPerUserSettingKey, maxRepositoriesPerOrganizationSettingKey,
		maxRepositorySizeBytesSettingKey)

	if override, err := store.GetMaxOrganizationsPerUserOverride(ctx); err != nil || override != nil {
		t.Fatalf("missing organization override = %v, error=%v", override, err)
	}
	if size, err := store.EffectiveMaxRepositorySizeBytes(ctx); err != nil || size != 10485760 {
		t.Fatalf("default repository size = %d, error=%v", size, err)
	}

	negative := int64(-1)
	if err := store.SetMaxOrganizationsPerUserOverride(ctx, actor, &negative); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative organization override error = %v, want invalid input", err)
	}
	if err := store.SetMaxRepositoriesPerOrganizationOverride(ctx, actor, &negative); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative repository override error = %v, want invalid input", err)
	}
	if err := store.SetMaxRepositorySizeBytesOverride(ctx, actor, &negative); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("negative size override error = %v, want invalid input", err)
	}

	one := int64(1)
	if err := store.SetMaxOrganizationsPerUserOverride(ctx, actor, &one); err != nil {
		t.Fatalf("set organization override: %v", err)
	}
	if _, err := store.CreateOrganization(ctx, actor, CreateOrganizationInput{
		Slug: "limit-override-a-" + suffix, DisplayName: "First", Visibility: "public",
	}); err != nil {
		t.Fatalf("first organization: %v", err)
	}
	if _, err := store.CreateOrganization(ctx, actor, CreateOrganizationInput{
		Slug: "limit-override-b-" + suffix, DisplayName: "Second", Visibility: "public",
	}); !errors.Is(err, ErrOrganizationLimit) {
		t.Fatalf("second organization error = %v, want organization limit", err)
	}

	if err := store.SetMaxOrganizationsPerUserOverride(ctx, actor, nil); err != nil {
		t.Fatalf("clear organization override: %v", err)
	}
	if _, err := store.CreateOrganization(ctx, actor, CreateOrganizationInput{
		Slug: "limit-override-c-" + suffix, DisplayName: "Third", Visibility: "public",
	}); err != nil {
		t.Fatalf("cleared organization override: %v", err)
	}

	unlimited := int64(0)
	if err := store.SetMaxOrganizationsPerUserOverride(ctx, actor, &unlimited); err != nil {
		t.Fatalf("set unlimited organization override: %v", err)
	}
	if _, err := store.CreateOrganization(ctx, actor, CreateOrganizationInput{
		Slug: "limit-override-d-" + suffix, DisplayName: "Fourth", Visibility: "public",
	}); err != nil {
		t.Fatalf("unlimited organization override: %v", err)
	}

	size := int64(2048)
	if err := store.SetMaxRepositorySizeBytesOverride(ctx, actor, &size); err != nil {
		t.Fatalf("set size override: %v", err)
	}
	effective, err := store.EffectiveMaxRepositorySizeBytes(ctx)
	if err != nil || effective != 2048 {
		t.Fatalf("overridden repository size = %d, error=%v", effective, err)
	}
	if err := store.SetMaxRepositorySizeBytesOverride(ctx, actor, nil); err != nil {
		t.Fatalf("clear size override: %v", err)
	}
	effective, err = store.EffectiveMaxRepositorySizeBytes(ctx)
	if err != nil || effective != 10485760 {
		t.Fatalf("cleared repository size = %d, error=%v", effective, err)
	}
}

func TestRepositoryLimitOverrideTakesEffectWithoutRestart(t *testing.T) {
	pool, base := identityIntegrationStore(t)
	ctx := context.Background()
	actor, organization := resourceLimitOrganization(t, base, "repo-override")
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM instance_settings WHERE key = $1`,
			maxRepositoriesPerOrganizationSettingKey)
	})
	store := NewStoreWithSettings(pool, StoreSettings{MaxRepositoriesPerOrganization: 2})
	mustIdentityExec(t, pool, `DELETE FROM instance_settings WHERE key = $1`,
		maxRepositoriesPerOrganizationSettingKey)
	server := insertLoreServerFixture(t, pool, organization.ID, "repo-override-"+organization.Slug)
	firstID := strings.ReplaceAll(uuid.NewString(), "-", "")
	secondID := strings.ReplaceAll(uuid.NewString(), "-", "")

	if _, err := store.RegisterRepository(ctx, actor, organization.Slug, RegisterRepositoryInput{
		Slug: "imported-a", DisplayName: "Imported A", Visibility: "private",
		LoreRepositoryID: firstID, LoreURL: server.PublicURL + "/" + firstID,
		LoreServerID: server.ID, DefaultBranch: "main",
	}); err != nil {
		t.Fatalf("first imported repository: %v", err)
	}

	one := int64(1)
	if err := store.SetMaxRepositoriesPerOrganizationOverride(ctx, actor, &one); err != nil {
		t.Fatalf("set repository override: %v", err)
	}
	if _, err := store.RegisterRepository(ctx, actor, organization.Slug, RegisterRepositoryInput{
		Slug: "imported-b", DisplayName: "Imported B", Visibility: "private",
		LoreRepositoryID: secondID, LoreURL: server.PublicURL + "/" + secondID,
		LoreServerID: server.ID, DefaultBranch: "main",
	}); !errors.Is(err, ErrRepositoryLimit) {
		t.Fatalf("second imported repository error = %v, want repository limit", err)
	}

	if err := store.SetMaxRepositoriesPerOrganizationOverride(ctx, actor, nil); err != nil {
		t.Fatalf("clear repository override: %v", err)
	}
	if _, err := store.RegisterRepository(ctx, actor, organization.Slug, RegisterRepositoryInput{
		Slug: "imported-b", DisplayName: "Imported B", Visibility: "private",
		LoreRepositoryID: secondID, LoreURL: server.PublicURL + "/" + secondID,
		LoreServerID: server.ID, DefaultBranch: "main",
	}); err != nil {
		t.Fatalf("cleared repository override: %v", err)
	}
}
