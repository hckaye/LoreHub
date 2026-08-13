package runner

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActArgumentsPreserveSupportedEventAndExactWorkflow(t *testing.T) {
	for _, event := range []string{"push", "workflow_dispatch", "pull_request", "schedule", "repository_dispatch"} {
		t.Run(event, func(t *testing.T) {
			job := Job{
				EventName:    event,
				Owner:        "owner",
				Repository:   "repository",
				Revision:     "lore-revision",
				Branch:       "main",
				EventPayload: json.RawMessage(`{"ref":"refs/heads/main"}`),
			}
			args := actArguments(
				job,
				"/work/repository",
				"/work/repository/.github/workflows/ci.yml",
				"/work/event.json",
				"/work/artifacts",
				"lorehub-job-test",
				"http://172.28.244.2:3128",
				actInvocation{
					PlatformImages: DefaultRunnerPlatformImages(),
					GitHub: GitHubContext{
						ServerURL: "http://lorehub.test", APIURL: "http://lorehub.test/api/v1",
						GraphQLURL: "http://lorehub.test/api/graphql",
					},
				},
			)
			if args[0] != event {
				t.Fatalf("act event argument was %q, want %q", args[0], event)
			}
			assertArgumentValue(t, args, "--workflows", "/work/repository/.github/workflows/ci.yml")
			assertArgumentValue(t, args, "--eventpath", "/work/event.json")
			assertArgumentValue(t, args, "--artifact-server-addr", defaultArtifactServerAddress)
			assertArgumentValue(t, args, "--network", "lorehub-job-test")
			assertArgumentValue(t, args, "--env", "SHA_REF=lore-revision")
			assertArgumentValue(t, args, "--env", "GITHUB_SHA=lore-revision")
			assertArgumentValue(t, args, "--env", "GITHUB_REF=refs/heads/main")
			assertArgumentValue(t, args, "--env", "GITHUB_REPOSITORY=owner/repository")
			assertArgumentValue(t, args, "--env", "GITHUB_EVENT_NAME="+event)
			assertArgumentValue(t, args, "--env", "GITHUB_SERVER_URL=http://lorehub.test")
			assertArgumentValue(t, args, "--env", "GITHUB_API_URL=http://lorehub.test/api/v1")
			assertArgumentValue(t, args, "--env", "GITHUB_GRAPHQL_URL=http://lorehub.test/api/graphql")
			assertArgumentValue(t, args, "--platform", "ubuntu-latest="+DefaultUbuntuLatestImage)
			if countArgument(args, "--network") != 1 || countArgument(args, "--workflows") != 1 {
				t.Fatalf("act received duplicate network/workflow selectors: %#v", args)
			}
		})
	}
}

func TestActArgumentsPassVariablesAndSecretFilePathOnly(t *testing.T) {
	job := Job{EventName: "workflow_dispatch", Revision: "revision", Branch: "main"}
	args := actArguments(
		job,
		"/work/repository",
		"/work/repository/.github/workflows/ci.yml",
		"/work/event.json",
		"/work/artifacts",
		"lorehub-job-test",
		"http://172.28.244.2:3128",
		actInvocation{
			PlatformImages: DefaultRunnerPlatformImages(),
			GitHub: GitHubContext{
				ServerURL: "http://lorehub.test", APIURL: "http://lorehub.test/api/v1",
				GraphQLURL: "http://lorehub.test/api/graphql",
			},
			Variables:  map[string]string{"DEPLOY_TARGET": "staging"},
			SecretFile: "/tmp/actions-secrets-123",
		},
	)
	assertArgumentValue(t, args, "--var", "DEPLOY_TARGET=staging")
	if containsArgumentValue(args, "--env", "DEPLOY_TARGET=staging") {
		t.Fatal("repository Actions variable was incorrectly passed as an environment variable")
	}
	assertArgumentValue(t, args, "--secret-file", "/tmp/actions-secrets-123")
	if strings.Contains(strings.Join(args, "\x00"), "super-secret") {
		t.Fatal("secret value appeared in act arguments")
	}
}

func TestActArgumentsFilterNamedJob(t *testing.T) {
	job := Job{JobName: "build", EventName: "push", Revision: "revision", Branch: "main"}
	args := actArguments(
		job, "/work/repository", "/work/repository/.github/workflows/ci.yml",
		"/work/event.json", "/work/artifacts", "job-network", "",
		actInvocation{PlatformImages: DefaultRunnerPlatformImages()},
	)
	assertArgumentValue(t, args, "--job", "build")
	externalArgs := ExternalActArguments(
		job, "/work/repository", "/work/repository/.github/workflows/ci.yml",
		"/work/event.json", "/work/artifacts",
		ExternalActInvocation{PlatformImages: DefaultRunnerPlatformImages()},
	)
	assertArgumentValue(t, externalArgs, "--job", "build")

	job.JobName = ""
	legacyArgs := actArguments(
		job, "/work/repository", "/work/repository/.github/workflows/ci.yml",
		"/work/event.json", "/work/artifacts", "job-network", "",
		actInvocation{PlatformImages: DefaultRunnerPlatformImages()},
	)
	if countArgument(legacyArgs, "--job") != 0 {
		t.Fatalf("legacy workflow job received an act job filter: %#v", legacyArgs)
	}
}

func TestRemoveStaleWorkspacesOnlyRemovesMatchingJob(t *testing.T) {
	workDir := t.TempDir()
	worker := &Worker{config: WorkerConfig{WorkDir: workDir}}
	stale := filepath.Join(workDir, "job-job-1-old")
	other := filepath.Join(workDir, "job-job-2-old")
	if err := os.MkdirAll(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "actions-secrets"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := worker.removeStaleWorkspaces("job-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale workspace remained: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("unrelated workspace was removed: %v", err)
	}
}

func assertArgumentValue(t *testing.T, arguments []string, flag string, want string) {
	t.Helper()
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == flag && arguments[index+1] == want {
			return
		}
	}
	t.Fatalf("act arguments did not contain %s %s: %#v", flag, want, arguments)
}

func countArgument(arguments []string, want string) int {
	count := 0
	for _, argument := range arguments {
		if argument == want {
			count++
		}
	}
	return count
}

func containsArgumentValue(arguments []string, flag string, want string) bool {
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == flag && arguments[index+1] == want {
			return true
		}
	}
	return false
}

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
