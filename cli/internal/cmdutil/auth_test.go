package cmdutil

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lorehub/lorehub/cli/internal/config"
)

func TestAuthLoginWithTokenValidatesAndStoresToken(t *testing.T) {
	var authorization string
	var requestPath string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authorization = request.Header.Get("Authorization")
		requestPath = request.URL.Path
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"repositories":[]}`))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config", "lh", "hosts.yml")
	var output bytes.Buffer
	command := NewRootCommand(Options{
		In:         strings.NewReader("lhp_test-token\n"),
		Out:        &output,
		ErrOut:     &output,
		ConfigPath: configPath,
	})
	command.SetArgs([]string{"--host", server.URL, "auth", "login", "--with-token"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if authorization != "Bearer lhp_test-token" {
		t.Fatalf("authorization = %q", authorization)
	}
	if requestPath != "/api/v1/account" {
		t.Fatalf("validation path = %q", requestPath)
	}
	hosts, err := config.NewStore(configPath).Load()
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := hosts[server.URL]
	if !ok || entry.Token != "lhp_test-token" {
		t.Fatalf("stored hosts = %#v", hosts)
	}
	if !strings.Contains(output.String(), "Logged in to "+server.URL) {
		t.Fatalf("output = %q", output.String())
	}
}

func TestAuthStatusUsesEnvironmentToken(t *testing.T) {
	t.Setenv("LH_TOKEN", "environment-token")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer environment-token" {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"repositories":[]}`))
	}))
	defer server.Close()

	var output bytes.Buffer
	command := NewRootCommand(Options{
		Out:         &output,
		ErrOut:      &output,
		ConfigPath:  filepath.Join(t.TempDir(), "hosts.yml"),
		DefaultHost: server.URL,
	})
	command.SetArgs([]string{"auth", "status", "--json"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var status authStatus
	if err := json.Unmarshal(output.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if !status.Authenticated || status.TokenSource != "environment" {
		t.Fatalf("status = %#v", status)
	}
}
