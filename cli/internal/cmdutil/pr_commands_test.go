package cmdutil

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lorehub/lorehub/cli/internal/config"
)

func TestPRCommandsAndBoundedMerge(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		paths = append(paths, request.Method+" "+request.URL.Path)
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/repositories/acme/widget/merge-requests":
			_, _ = writer.Write([]byte(
				`{"mergeRequests":[{"number":3,"title":"Add feature","state":"open",` +
					`"author":"alice","sourceBranch":"feature","targetBranch":"main"}],` +
					`"totalCount":1,"openCount":1,"closedCount":0,"mergedCount":0,` +
					`"page":1,"perPage":25,"hasNext":false}`,
			))
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/repositories/acme/widget/merge-requests/3":
			_, _ = writer.Write([]byte(
				`{"number":3,"title":"Add feature","body":"Details","state":"open",` +
					`"author":"alice","sourceBranch":"feature","targetBranch":"main"}`,
			))
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/repositories/acme/widget/merge-requests":
			_, _ = writer.Write([]byte(
				`{"number":4,"title":"New PR","state":"open","sourceBranch":"feature-2",` +
					`"targetBranch":"main","author":"alice"}`,
			))
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/merge-readiness"):
			_, _ = writer.Write([]byte(`{"ready":true,"canMerge":true,"blockers":[]}`))
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/merge/start"):
			_, _ = writer.Write([]byte(`{"id":"operation-1","state":"started"}`))
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/merge-operation"):
			_, _ = writer.Write([]byte(`{"id":"operation-1","state":"ready_to_push"}`))
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/merge/push"):
			contents, _ := io.ReadAll(request.Body)
			if len(contents) != 0 {
				t.Errorf("push body = %s", contents)
			}
			_, _ = writer.Write([]byte(`{"number":3,"title":"Add feature","state":"merged"}`))
		default:
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte(`{"error":{"code":"not_found","detail":"missing"}}`))
		}
	}))
	defer server.Close()
	t.Setenv("LH_TOKEN", "test-token")
	configPath := filepath.Join(t.TempDir(), "hosts.yml")
	if err := config.NewStore(configPath).Save(config.Hosts{server.URL: {DefaultRepo: "acme/widget"}}); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	command := NewRootCommand(Options{Out: &output, ErrOut: &output, ConfigPath: configPath, DefaultHost: server.URL})
	command.SetArgs([]string{"pr", "list"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "3\tAdd feature") {
		t.Fatalf("pr list output = %q", output.String())
	}

	output.Reset()
	command = NewRootCommand(Options{Out: &output, ErrOut: &output, ConfigPath: configPath, DefaultHost: server.URL})
	command.SetArgs([]string{"--json", "pr", "view", "3"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var viewed mergeRequest
	if err := json.Unmarshal(output.Bytes(), &viewed); err != nil {
		t.Fatal(err)
	}
	if viewed.Number != 3 || viewed.SourceBranch != "feature" {
		t.Fatalf("pr view = %#v", viewed)
	}

	for _, args := range [][]string{
		{"pr", "create", "--title", "New PR", "--body", "Body", "--source", "feature-2", "--target", "main"},
		{"pr", "merge", "3", "--poll-interval", "1ms", "--timeout", "1s"},
	} {
		output.Reset()
		command = NewRootCommand(Options{Out: &output, ErrOut: &output, ConfigPath: configPath, DefaultHost: server.URL})
		command.SetArgs(args)
		if err := command.Execute(); err != nil {
			t.Fatalf("args %v: %v", args, err)
		}
	}

	joined := strings.Join(paths, ",")
	for _, expected := range []string{
		"GET /api/v1/repositories/acme/widget/merge-requests",
		"GET /api/v1/repositories/acme/widget/merge-requests/3",
		"POST /api/v1/repositories/acme/widget/merge-requests",
		"GET /api/v1/repositories/acme/widget/merge-requests/3/merge-readiness",
		"POST /api/v1/repositories/acme/widget/merge-requests/3/merge/start",
		"GET /api/v1/repositories/acme/widget/merge-requests/3/merge-operation",
		"POST /api/v1/repositories/acme/widget/merge-requests/3/merge/push",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("paths = %q, missing %q", joined, expected)
		}
	}
}

func TestPRMergeReportsReadinessProblem(t *testing.T) {
	var started bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(request.URL.Path, "/merge-readiness") {
			_, _ = writer.Write([]byte(
				`{"ready":false,"canMerge":false,"blockers":[{"code":"ci_failed",` +
					`"detail":"CI failed"}]}`,
			))
			return
		}
		if strings.HasSuffix(request.URL.Path, "/merge/start") {
			started = true
		}
		writer.WriteHeader(http.StatusNotFound)
		_, _ = writer.Write([]byte(`{"error":{"code":"not_found","detail":"unexpected"}}`))
	}))
	defer server.Close()
	t.Setenv("LH_TOKEN", "test-token")
	configPath := filepath.Join(t.TempDir(), "hosts.yml")
	if err := config.NewStore(configPath).Save(config.Hosts{server.URL: {DefaultRepo: "acme/widget"}}); err != nil {
		t.Fatal(err)
	}

	command := NewRootCommand(Options{ConfigPath: configPath, DefaultHost: server.URL})
	command.SetArgs([]string{"pr", "merge", "3"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "merge is not ready") {
		t.Fatalf("error = %v", err)
	}
	if started {
		t.Fatal("merge start was called while readiness was blocked")
	}
}

func TestPRCommandReturnsAPIProblem(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/problem+json")
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte(`{"error":{"code":"forbidden","detail":"Pull requests are disabled"}}`))
	}))
	defer server.Close()
	t.Setenv("LH_TOKEN", "test-token")
	configPath := filepath.Join(t.TempDir(), "hosts.yml")
	if err := config.NewStore(configPath).Save(config.Hosts{server.URL: {DefaultRepo: "acme/widget"}}); err != nil {
		t.Fatal(err)
	}

	command := NewRootCommand(Options{ConfigPath: configPath, DefaultHost: server.URL})
	command.SetArgs([]string{"pr", "list"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "Pull requests are disabled") {
		t.Fatalf("error = %v", err)
	}
}
