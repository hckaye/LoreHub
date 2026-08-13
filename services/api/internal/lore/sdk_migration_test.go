package lore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplaceLoreRemote(t *testing.T) {
	workspace := t.TempDir()
	configDirectory := filepath.Join(workspace, ".lore")
	if err := os.Mkdir(configDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDirectory, "config.toml")
	contents := "remote_url = \"lores://source/partition\"\nidentity = \"migration\"\n"
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := replaceLoreRemote(workspace, "lores://target/partition"); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), `remote_url = "lores://target/partition"`) ||
		strings.Contains(string(updated), "lores://source/partition") {
		t.Fatalf("updated Lore config = %s", updated)
	}
}
