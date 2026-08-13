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

func TestResolveRepoPrecedence(t *testing.T) {
	if got, err := ResolveRepo("flag/one", "env/two", "stored/three"); err != nil || got != "flag/one" {
		t.Fatalf("flag repository = %q, %v", got, err)
	}
	if got, err := ResolveRepo("", "env/two", "stored/three"); err != nil || got != "env/two" {
		t.Fatalf("environment repository = %q, %v", got, err)
	}
	if got, err := ResolveRepo("", "", "stored/three"); err != nil || got != "stored/three" {
		t.Fatalf("stored repository = %q, %v", got, err)
	}
	if _, err := ResolveRepo("", "", ""); err == nil {
		t.Fatal("missing repository was accepted")
	}

	repository, err := ParseRepoContext("https://forge.example/acme/widget")
	if err != nil {
		t.Fatal(err)
	}
	if repository.Host != "https://forge.example" || repository.String() != "acme/widget" {
		t.Fatalf("qualified repository = %#v", repository)
	}
}

func TestRepoCommandsUseExpectedEndpoints(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		paths = append(paths, request.Method+" "+request.URL.Path)
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.URL.Path == "/api/v1/account":
			_, _ = writer.Write([]byte(`{"user":{"username":"alice"}}`))
		case request.URL.Path == "/api/v1/users/alice/repositories":
			_, _ = writer.Write([]byte(
				`{"repositories":[{"owner":"alice","slug":"notes","displayName":"Notes",` +
					`"visibility":"public","issueCount":2,"mergeRequestCount":1}]}`,
			))
		case request.URL.Path == "/api/v1/organizations/acme/repositories" && request.Method == http.MethodGet:
			_, _ = writer.Write([]byte(
				`{"repositories":[{"owner":"acme","slug":"widget","displayName":"Widget",` +
					`"visibility":"private"}]}`,
			))
		case request.URL.Path == "/api/v1/repositories/acme/widget":
			_, _ = writer.Write([]byte(
				`{"owner":"acme","slug":"widget","displayName":"Widget","visibility":"private",` +
					`"description":"A widget","defaultBranch":"main"}`,
			))
		case request.URL.Path == "/api/v1/organizations/acme/repositories" && request.Method == http.MethodPost:
			_, _ = writer.Write([]byte(`{"owner":"acme","slug":"new-widget","displayName":"new-widget","visibility":"public"}`))
		default:
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte(`{"error":{"code":"not_found","detail":"missing"}}`))
		}
	}))
	defer server.Close()
	t.Setenv("LH_TOKEN", "test-token")
	configPath := filepath.Join(t.TempDir(), "hosts.yml")
	store := config.NewStore(configPath)
	if err := store.Save(config.Hosts{server.URL: {DefaultRepo: "acme/widget"}}); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	command := NewRootCommand(Options{Out: &output, ErrOut: &output, ConfigPath: configPath, DefaultHost: server.URL})
	command.SetArgs([]string{"repo", "list"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "alice/notes") && !strings.Contains(output.String(), "alice\tnotes") {
		t.Fatalf("user repository output = %q", output.String())
	}

	output.Reset()
	command = NewRootCommand(Options{Out: &output, ErrOut: &output, ConfigPath: configPath, DefaultHost: server.URL})
	command.SetArgs([]string{"repo", "list", "acme"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "acme\twidget") {
		t.Fatalf("organization repository output = %q", output.String())
	}

	output.Reset()
	command = NewRootCommand(Options{Out: &output, ErrOut: &output, ConfigPath: configPath, DefaultHost: server.URL})
	command.SetArgs([]string{"--repo", "acme/widget", "repo", "view", "--json"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var viewed repository
	if err := json.Unmarshal(output.Bytes(), &viewed); err != nil {
		t.Fatal(err)
	}
	if viewed.Slug != "widget" {
		t.Fatalf("viewed repository = %#v", viewed)
	}

	output.Reset()
	command = NewRootCommand(Options{Out: &output, ErrOut: &output, ConfigPath: configPath, DefaultHost: server.URL})
	command.SetArgs([]string{
		"repo", "create", "acme/new-widget", "--visibility", "public", "--description", "A new widget",
	})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Created repository acme/new-widget") {
		t.Fatalf("create output = %q", output.String())
	}

	joined := strings.Join(paths, ",")
	for _, expected := range []string{
		"GET /api/v1/account",
		"GET /api/v1/users/alice/repositories",
		"GET /api/v1/organizations/acme/repositories",
		"GET /api/v1/repositories/acme/widget",
		"POST /api/v1/organizations/acme/repositories",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("paths = %q, missing %q", joined, expected)
		}
	}
}

func TestRepoCommandReturnsAPIProblem(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/problem+json")
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte(`{"error":{"code":"forbidden","detail":"Repository access is denied"}}`))
	}))
	defer server.Close()
	t.Setenv("LH_TOKEN", "test-token")
	configPath := filepath.Join(t.TempDir(), "hosts.yml")
	if err := config.NewStore(configPath).Save(config.Hosts{server.URL: {DefaultRepo: "acme/widget"}}); err != nil {
		t.Fatal(err)
	}

	command := NewRootCommand(Options{ConfigPath: configPath, DefaultHost: server.URL})
	command.SetArgs([]string{"repo", "view"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "Repository access is denied") {
		t.Fatalf("error = %v", err)
	}
}
