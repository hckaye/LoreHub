package platform

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCreateOrganizationGrantsConfiguredEntitlementsIntegration(t *testing.T) {
	fixture := authorizationIntegrationFixture(t)
	ctx := context.Background()
	store := NewStoreWithSettings(fixture.pool, StoreSettings{
		DefaultEntitlements: []string{EntitlementHostedLoreServer, EntitlementHostedRunners, "free_lunch"},
	})
	slug := "entitled-" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	organization, err := store.CreateOrganization(ctx, fixture.manager, CreateOrganizationInput{
		Slug:        slug,
		DisplayName: "Entitled organization",
		Visibility:  "public",
	})
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}

	rows, err := fixture.pool.Query(ctx, `
		SELECT feature, grant_source
		FROM entitlements
		WHERE organization_id = $1 AND revoked_at IS NULL
		ORDER BY feature
	`, organization.ID)
	if err != nil {
		t.Fatalf("read entitlements: %v", err)
	}
	defer rows.Close()
	var granted []string
	for rows.Next() {
		var feature, source string
		if err := rows.Scan(&feature, &source); err != nil {
			t.Fatalf("scan entitlement: %v", err)
		}
		if source != "default" {
			t.Fatalf("unexpected grant source %q for %q", source, feature)
		}
		granted = append(granted, feature)
	}
	if len(granted) != 2 || granted[0] != EntitlementHostedLoreServer || granted[1] != EntitlementHostedRunners {
		t.Fatalf("unexpected entitlements: %v", granted)
	}
}

func TestCreateOrganizationWithoutConfiguredEntitlementsIntegration(t *testing.T) {
	fixture := authorizationIntegrationFixture(t)
	ctx := context.Background()
	slug := "unentitled-" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	organization, err := fixture.store.CreateOrganization(ctx, fixture.manager, CreateOrganizationInput{
		Slug:        slug,
		DisplayName: "Unentitled organization",
		Visibility:  "public",
	})
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}

	var count int
	if err := fixture.pool.QueryRow(ctx, `
		SELECT count(*) FROM entitlements WHERE organization_id = $1
	`, organization.ID).Scan(&count); err != nil {
		t.Fatalf("count entitlements: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no entitlements, got %d", count)
	}
}
