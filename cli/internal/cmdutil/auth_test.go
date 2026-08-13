package cmdutil

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lorehub/lorehub/cli/internal/config"
)

func TestAuthLoginWithTokenValidatesAndStoresToken(t *testing.T) {
	var authorization string
	var requestPath string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authorization = request.Header.Get("Authorization")
		requestPath = request.URL.Path
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(
			`{"user":{"id":"user-1","username":"alice","displayName":"Alice"},` +
				`"token":{"id":"token-1","prefix":"lhp_abc","permissions":["read_api",` +
				`"write_repository"],"expiresAt":"2026-08-14T12:00:00Z"}}`,
		))
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
		_, _ = writer.Write([]byte(
			`{"user":{"username":"alice"},"token":{"prefix":"lhp_abc",` +
				`"permissions":["read_api"],"expiresAt":"2026-08-14T12:00:00Z"}}`,
		))
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
	if !status.Authenticated || status.TokenSource != "environment" || status.User == nil ||
		status.User.Username != "alice" || status.TokenPrefix != "lhp_abc" ||
		len(status.Permissions) != 1 || status.ExpiresAt == nil ||
		!status.ExpiresAt.Equal(time.Date(2026, time.August, 14, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("status = %#v", status)
	}
}

func TestAuthLoginReturnsAPIProblem(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/problem+json")
		writer.WriteHeader(http.StatusUnauthorized)
		_, _ = writer.Write([]byte(`{"error":{"code":"invalid_token","detail":"The token is invalid"}}`))
	}))
	defer server.Close()

	command := NewRootCommand(Options{
		In:          strings.NewReader("bad-token\n"),
		ConfigPath:  filepath.Join(t.TempDir(), "hosts.yml"),
		DefaultHost: server.URL,
	})
	command.SetArgs([]string{"auth", "login", "--with-token"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "The token is invalid") {
		t.Fatalf("error = %v", err)
	}
}
