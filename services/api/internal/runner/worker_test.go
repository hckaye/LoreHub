package runner

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBoundedLogWriter(t *testing.T) {
	var output bytes.Buffer
	writer := &boundedLogWriter{writer: &output, remaining: 128}
	if written, err := writer.Write([]byte("123456789")); err != nil || written != 9 {
		t.Fatalf("bounded writer returned %d/%v", written, err)
	}
	if written, err := writer.Write([]byte(strings.Repeat("x", 200))); err != nil || written != 200 {
		t.Fatalf("bounded writer returned %d/%v after limit", written, err)
	}
	if output.Len() > 128 || strings.Count(output.String(), "log limit reached") != 1 ||
		!strings.HasPrefix(output.String(), "123456789") {
		t.Fatalf("unexpected bounded log output: %q", output.String())
	}
}

func TestSafeEnvironmentPassesDockerTLSConfiguration(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://runner-engine:2376")
	t.Setenv("DOCKER_TLS_VERIFY", "1")
	t.Setenv("DOCKER_CERT_PATH", "/etc/lorehub/docker-client")
	t.Setenv("DOCKER_CONFIG", "")
	environment := safeEnvironment()
	joined := strings.Join(environment, "\n")
	for _, expected := range []string{
		"DOCKER_HOST=tcp://runner-engine:2376",
		"DOCKER_TLS_VERIFY=1",
		"DOCKER_CERT_PATH=/etc/lorehub/docker-client",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("safe environment omitted %q: %q", expected, joined)
		}
	}
}

func TestPersistArtifactsQuotasAndPathSafety(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	artifactDirectory := t.TempDir()
	worker := &Worker{
		config: WorkerConfig{
			ArtifactDir:           artifactDirectory,
			ArtifactMaxCount:      1,
			ArtifactMaxFileBytes:  4,
			ArtifactMaxTotalBytes: 4,
		},
		logger: logger,
	}
	job := Job{ID: "job-one", RepositoryID: "repo-one", RunID: "run-one", Attempt: 1}
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "output.txt"), []byte("four"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifacts, err := worker.persistArtifacts(job, source)
	if err != nil || len(artifacts) != 1 || artifacts[0].Size != 4 {
		t.Fatalf("persisted artifacts were unexpected: %#v, %v", artifacts, err)
	}
	if _, err := os.Stat(filepath.Join(artifactDirectory, filepath.FromSlash(artifacts[0].ObjectKey))); err != nil {
		t.Fatalf("persisted artifact is missing: %v", err)
	}

	quotaJob := Job{ID: "job-quota", RepositoryID: "repo-one", RunID: "run-one", Attempt: 1}
	quotaSource := t.TempDir()
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(quotaSource, name), []byte("four"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := worker.persistArtifacts(quotaJob, quotaSource); err == nil {
		t.Fatal("artifact count quota was not enforced")
	}
	quotaRoot := filepath.Join(artifactDirectory, quotaJob.RepositoryID, quotaJob.RunID, quotaJob.ID)
	if entries, readErr := os.ReadDir(quotaRoot); readErr == nil {
		for _, entry := range entries {
			if strings.Contains(entry.Name(), ".partial-") || entry.Name() == "attempt-1" {
				t.Fatalf("partial artifact output remained: %s", entry.Name())
			}
		}
	}

	symlinkJob := Job{ID: "job-symlink", RepositoryID: "repo-one", RunID: "run-one", Attempt: 1}
	symlinkSource := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(symlinkSource, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.persistArtifacts(symlinkJob, symlinkSource); err == nil {
		t.Fatal("artifact symlink was accepted")
	}
	if _, err := safeArtifactPath(artifactDirectory, "../outside"); err == nil {
		t.Fatal("artifact path escape was accepted")
	}
}
