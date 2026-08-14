package config

import (
	"strings"
	"testing"
)

func TestDefaultOrganizationEntitlementsReadsConfiguredFeatures(t *testing.T) {
	features, err := defaultOrganizationEntitlements(" hosted_lore_server , HOSTED_RUNNERS ,hosted_lore_server, ")
	if err != nil {
		t.Fatalf("read default entitlements: %v", err)
	}
	if len(features) != 2 || features[0] != "hosted_lore_server" || features[1] != "hosted_runners" {
		t.Fatalf("unexpected features: %v", features)
	}
}

func TestDefaultOrganizationEntitlementsAreEmptyByDefault(t *testing.T) {
	features, err := defaultOrganizationEntitlements("")
	if err != nil {
		t.Fatalf("read default entitlements: %v", err)
	}
	if len(features) != 0 {
		t.Fatalf("expected no features, got %v", features)
	}
}

func TestDefaultOrganizationEntitlementsRejectUnknownFeatures(t *testing.T) {
	if _, err := defaultOrganizationEntitlements("hosted_lore_server,free_lunch"); err == nil ||
		!strings.Contains(err.Error(), "free_lunch") {
		t.Fatalf("expected an error naming the unknown feature, got %v", err)
	}
}
