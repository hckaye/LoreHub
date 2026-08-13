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

type recordedRequest struct {
	method string
	path   string
	body   string
}

func TestFollowupResourceCommandsUsePublicAPI(t *testing.T) {
	var requests []recordedRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		contents, _ := io.ReadAll(request.Body)
		requests = append(requests, recordedRequest{
			method: request.Method, path: request.URL.Path, body: string(contents),
		})
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodPatch && strings.HasSuffix(request.URL.Path, "/issues/7"):
			_, _ = writer.Write([]byte(`{"number":7,"title":"Edited issue","state":"open","author":"alice"}`))
		case request.Method == http.MethodPatch && strings.HasSuffix(request.URL.Path, "/merge-requests/3"):
			_, _ = writer.Write([]byte(`{"number":3,"title":"Edited PR","state":"open","author":"alice"}`))
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/merge-requests/3/comments"):
			_, _ = writer.Write([]byte(`{"id":"comment-1","mergeRequestId":"pr-3","author":"alice","body":"Review"}`))
		case request.Method == http.MethodPost && strings.Contains(request.URL.Path, "/actions/runs/7/"):
			_, _ = writer.Write([]byte(`{"runNumber":7,"workflowName":"CI","status":"queued"}`))
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/labels"):
			_, _ = writer.Write([]byte(
				`{"items":[{"id":"label-7","name":"bug","color":"ff0000","description":"Old"}],"hasMore":false}`))
		case request.Method == http.MethodPatch && strings.HasSuffix(request.URL.Path, "/labels/label-7"):
			_, _ = writer.Write([]byte(`{"id":"label-7","name":"defect","color":"00ff00","description":"New"}`))
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/releases"):
			_, _ = writer.Write([]byte(
				`{"releases":[{"id":"11111111-1111-1111-1111-111111111111","tagName":"v1.0.0",` +
					`"title":"Old","notes":"Old notes","state":"draft","version":3}],"page":1,"perPage":20,"hasNext":false}`))
		case request.Method == http.MethodPatch && strings.Contains(request.URL.Path, "/releases/"):
			_, _ = writer.Write([]byte(
				`{"id":"11111111-1111-1111-1111-111111111111","tagName":"v1.0.0","title":"New","version":4}`))
		case request.Method == http.MethodDelete && strings.Contains(request.URL.Path, "/releases/"):
			writer.WriteHeader(http.StatusNoContent)
		default:
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte(`{"error":{"code":"not_found","detail":"missing"}}`))
		}
	}))
	defer server.Close()

	configPath := cliTestConfig(t, server.URL)
	commands := [][]string{
		{"issue", "edit", "7", "--title", "Edited issue", "--body", "New body"},
		{"pr", "edit", "3", "--title", "Edited PR"},
		{"pr", "comment", "3", "--body", "Review"},
		{"pr", "close", "3"},
		{"pr", "reopen", "3"},
		{"run", "cancel", "7"},
		{"run", "rerun", "7"},
		{"label", "edit", "bug", "--name", "defect", "--color", "00ff00", "--description", "New"},
		{"release", "edit", "v1.0.0", "--title", "New", "--notes", "New notes"},
		{"release", "delete", "v1.0.0"},
	}
	for _, args := range commands {
		var output bytes.Buffer
		if err := executeCLI(t, configPath, server.URL, &output, args...); err != nil {
			t.Fatalf("args %v: %v", args, err)
		}
	}

	assertRecordedJSON(t, requests, http.MethodPatch,
		"/api/v1/repositories/acme/widget/issues/7", map[string]any{
			"title": "Edited issue", "body": "New body",
		})
	assertRecordedJSON(t, requests, http.MethodPost,
		"/api/v1/repositories/acme/widget/merge-requests/3/comments", map[string]any{"body": "Review"})
	assertRecordedJSONValue(t, requests, http.MethodPatch,
		"/api/v1/repositories/acme/widget/merge-requests/3", "state", "closed")
	assertRecordedJSONValue(t, requests, http.MethodPatch,
		"/api/v1/repositories/acme/widget/merge-requests/3", "state", "open")
	assertRecordedJSON(t, requests, http.MethodPatch,
		"/api/v1/repositories/acme/widget/labels/label-7", map[string]any{
			"name": "defect", "color": "00ff00", "description": "New",
		})
	assertRecordedJSON(t, requests, http.MethodPatch,
		"/api/v1/repositories/acme/widget/releases/11111111-1111-1111-1111-111111111111", map[string]any{
			"title": "New", "notes": "New notes", "expectedVersion": float64(3),
		})
	assertRecordedJSON(t, requests, http.MethodDelete,
		"/api/v1/repositories/acme/widget/releases/11111111-1111-1111-1111-111111111111",
		map[string]any{"expectedVersion": float64(3)})

	joined := ""
	for _, request := range requests {
		joined += request.method + " " + request.path + "\n"
	}
	for _, expected := range []string{
		"POST /api/v1/repositories/acme/widget/actions/runs/7/cancel",
		"POST /api/v1/repositories/acme/widget/actions/runs/7/rerun",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("requests did not contain %q:\n%s", expected, joined)
		}
	}
}

