package httpapi

import (
	"strings"
	"testing"
)

func TestConfiguredDependenciesRejectIncompleteServer(t *testing.T) {
	err := (&API{}).validateConfiguredDependencies()
	if err == nil {
		t.Fatal("expected incomplete HTTP API configuration to fail")
	}
	for _, name := range []string{"store", "lore", "Actions", "operational endpoints"} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("expected missing dependency %q in %q", name, err)
		}
	}
}
