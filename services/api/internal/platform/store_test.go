package platform

import "testing"

func TestValidateSlug(t *testing.T) {
	t.Parallel()
	valid := []string{"lore", "lore-hub", "a1"}
	for _, value := range valid {
		if err := validateSlug(value); err != nil {
			t.Errorf("expected %q to be valid: %v", value, err)
		}
	}
	invalid := []string{"Lore", "-lore", "lore-", "lore_hub", ""}
	for _, value := range invalid {
		if err := validateSlug(value); err == nil {
			t.Errorf("expected %q to be invalid", value)
		}
	}
}

func TestNormalizedUsername(t *testing.T) {
	t.Parallel()
	if value := normalizedUsername(" Lore.Hub ", "12345678-rest"); value != "lorehub" {
		t.Fatalf("unexpected normalized username %q", value)
	}
	if value := normalizedUsername("日本語", "12345678-rest"); value != "user-12345678" {
		t.Fatalf("unexpected fallback username %q", value)
	}
}
