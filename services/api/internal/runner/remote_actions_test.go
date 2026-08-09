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

func TestPrepareRemoteActionsFetchesArchiveAndSkipsLoreCheckout(t *testing.T) {
	archive := actionArchive(t, map[string]string{
		"github-script-v8/action.yml":    "name: github-script\nruns:\n  using: node24\n  main: dist/index.js\n",
		"github-script-v8/dist/index.js": "console.log('official action')\n",
	})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasSuffix(request.URL.Path, "/archive/refs/tags/v8.tar.gz") {
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
      - uses: actions/github-script@v8
`), 0o600); err != nil {
		t.Fatal(err)
	}
	actionDirectory := filepath.Join(t.TempDir(), "actions")
	mappings, err := prepareRemoteActions(context.Background(), workflow, actionDirectory, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(mappings) != 1 || !strings.HasPrefix(mappings[0], "actions/github-script@v8=") {
		t.Fatalf("unexpected action mappings: %#v", mappings)
	}
	destination := strings.TrimPrefix(mappings[0], "actions/github-script@v8=")
	contents, err := os.ReadFile(filepath.Join(destination, "dist", "index.js"))
	if err != nil || string(contents) != "console.log('official action')\n" {
		t.Fatalf("remote Action was not extracted: %v %q", err, contents)
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
