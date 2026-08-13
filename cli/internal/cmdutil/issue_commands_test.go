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

func TestIssueCommandsUseAPIEndpoints(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		paths = append(paths, request.Method+" "+request.URL.RequestURI())
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/repositories/acme/widget/issues":
			if request.URL.Query().Get("state") != "closed" || request.URL.Query().Get("q") != "bug" ||
				request.URL.Query().Get("author") != "alice" || request.URL.Query().Get("label") != "defect" {
				t.Errorf("issue query = %s", request.URL.RawQuery)
			}
			_, _ = writer.Write([]byte(`{"issues":[{"number":7,"title":"Fix bug","state":"closed","author":"alice","commentCount":1}],"totalCount":1,"openCount":0,"closedCount":1,"page":1,"perPage":25,"hasNext":false}`))
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/repositories/acme/widget/issues/7":
			_, _ = writer.Write([]byte(`{"number":7,"title":"Fix bug","body":"Details","state":"open","author":"alice","commentCount":1}`))
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/repositories/acme/widget/issues/7/comments":
			_, _ = writer.Write([]byte(`{"items":[{"id":"comment-1","author":"bob","body":"Looks good","createdAt":"2026-08-13T00:00:00Z"}],"hasMore":false}`))
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/repositories/acme/widget/issues":
			_, _ = writer.Write([]byte(`{"number":8,"title":"New issue","state":"open","author":"alice"}`))
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/repositories/acme/widget/issues/7/comments":
			_, _ = writer.Write([]byte(`{"id":"comment-2","issueId":"issue-7","author":"alice","body":"A comment"}`))
		case request.Method == http.MethodPatch && request.URL.Path == "/api/v1/repositories/acme/widget/issues/7":
			contents, _ := io.ReadAll(request.Body)
			if string(contents) != `{"state":"closed"}` {
				t.Errorf("issue patch body = %s", contents)
			}
			_, _ = writer.Write([]byte(`{"number":7,"title":"Fix bug","state":"closed","author":"alice"}`))
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
	command.SetArgs([]string{"issue", "list", "--state", "closed", "--author", "alice", "--label", "defect", "--search", "bug"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "7\tFix bug") {
		t.Fatalf("issue list output = %q", output.String())
	}

	output.Reset()
	command = NewRootCommand(Options{Out: &output, ErrOut: &output, ConfigPath: configPath, DefaultHost: server.URL})
	command.SetArgs([]string{"--json", "issue", "view", "7"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var view issueView
	if err := json.Unmarshal(output.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Issue.Number != 7 || len(view.Comments.Items) != 1 {
		t.Fatalf("issue view = %#v", view)
	}

	for _, args := range [][]string{
		{"issue", "create", "--title", "New issue", "--body", "Body"},
		{"issue", "comment", "7", "--body", "A comment"},
		{"issue", "close", "7"},
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
		"GET /api/v1/repositories/acme/widget/issues?",
		"GET /api/v1/repositories/acme/widget/issues/7",
		"GET /api/v1/repositories/acme/widget/issues/7/comments",
		"POST /api/v1/repositories/acme/widget/issues",
		"POST /api/v1/repositories/acme/widget/issues/7/comments",
		"PATCH /api/v1/repositories/acme/widget/issues/7",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("paths = %q, missing %q", joined, expected)
		}
	}
}

func TestIssueCommandReturnsAPIProblem(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/problem+json")
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte(`{"error":{"code":"forbidden","detail":"Issues are disabled"}}`))
	}))
	defer server.Close()
	t.Setenv("LH_TOKEN", "test-token")
	configPath := filepath.Join(t.TempDir(), "hosts.yml")
	if err := config.NewStore(configPath).Save(config.Hosts{server.URL: {DefaultRepo: "acme/widget"}}); err != nil {
		t.Fatal(err)
	}

	command := NewRootCommand(Options{ConfigPath: configPath, DefaultHost: server.URL})
	command.SetArgs([]string{"issue", "list"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "Issues are disabled") {
		t.Fatalf("error = %v", err)
	}
}
