package runner

import "testing"

func TestIsZeroLoreRevision(t *testing.T) {
	t.Parallel()
	zero := "0000000000000000000000000000000000000000000000000000000000000000"
	if !isZeroLoreRevision(zero) {
		t.Fatal("expected Lore's zero revision to be recognized")
	}
	for _, value := range []string{"", "0", zero[:63], zero[:63] + "1"} {
		if isZeroLoreRevision(value) {
			t.Fatalf("unexpected zero Lore revision: %q", value)
		}
	}
}
