package cmdutil

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lorehub/lorehub/cli/internal/config"
)

func TestReleaseCommandsUseAPIEndpoints(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		paths = append(paths, request.Method+" "+request.URL.Path)
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/repositories/acme/widget/releases" &&
			request.URL.Query().Get("page") == "":
			_, _ = writer.Write([]byte(`{"releases":[{"id":"release-1","tagName":"v1.0.0","title":"First","state":"draft","sourceBranch":"main","revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}],"page":1,"perPage":20,"hasNext":false}`))
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/repositories/acme/widget/releases/11111111-1111-1111-1111-111111111111":
			_, _ = writer.Write([]byte(`{"id":"11111111-1111-1111-1111-111111111111","tagName":"v1.0.0","title":"First","state":"published","sourceBranch":"main"}`))
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/repositories/acme/widget/branches":
			_, _ = writer.Write([]byte(`{"branches":[{"name":"main","latestRevision":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","archived":false}]}`))
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/repositories/acme/widget/releases":
			var input map[string]string
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Errorf("decode release input: %v", err)
			}
			if input["tagName"] != "v2.0.0" || input["sourceBranch"] != "main" ||
				input["revision"] != strings.Repeat("b", 64) {
				t.Errorf("release input = %#v", input)
			}
			_, _ = writer.Write([]byte(`{"id":"release-2","tagName":"v2.0.0","title":"Second","state":"draft","sourceBranch":"main"}`))
		default:
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte(`{"error":{"code":"not_found","detail":"missing"}}`))
		}
	}))
	defer server.Close()
	configPath := cliTestConfig(t, server.URL)
	var output bytes.Buffer
	if err := executeCLI(t, configPath, server.URL, &output, "release", "list"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "v1.0.0") {
		t.Fatalf("release list output = %q", output.String())
	}

	output.Reset()
	if err := executeCLI(t, configPath, server.URL, &output, "release", "view", "v1.0.0", "--json"); err != nil {
		t.Fatal(err)
	}
	var viewed release
	if err := json.Unmarshal(output.Bytes(), &viewed); err != nil {
		t.Fatal(err)
	}
	if viewed.TagName != "v1.0.0" {
		t.Fatalf("release view = %#v", viewed)
	}

	output.Reset()
	if err := executeCLI(t, configPath, server.URL, &output, "release", "view", "11111111-1111-1111-1111-111111111111"); err != nil {
		t.Fatal(err)
	}
	if err := executeCLI(t, configPath, server.URL, &output, "release", "create", "--tag", "v2.0.0", "--title", "Second", "--notes", "Notes", "--branch", "main"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(paths, ",")
	for _, expected := range []string{
		"GET /api/v1/repositories/acme/widget/releases",
		"GET /api/v1/repositories/acme/widget/releases/11111111-1111-1111-1111-111111111111",
		"GET /api/v1/repositories/acme/widget/branches",
		"POST /api/v1/repositories/acme/widget/releases",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("paths = %q, missing %q", joined, expected)
		}
	}
}

func TestRunWatchPollsUntilTerminalAndUsesConclusion(t *testing.T) {
	var watchCalls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v1/repositories/acme/widget/actions/runs":
			_, _ = writer.Write([]byte(`{"runs":[{"runNumber":7,"workflowName":"CI","status":"in_progress","branch":"main","eventName":"push"}],"totalCount":1,"page":1,"perPage":30,"hasMore":false}`))
		case "/api/v1/repositories/acme/widget/actions/runs/7":
			watchCalls++
			if watchCalls == 1 {
				_, _ = writer.Write(runDetailJSON("in_progress", ""))
				return
			}
			_, _ = writer.Write(runDetailJSON("completed", "success"))
		case "/api/v1/repositories/acme/widget/actions/runs/8":
			_, _ = writer.Write(runDetailJSON("completed", "failure"))
		default:
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte(`{"error":{"code":"not_found","detail":"missing"}}`))
		}
	}))
	defer server.Close()
	configPath := cliTestConfig(t, server.URL)
	var output bytes.Buffer
	if err := executeCLI(t, configPath, server.URL, &output, "run", "list"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "7\tCI") {
		t.Fatalf("run list output = %q", output.String())
	}
	output.Reset()
	if err := executeCLI(t, configPath, server.URL, &output, "run", "watch", "7", "--interval", "1ms", "--timeout", "1s"); err != nil {
		t.Fatal(err)
	}
	if watchCalls != 2 || !strings.Contains(output.String(), "success") {
		t.Fatalf("watch calls = %d, output = %q", watchCalls, output.String())
	}
	command := NewRootCommand(Options{ConfigPath: configPath, DefaultHost: server.URL})
	command.SetArgs([]string{"run", "watch", "8", "--interval", "1ms", "--timeout", "1s"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "failure") {
		t.Fatalf("failed watch error = %v", err)
	}
}

