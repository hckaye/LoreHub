package servercert

import "testing"

func TestPolicyClientCommonNames(t *testing.T) {
	serverID := "c727d690-34d4-4b44-bd13-a132f89c5919"
	for _, commonName := range []string{LegacyCommonName, "lore-server-" + serverID} {
		if !ValidCommonName(commonName) {
			t.Errorf("valid CommonName %q was rejected", commonName)
		}
	}
	if resolved, ok := ServerIDFromCommonName("lore-server-" + serverID); !ok || resolved != serverID {
		t.Fatalf("resolved server ID = %q, valid = %t", resolved, ok)
	}
	for _, commonName := range []string{
		"", "lore-server", "lore-server-not-a-uuid",
		"lore-server-C727D690-34D4-4B44-BD13-A132F89C5919", "other-client",
	} {
		if ValidCommonName(commonName) {
			t.Errorf("invalid CommonName %q was accepted", commonName)
		}
	}
}
