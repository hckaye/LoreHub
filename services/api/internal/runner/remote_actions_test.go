package runner

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareRemoteActionsFetchesSubpathsAndNestedCompositeActions(t *testing.T) {
	rootArchive := actionArchive(t, map[string]string{
		"root-v1/actions/action.yml": `name: root
runs:
  using: composite
  steps:
    - uses: nested/composite/subdir@v2
`,
		"root-v1/actions/run.sh": "echo root\n",
	})
	nestedArchive := actionArchive(t, map[string]string{
		"nested-v2/subdir/action.yaml": `name: nested
runs:
  using: composite
  steps:
    - uses: leaf/action@0123456789012345678901234567890123456789
`,
	})
	leafArchive := actionArchive(t, map[string]string{
		"0123456789012345678901234567890123456789/action.yml": `name: leaf
runs:
  using: node24
  main: dist/index.js
`,
	})
	githubScriptArchive := actionArchive(t, map[string]string{
		"github-script-v8/action.yml":    "name: github-script\nruns:\n  using: node24\n  main: dist/index.js\n",
		"github-script-v8/dist/index.js": "console.log('official action')\n",
	})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var archive []byte
		switch request.URL.Path {
		case "/example/root/archive/refs/tags/v1.tar.gz":
			archive = rootArchive
		case "/nested/composite/archive/refs/tags/v2.tar.gz":
			archive = nestedArchive
		case "/leaf/action/archive/0123456789012345678901234567890123456789.tar.gz":
			archive = leafArchive
		case "/actions/github-script/archive/refs/tags/v8.tar.gz":
			archive = githubScriptArchive
		default:
			http.NotFound(writer, request)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(archive)
	}))
	defer server.Close()
	t.Setenv("NO_PROXY", "127.0.0.1")
	workflow := filepath.Join(t.TempDir(), "workflow.yml")
	if err := os.WriteFile(workflow, []byte(`
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: example/root/actions@v1
      - uses: actions/github-script@v8
`), 0o600); err != nil {
		t.Fatal(err)
	}
	actionDirectory := filepath.Join(t.TempDir(), "actions")
	mappings, err := prepareRemoteActions(context.Background(), workflow, actionDirectory, server.URL, "development")
	if err != nil {
		t.Fatal(err)
	}
	if len(mappings) != 4 || !containsMapping(mappings, "example/root@v1=") ||
		containsMapping(mappings, "example/root/actions@v1=") ||
		!containsMapping(mappings, "nested/composite@v2=") ||
		!containsMapping(mappings, "leaf/action@0123456789012345678901234567890123456789=") ||
		!containsMapping(mappings, "actions/github-script@v8=") {
		t.Fatalf("unexpected action mappings: %#v", mappings)
	}
	destination := mappingDestination(mappings, "actions/github-script@v8")
	contents, err := os.ReadFile(filepath.Join(destination, "dist", "index.js"))
	if err != nil || string(contents) != "console.log('official action')\n" {
		t.Fatalf("remote Action was not extracted: %v %q", err, contents)
	}
	rootDestination := mappingDestination(mappings, "example/root@v1")
	if _, err := os.Stat(filepath.Join(rootDestination, "actions", "action.yml")); err != nil {
		t.Fatalf("subdirectory Action was not extracted: %v", err)
	}
}

