package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestHostsRoundTripAndPermissions(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "lh", "hosts.yml"))
	want := Hosts{
		"example.com": {
			Token:       "lhp_example",
			DefaultRepo: "acme/widget",
		},
		"http://localhost:3000": {
			Token: "lhp_local",
		},
	}

	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hosts round trip = %#v, want %#v", got, want)
	}
	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if permissions := info.Mode().Perm(); permissions != 0o600 {
		t.Fatalf("hosts permissions = %o, want 600", permissions)
	}
}

func TestEnvironmentPrecedence(t *testing.T) {
	t.Setenv("LH_HOST", "")
	if got := ResolveHost("", "configured.example"); got != "configured.example" {
		t.Fatalf("default host = %q", got)
	}
	t.Setenv("LH_HOST", "https://environment.example/")
	if got := ResolveHost("", "configured.example"); got != "https://environment.example" {
		t.Fatalf("environment host = %q", got)
	}
	if got := ResolveHost("flag.example", "configured.example"); got != "flag.example" {
		t.Fatalf("flag host = %q", got)
	}

	t.Setenv("LH_TOKEN", "environment-token")
	if got, source := ResolveToken("file-token"); got != "environment-token" || source != "environment" {
		t.Fatalf("environment token = %q from %q", got, source)
	}
	t.Setenv("LH_TOKEN", "")
	if got, source := ResolveToken("file-token"); got != "file-token" || source != "hosts file" {
		t.Fatalf("file token = %q from %q", got, source)
	}
}

func TestParseRepo(t *testing.T) {
	if got, err := ParseRepo(" acme/widget "); err != nil || got != "acme/widget" {
		t.Fatalf("ParseRepo = %q, %v", got, err)
	}
	if _, err := ParseRepo("acme"); err == nil {
		t.Fatal("ParseRepo accepted a repository without a name")
	}
}
