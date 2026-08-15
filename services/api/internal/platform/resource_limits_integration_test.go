package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCreateOrganizationEnforcesConfiguredUserLimit(t *testing.T) {
	pool, _ := identityIntegrationStore(t)
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	actor := platformTestUser("org-limit-" + suffix)
	mustIdentityExec(t, pool, `INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)`,
		actor.ID, actor.Username, actor.DisplayName)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE created_by = $1`, actor.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, actor.ID)
	})
	store := NewStoreWithSettings(pool, StoreSettings{MaxOrganizationsPerUser: 1})

	first, err := store.CreateOrganization(ctx, actor, CreateOrganizationInput{
		Slug: "org-limit-a-" + suffix, DisplayName: "First", Visibility: "public",
	})
	if err != nil {
		t.Fatalf("first organization: %v", err)
	}
	if _, err := store.CreateOrganization(ctx, actor, CreateOrganizationInput{
		Slug: "org-limit-b-" + suffix, DisplayName: "Second", Visibility: "public",
	}); !errors.Is(err, ErrOrganizationLimit) {
		t.Fatalf("second organization error = %v, want organization limit", err)
	}

	unlimited := NewStore(pool)
	if _, err := unlimited.CreateOrganization(ctx, actor, CreateOrganizationInput{
		Slug: "org-limit-c-" + suffix, DisplayName: "Unlimited", Visibility: "public",
	}); err != nil {
		t.Fatalf("unlimited organization: %v", err)
	}
	_, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, first.ID)
}

func TestBeginRepositoryProvisioningEnforcesConfiguredOrganizationLimit(t *testing.T) {
	pool, base := identityIntegrationStore(t)
	ctx := context.Background()
	actor, organization := resourceLimitOrganization(t, base, "provision-limit")
	store := NewStoreWithSettings(pool, StoreSettings{
		HostedLoreServerDefaultEnabled: true,
		DefaultEntitlements:            []string{EntitlementHostedLoreServer},
		MaxRepositoriesPerOrganization: 1,
	})
	if _, err := store.EnsureInstanceLoreServer(ctx, "lores://provision-limit.example:41337"); err != nil {
		t.Fatalf("ensure instance Lore server: %v", err)
	}
	mustIdentityExec(t, pool, `
		INSERT INTO entitlements (organization_id, feature, granted_by, grant_source)
		VALUES ($1, $2, $3, 'admin')
	`, organization.ID, EntitlementHostedLoreServer, actor.ID)

	first, err := store.BeginRepositoryProvisioning(ctx, actor, organization.Slug, ProvisionRepositoryInput{
		Slug: "first", DisplayName: "First", Visibility: "private", DefaultBranch: "main",
	}, "")
	if err != nil {
		t.Fatalf("first repository: %v", err)
	}
	if _, err := store.BeginRepositoryProvisioning(ctx, actor, organization.Slug, ProvisionRepositoryInput{
		Slug: "second", DisplayName: "Second", Visibility: "private", DefaultBranch: "main",
	}, ""); !errors.Is(err, ErrRepositoryLimit) {
		t.Fatalf("second repository error = %v, want repository limit", err)
	}

	retried, err := store.BeginRepositoryProvisioning(ctx, actor, organization.Slug, ProvisionRepositoryInput{
		Slug: "first", DisplayName: "First", Visibility: "private", DefaultBranch: "main",
	}, "")
	if err != nil || retried.ID != first.ID {
		t.Fatalf("retry existing pending repository = %+v, err %v", retried, err)
	}
}

func TestRegisterRepositoryEnforcesConfiguredOrganizationLimit(t *testing.T) {
	pool, base := identityIntegrationStore(t)
	ctx := context.Background()
	actor, organization := resourceLimitOrganization(t, base, "register-limit")
	store := NewStoreWithSettings(pool, StoreSettings{MaxRepositoriesPerOrganization: 1})
	server := insertLoreServerFixture(t, pool, organization.ID, "register-limit-"+organization.Slug)
	firstID := strings.ReplaceAll(uuid.NewString(), "-", "")
	secondID := strings.ReplaceAll(uuid.NewString(), "-", "")

	if _, err := store.RegisterRepository(ctx, actor, organization.Slug, RegisterRepositoryInput{
		Slug: "imported-a", DisplayName: "Imported A", Visibility: "private",
		LoreRepositoryID: firstID, LoreURL: server.PublicURL + "/" + firstID,
		LoreServerID: server.ID, DefaultBranch: "main",
	}); err != nil {
		t.Fatalf("first imported repository: %v", err)
	}
	if _, err := store.RegisterRepository(ctx, actor, organization.Slug, RegisterRepositoryInput{
		Slug: "imported-b", DisplayName: "Imported B", Visibility: "private",
		LoreRepositoryID: secondID, LoreURL: server.PublicURL + "/" + secondID,
		LoreServerID: server.ID, DefaultBranch: "main",
	}); !errors.Is(err, ErrRepositoryLimit) {
		t.Fatalf("second imported repository error = %v, want repository limit", err)
	}
}

func TestRepositoryLimitExcludesSoftDeletedRepositories(t *testing.T) {
	pool, base := identityIntegrationStore(t)
	ctx := context.Background()
	actor, organization := resourceLimitOrganization(t, base, "deleted-limit")
	store := NewStoreWithSettings(pool, StoreSettings{MaxRepositoriesPerOrganization: 1})
	server := insertLoreServerFixture(t, pool, organization.ID, "deleted-limit-"+organization.Slug)
	deletedID := strings.ReplaceAll(uuid.NewString(), "-", "")
	activeID := strings.ReplaceAll(uuid.NewString(), "-", "")

	deleted, err := store.RegisterRepository(ctx, actor, organization.Slug, RegisterRepositoryInput{
		Slug: "deleted", DisplayName: "Deleted", Visibility: "private",
		LoreRepositoryID: deletedID, LoreURL: server.PublicURL + "/" + deletedID,
		LoreServerID: server.ID, DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("deleted repository: %v", err)
	}
	mustIdentityExec(t, pool, `UPDATE repositories SET lifecycle_state = 'deleting' WHERE id = $1`, deleted.ID)

	if _, err := store.RegisterRepository(ctx, actor, organization.Slug, RegisterRepositoryInput{
		Slug: "active", DisplayName: "Active", Visibility: "private",
		LoreRepositoryID: activeID, LoreURL: server.PublicURL + "/" + activeID,
		LoreServerID: server.ID, DefaultBranch: "main",
	}); err != nil {
		t.Fatalf("active repository after soft delete: %v", err)
	}
}

func resourceLimitOrganization(t *testing.T, store *Store, prefix string) (User, Organization) {
	t.Helper()
	ctx := context.Background()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
	actor := platformTestUser(prefix + "-" + suffix)
	mustIdentityExec(t, store.pool, `INSERT INTO users (id, username, display_name) VALUES ($1, $2, $3)`,
		actor.ID, actor.Username, actor.DisplayName)
	organization, err := store.CreateOrganization(ctx, actor, CreateOrganizationInput{
		Slug: prefix + "-" + suffix, DisplayName: prefix, Visibility: "private",
	})
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, organization.ID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, actor.ID)
	})
	return actor, organization
}