func TestPrepareRemoteActionsRequiresHTTPSInProduction(t *testing.T) {
	workflow := filepath.Join(t.TempDir(), "workflow.yml")
	if err := os.WriteFile(workflow, []byte(`jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/test@v1
`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := prepareRemoteActions(context.Background(), workflow, filepath.Join(t.TempDir(), "actions"),
		"http://github.example", "production")
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("production accepted an HTTP Action source: %v", err)
	}
}

func TestPrepareRemoteActionsAllowsSameRepositorySubpath(t *testing.T) {
	archive := actionArchive(t, map[string]string{
		"repo-v1/action.yml": `runs:
  using: composite
  steps:
    - uses: owner/repo/subdir@v1
`,
		"repo-v1/subdir/action.yml": `runs:
  using: node24
  main: index.js
`,
		"repo-v1/subdir/index.js": "console.log('subpath')\n",
	})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/owner/repo/archive/refs/tags/v1.tar.gz" {
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write(archive)
	}))
	defer server.Close()
	workflow := filepath.Join(t.TempDir(), "workflow.yml")
	if err := os.WriteFile(workflow, []byte(`jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: owner/repo@v1
`), 0o600); err != nil {
		t.Fatal(err)
	}
	mappings, err := prepareRemoteActions(context.Background(), workflow, filepath.Join(t.TempDir(), "actions"),
		server.URL, "development")
	if err != nil {
		t.Fatal(err)
	}
	if len(mappings) != 1 || !containsMapping(mappings, "owner/repo@v1=") {
		t.Fatalf("same-repository subpath was not resolved with one archive mapping: %#v", mappings)
	}
	destination := mappingDestination(mappings, "owner/repo@v1")
	if _, err := os.Stat(filepath.Join(destination, "subdir", "action.yml")); err != nil {
		t.Fatalf("same-repository subpath manifest was not extracted: %v", err)
	}
}

func TestPrepareRemoteActionsRejectsCompositeDependencyCycles(t *testing.T) {
	aArchive := actionArchive(t, map[string]string{
		"a-v1/action.yml": "runs:\n  using: composite\n  steps:\n    - uses: owner/b@v1\n",
	})
	bArchive := actionArchive(t, map[string]string{
		"b-v1/action.yml": "runs:\n  using: composite\n  steps:\n    - uses: owner/a@v1\n",
	})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var archive []byte
		switch request.URL.Path {
		case "/owner/a/archive/refs/tags/v1.tar.gz":
			archive = aArchive
		case "/owner/b/archive/refs/tags/v1.tar.gz":
			archive = bArchive
		default:
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write(archive)
	}))
	defer server.Close()
	workflow := filepath.Join(t.TempDir(), "workflow.yml")
	if err := os.WriteFile(workflow, []byte(`jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: owner/a@v1
`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := prepareRemoteActions(context.Background(), workflow, filepath.Join(t.TempDir(), "actions"),
		server.URL, "development")
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("composite dependency cycle was accepted: %v", err)
	}
}

func TestExtractActionArchiveRejectsPathEscapes(t *testing.T) {
	archive := actionArchive(t, map[string]string{"root/../escape": "bad"})
	if err := extractActionArchive(archive, filepath.Join(t.TempDir(), "actions")); err == nil {
		t.Fatal("path escape was accepted")
	}
}

func TestParseRemoteActionRejectsExpressionsAndInvalidRefs(t *testing.T) {
	for _, uses := range []string{"actions/test@${{ inputs.ref }}", "actions/test@v1..bad", "actions/test"} {
		if _, err := parseRemoteAction(uses); err == nil {
			t.Fatalf("invalid remote Action reference was accepted: %q", uses)
		}
	}
	action, err := parseRemoteAction("owner/repository/sub/path@0123456789012345678901234567890123456789")
	if err != nil || action.Subpath != "sub/path" || action.repositoryKey() !=
		"owner/repository@0123456789012345678901234567890123456789" {
		t.Fatalf("subpath or immutable ref was not preserved: %#v, %v", action, err)
	}
	wantIdentity := "owner/repository@0123456789012345678901234567890123456789#sub/path"
	if action.identity() != wantIdentity {
		t.Fatalf("subpath was not included in the recursion identity: %q", action.identity())
	}
	immutableRef := "owner/repository@0123456789012345678901234567890123456789012345678901234567890123"
	if _, err := parseRemoteAction(immutableRef); err != nil {
		t.Fatalf("64-character immutable ref was rejected: %v", err)
	}
}

func containsMapping(mappings []string, prefix string) bool {
	for _, mapping := range mappings {
		if strings.HasPrefix(mapping, prefix) {
			return true
		}
	}
	return false
}

func mappingDestination(mappings []string, key string) string {
	for _, mapping := range mappings {
		if strings.HasPrefix(mapping, key+"=") {
			return strings.TrimPrefix(mapping, key+"=")
		}
	}
	return ""
}

func actionArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	tarWriter := tar.NewWriter(writer)
	for name, contents := range files {
		header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(contents))}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(tarWriter, strings.NewReader(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}