func assertRecordedJSONValue(
	t *testing.T,
	requests []recordedRequest,
	method string,
	path string,
	key string,
	want any,
) {
	t.Helper()
	for _, request := range requests {
		if request.method != method || request.path != path {
			continue
		}
		var got map[string]any
		if json.Unmarshal([]byte(request.body), &got) == nil && got[key] == want {
			return
		}
	}
	t.Fatalf("request not found: %s %s with %s=%v", method, path, key, want)
}

func TestConfigAndCompletionCommands(t *testing.T) {
	t.Setenv("LH_TOKEN", "")
	t.Setenv("LH_REPO", "")
	t.Setenv("LH_HOST", "")
	configPath := filepath.Join(t.TempDir(), "hosts.yml")
	host := "https://lorehub.example"
	store := config.NewStore(configPath)
	if err := store.Save(config.Hosts{host: {Token: "saved-secret", DefaultRepo: "acme/old"}}); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := executeCLI(t, configPath, host, &output, "config", "get", "host"); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output.String()) != host {
		t.Fatalf("config host output = %q", output.String())
	}
	output.Reset()
	if err := executeCLI(t, configPath, host, &output,
		"config", "set", "default-repo", "acme/new"); err != nil {
		t.Fatal(err)
	}
	hosts, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if hosts[host].DefaultRepo != "acme/new" || hosts[host].Token != "saved-secret" {
		t.Fatalf("saved host config = %#v", hosts[host])
	}
	output.Reset()
	if err := executeCLI(t, configPath, host, &output, "config", "list"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "saved-secret") || !strings.Contains(output.String(), "hosts file") {
		t.Fatalf("config list output = %q", output.String())
	}
	output.Reset()
	if err := executeCLI(t, configPath, host, &output, "config", "unset", "default-repo"); err != nil {
		t.Fatal(err)
	}
	hosts, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if hosts[host].DefaultRepo != "" || hosts[host].Token != "saved-secret" {
		t.Fatalf("host after unset = %#v", hosts[host])
	}

	output.Reset()
	if err := executeCLI(t, configPath, host, &output, "completion", "bash"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "__start_lh") {
		t.Fatalf("bash completion output is incomplete")
	}
}

func assertRecordedJSON(
	t *testing.T,
	requests []recordedRequest,
	method string,
	path string,
	want map[string]any,
) {
	t.Helper()
	for _, request := range requests {
		if request.method != method || request.path != path {
			continue
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(request.body), &got); err != nil {
			t.Fatalf("decode %s %s body %q: %v", method, path, request.body, err)
		}
		if len(got) != len(want) {
			t.Fatalf("%s %s body = %#v, want %#v", method, path, got, want)
		}
		for key, value := range want {
			if got[key] != value {
				t.Fatalf("%s %s body[%s] = %#v, want %#v", method, path, key, got[key], value)
			}
		}
		return
	}
	t.Fatalf("request not found: %s %s", method, path)
}
