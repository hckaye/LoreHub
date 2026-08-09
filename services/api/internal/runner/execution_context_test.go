package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recordingExecutionResolver struct {
	request ExecutionContextRequest
	value   ExecutionContext
	err     error
}

func (resolver *recordingExecutionResolver) Resolve(
	_ context.Context,
	request ExecutionContextRequest,
) (ExecutionContext, error) {
	resolver.request = request
	if resolver.err != nil {
		return ExecutionContext{}, resolver.err
	}
	return resolver.value, nil
}

func TestResolveExecutionContextUsesExactScopeAndPrecedence(t *testing.T) {
	resolver := &recordingExecutionResolver{value: ExecutionContext{
		OrganizationVariables: map[string]string{"SHARED": "organization", "ORG_ONLY": "yes"},
		RepositoryVariables:   map[string]string{"SHARED": "repository", "REPO_ONLY": "yes"},
		EnvironmentVariables:  map[string]string{"SHARED": "environment", "ENV_ONLY": "yes"},
		OrganizationSecrets:   map[string]string{"TOKEN": "organization-token"},
		RepositorySecrets:     map[string]string{"TOKEN": "repository-token"},
		EnvironmentSecrets:    map[string]string{"TOKEN": "environment-token"},
	}}
	request := ExecutionContextRequest{
		Principal:      CredentialPrincipal{Kind: "service", Subject: "runner"},
		RepositoryID:   "repository-a",
		OrganizationID: "organization-a",
		Environment:    "production",
		RequestedScope: "actions:execute",
	}
	resolved, err := resolveExecutionContext(context.Background(), resolver, request)
	if err != nil {
		t.Fatal(err)
	}
	if resolver.request != request {
		t.Fatalf("resolver did not receive exact request: %#v", resolver.request)
	}
	if resolved.Variables["SHARED"] != "environment" || resolved.Secrets["TOKEN"] != "environment-token" {
		t.Fatalf("scope precedence was incorrect: %#v %#v", resolved.Variables, resolved.Secrets)
	}
	if resolved.Variables["ORG_ONLY"] != "yes" || resolved.Variables["REPO_ONLY"] != "yes" ||
		resolved.Variables["ENV_ONLY"] != "yes" {
		t.Fatalf("scoped values were lost: %#v", resolved.Variables)
	}
}

func TestResolveExecutionContextFailsClosed(t *testing.T) {
	resolver := &recordingExecutionResolver{err: errors.New("control plane unavailable")}
	_, err := resolveExecutionContext(context.Background(), resolver, ExecutionContextRequest{
		Principal:      CredentialPrincipal{Kind: "service", Subject: "runner"},
		RepositoryID:   "repository-a",
		OrganizationID: "organization-a",
		RequestedScope: "actions:execute",
	})
	if err == nil || !strings.Contains(err.Error(), "control plane unavailable") {
		t.Fatalf("resolver error did not fail closed: %v", err)
	}
	_, err = resolveExecutionContext(context.Background(), nil, ExecutionContextRequest{})
	if err == nil {
		t.Fatal("nil execution resolver was accepted")
	}
}

func TestExecutionContextRejectsReservedAndUnsafeValues(t *testing.T) {
	resolver := &recordingExecutionResolver{value: ExecutionContext{
		RepositoryVariables: map[string]string{"GITHUB_SHA": "override"},
	}}
	_, err := resolveExecutionContext(context.Background(), resolver, ExecutionContextRequest{
		Principal:      CredentialPrincipal{Kind: "service", Subject: "runner"},
		RepositoryID:   "repository-a",
		OrganizationID: "organization-a",
		RequestedScope: "actions:execute",
	})
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("reserved variable was accepted: %v", err)
	}
	resolver.value = ExecutionContext{
		RepositorySecrets: map[string]string{"GITHUB_TOKEN": "user-override"},
	}
	_, err = resolveExecutionContext(context.Background(), resolver, ExecutionContextRequest{
		Principal:      CredentialPrincipal{Kind: "service", Subject: "runner"},
		RepositoryID:   "repository-a",
		OrganizationID: "organization-a",
		RequestedScope: "actions:execute",
	})
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("reserved GITHUB_TOKEN secret was accepted: %v", err)
	}
	resolver.value = ExecutionContext{
		RepositorySecrets: map[string]string{"TOKEN": "line\nvalue"},
	}
	_, err = resolveExecutionContext(context.Background(), resolver, ExecutionContextRequest{
		Principal:      CredentialPrincipal{Kind: "service", Subject: "runner"},
		RepositoryID:   "repository-a",
		OrganizationID: "organization-a",
		RequestedScope: "actions:execute",
	})
	if err != nil {
		t.Fatalf("multiline secret was rejected: %v", err)
	}
}

func TestSecretFileIsRestrictedAndRemoved(t *testing.T) {
	directory := t.TempDir()
	path, err := writeSecretFile(directory, map[string]string{"ZED": "last", "ALPHA": "first"})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("secret file mode was %o", info.Mode().Perm())
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "ALPHA=\"first\"\nZED=\"last\"\n" {
		t.Fatalf("secret file contents were not deterministic: %q", contents)
	}
	cleanupSecretFile(path)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("secret file remained after cleanup: %v", err)
	}
	if _, err := writeSecretFile(filepath.Join(directory, "missing"), map[string]string{"TOKEN": "value"}); err == nil {
		t.Fatal("secret file creation unexpectedly succeeded in a missing directory")
	}
}

func TestSecretFileUsesActQuotedMultilineFormat(t *testing.T) {
	path, err := writeSecretFile(t.TempDir(), map[string]string{
		"MULTILINE": "first\r\nsecond\n$literal\\path\"quote",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupSecretFile(path)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "MULTILINE=\"first\\r\\nsecond\\n\\$literal\\\\path\\\"quote\"\n"
	if string(contents) != want {
		t.Fatalf("multiline secret did not use act format: %q want %q", contents, want)
	}
}

func TestGitHubContextRequiresProductionHTTPSAndOneOrigin(t *testing.T) {
	local := GitHubContext{
		ServerURL:  "http://localhost:3000",
		APIURL:     "http://localhost:3000/api/v1",
		GraphQLURL: "http://localhost:3000/api/graphql",
	}
	if err := validateGitHubContext(local, "development"); err != nil {
		t.Fatal(err)
	}
	if err := validateGitHubContext(local, "production"); err == nil {
		t.Fatal("HTTP GitHub context was accepted in production")
	}
	local.APIURL = "http://api.example.test/api/v1"
	if err := validateGitHubContext(local, "development"); err == nil {
		t.Fatal("GitHub context with a different API origin was accepted")
	}
}