func TestLabelDeleteResolvesNameToID(t *testing.T) {
	var deletedPath string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/repositories/acme/widget/labels":
			_, _ = writer.Write([]byte(`{"items":[{"id":"label-7","name":"bug","color":"ff0000","description":"A bug"}],"hasMore":false}`))
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/repositories/acme/widget/labels":
			_, _ = writer.Write([]byte(`{"id":"label-8","name":"feature","color":"00ff00","description":"A feature"}`))
		case request.Method == http.MethodDelete:
			deletedPath = request.URL.Path
			writer.WriteHeader(http.StatusNoContent)
		default:
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte(`{"error":{"code":"not_found","detail":"missing"}}`))
		}
	}))
	defer server.Close()
	configPath := cliTestConfig(t, server.URL)
	var output bytes.Buffer
	if err := executeCLI(t, configPath, server.URL, &output, "label", "list"); err != nil {
		t.Fatal(err)
	}
	if err := executeCLI(t, configPath, server.URL, &output, "label", "create", "--name", "feature", "--color", "00ff00"); err != nil {
		t.Fatal(err)
	}
	if err := executeCLI(t, configPath, server.URL, &output, "label", "delete", "bug"); err != nil {
		t.Fatal(err)
	}
	if deletedPath != "/api/v1/repositories/acme/widget/labels/label-7" {
		t.Fatalf("deleted path = %q", deletedPath)
	}
}

func TestSearchCommandsSendAPIType(t *testing.T) {
	types := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/search" {
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		types[request.URL.Query().Get("q")] = request.URL.Query().Get("type")
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"repositories":[{"owner":"acme","slug":"widget","displayName":"Widget","visibility":"public"}],"organizations":[],"users":[],"issues":[{"repository":{"owner":"acme","slug":"widget"},"number":1,"title":"Bug","state":"open","author":{"username":"alice"}}],"pullRequests":[],"counts":{},"page":1,"perPage":20}`))
	}))
	defer server.Close()
	configPath := cliTestConfig(t, server.URL)
	for _, args := range [][]string{
		{"search", "repos", "widget"},
		{"search", "issues", "bug"},
		{"search", "prs", "feature"},
	} {
		var output bytes.Buffer
		if err := executeCLI(t, configPath, server.URL, &output, args...); err != nil {
			t.Fatalf("args %v: %v", args, err)
		}
	}
	if types["widget"] != "repositories" || types["bug"] != "issues" || types["feature"] != "pulls" {
		t.Fatalf("search types = %#v", types)
	}
}

func TestRepoCloneUsesRepositoryURLAndFakeLore(t *testing.T) {
	const token = "test-clone-token"
	var serverRequests []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serverRequests = append(serverRequests, request.Method+" "+request.URL.Path)
		writer.Header().Set("Content-Type", "application/json")
		if request.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		switch request.URL.Path {
		case "/api/v1/repositories/acme/widget":
			_, _ = writer.Write([]byte(`{"owner":"acme","slug":"widget","loreUrl":"lores://lore.example/0123456789abcdef0123456789abcdef"}`))
		case "/api/v1/account":
			_, _ = writer.Write([]byte(`{"user":{"username":"alice"},"token":{"permissions":["read_api","read_repository"]}}`))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	t.Setenv("LH_TOKEN", token)
	toolDir := t.TempDir()
	logPath := filepath.Join(toolDir, "lore.log")
	lorePath := filepath.Join(toolDir, "lore")
	script := "#!/bin/sh\nset -eu\nprintf '%s\\n' \"$@\" >> \"$LORE_FAKE_LOG\"\n"
	if err := os.WriteFile(lorePath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LORE_FAKE_LOG", logPath)
	t.Setenv("PATH", toolDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	configPath := cliTestConfig(t, server.URL)
	t.Setenv("LH_TOKEN", token)
	var output bytes.Buffer
	if err := executeCLI(t, configPath, server.URL, &output, "repo", "clone", "acme/widget", "target"); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(contents)
	if !strings.Contains(log, "auth\nlogin\n") || !strings.Contains(log, "--token\n"+token+"\n") ||
		!strings.Contains(log, "clone\nlores://lore.example/0123456789abcdef0123456789abcdef\ntarget\n") {
		t.Fatalf("fake lore log = %q", log)
	}
	if strings.Contains(output.String(), token) {
		t.Fatalf("clone output contains token: %q", output.String())
	}
	if len(serverRequests) != 2 {
		t.Fatalf("server requests = %v", serverRequests)
	}
}

func cliTestConfig(t *testing.T, host string) string {
	t.Helper()
	t.Setenv("LH_TOKEN", "test-token")
	path := filepath.Join(t.TempDir(), "hosts.yml")
	if err := config.NewStore(path).Save(config.Hosts{host: {DefaultRepo: "acme/widget"}}); err != nil {
		t.Fatal(err)
	}
	return path
}

func executeCLI(t *testing.T, configPath string, host string, output *bytes.Buffer, args ...string) error {
	t.Helper()
	command := NewRootCommand(Options{Out: output, ErrOut: output, ConfigPath: configPath, DefaultHost: host})
	command.SetArgs(args)
	return command.Execute()
}

func runDetailJSON(status string, conclusion string) []byte {
	var conclusionJSON string
	if conclusion == "" {
		conclusionJSON = "null"
	} else {
		contents, _ := json.Marshal(conclusion)
		conclusionJSON = string(contents)
	}
	return []byte(`{"run":{"runNumber":7,"workflowName":"CI","status":"` + status + `","conclusion":` + conclusionJSON + `,"branch":"main","eventName":"push"},"workflow":{},"jobs":[],"artifacts":[]}`)
}
