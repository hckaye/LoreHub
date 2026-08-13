package lore

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type serverResolverFunc func(context.Context, string) (ServerTransport, error)

func (resolve serverResolverFunc) ResolveTransport(
	ctx context.Context,
	repositoryURL string,
) (ServerTransport, error) {
	return resolve(ctx, repositoryURL)
}

func loreTestTempDir(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp("", "lorehub-sdk-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		deadline := time.Now().Add(5 * time.Second)
		for {
			removeErr := os.RemoveAll(directory)
			_, statErr := os.Stat(directory)
			if removeErr == nil && errors.Is(statErr, os.ErrNotExist) {
				return
			}
			if time.Now().After(deadline) {
				t.Errorf("Lore test cache cleanup failed: remove=%v stat=%v", removeErr, statErr)
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
	})
	return directory
}

func TestCredentialCachePathSeparatesUserAndServiceState(t *testing.T) {
	client, err := NewSDKClient(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repository, err := client.transportRepositoryRef(t.Context(), RepositoryRef{
		CacheKey: "repository-1", URL: "lores://lore.example/partition-1", LoreRepositoryID: "partition-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	userA := Credential{Principal: UserPrincipal("user-a")}
	userB := Credential{Principal: UserPrincipal("user-b")}
	service := Credential{Principal: ServicePrincipal(ServicePurposePublicReader, "public-reader-subject")}
	serviceSamePurpose := Credential{Principal: ServicePrincipal(ServicePurposePublicReader, "other-subject")}
	serviceOtherPurpose := Credential{Principal: ServicePrincipal(ServicePurposeActionsRunner, "actions-subject")}
	pathA, err := client.credentialCachePath(repository, userA)
	if err != nil {
		t.Fatal(err)
	}
	pathB, err := client.credentialCachePath(repository, userB)
	if err != nil {
		t.Fatal(err)
	}
	servicePath, err := client.credentialCachePath(repository, service)
	if err != nil {
		t.Fatal(err)
	}
	serviceSamePurposePath, err := client.credentialCachePath(repository, serviceSamePurpose)
	if err != nil {
		t.Fatal(err)
	}
	serviceOtherPurposePath, err := client.credentialCachePath(repository, serviceOtherPurpose)
	if err != nil {
		t.Fatal(err)
	}
	if pathA == pathB || pathA == servicePath || pathB == servicePath || servicePath == serviceSamePurposePath ||
		servicePath == serviceOtherPurposePath {
		t.Fatalf("credential cache paths are not isolated: %q %q %q %q %q", pathA, pathB, servicePath,
			serviceSamePurposePath, serviceOtherPurposePath)
	}
	for _, value := range []string{pathA, pathB, servicePath, serviceSamePurposePath, serviceOtherPurposePath} {
		if strings.Contains(value, "short-lived-token") || strings.Contains(value, "auth.example") {
			t.Fatalf("credential cache path contains auth material: %q", value)
		}
	}
}

func TestProductionSDKRejectsInsecureDevelopmentCredential(t *testing.T) {
	client, err := NewSDKClient(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	credential := Credential{
		Partition:           "partition-1",
		Scope:               ScopeRead,
		Identity:            "fixture",
		Principal:           UserPrincipal("user-a"),
		InsecureDevelopment: true,
	}
	if err := client.authenticate(t.Context(), "", "lore://lore.example/partition-1", credential); err == nil {
		t.Fatal("production SDK accepted an insecure development credential")
	}
}

func TestSDKClientResolvesPerServerTransport(t *testing.T) {
	resolver := serverResolverFunc(func(_ context.Context, repositoryURL string) (ServerTransport, error) {
		parsed, err := url.Parse(repositoryURL)
		if err != nil {
			return ServerTransport{}, err
		}
		switch strings.ToLower(parsed.Host) {
		case "public.lorehub.example:41337":
			return ServerTransport{
				Authority: "lores://internal.lorehub.example:41337", ServerID: "instance-server",
			}, nil
		case "tenant.example:41337":
			return ServerTransport{
				Authority: "lores://tenant.example:41337", ServerID: "registered-server",
			}, nil
		default:
			return ServerTransport{}, &UnknownServerAuthorityError{Authority: parsed.Host}
		}
	})
	client, err := NewSDKClientWithServerResolver(
		t.TempDir(),
		"auth.lorehub.example:8443",
		resolver,
	)
	if err != nil {
		t.Fatal(err)
	}
	instanceURL, err := client.transportRepositoryURL(
		t.Context(), "lores://public.lorehub.example:41337/0123456789abcdef0123456789abcdef",
	)
	if err != nil {
		t.Fatal(err)
	}
	if instanceURL != "lores://internal.lorehub.example:41337/0123456789abcdef0123456789abcdef" {
		t.Fatalf("unexpected instance Lore transport URL: %q", instanceURL)
	}
	registeredURL, err := client.transportRepositoryURL(
		t.Context(), "lores://tenant.example:41337/0123456789abcdef0123456789abcdef",
	)
	if err != nil {
		t.Fatal(err)
	}
	if registeredURL != "lores://tenant.example:41337/0123456789abcdef0123456789abcdef" {
		t.Fatalf("unexpected registered Lore transport URL: %q", registeredURL)
	}
	_, err = client.transportRepositoryURL(
		t.Context(), "lores://unknown.example:41337/0123456789abcdef0123456789abcdef",
	)
	var unknownAuthority *UnknownServerAuthorityError
	if !errors.Is(err, ErrUnknownServerAuthority) || !errors.As(err, &unknownAuthority) ||
		unknownAuthority.Authority != "unknown.example:41337" {
		t.Fatalf("unknown Lore authority error = %v", err)
	}
}

func TestCredentialCachePathSeparatesServerAndTransportAuthority(t *testing.T) {
	resolver := serverResolverFunc(func(_ context.Context, repositoryURL string) (ServerTransport, error) {
		parsed, err := url.Parse(repositoryURL)
		if err != nil {
			return ServerTransport{}, err
		}
		switch parsed.Host {
		case "server-a.example:41337":
			return ServerTransport{Authority: "lores://SHARED.example:41337", ServerID: "server-a"}, nil
		case "server-b.example:41337":
			return ServerTransport{Authority: "lores://shared.example:41337", ServerID: "server-b"}, nil
		default:
			return ServerTransport{Authority: "lores://other.example:41337", ServerID: "server-a"}, nil
		}
	})
	credential := Credential{Principal: UserPrincipal("user-a")}
	client, err := NewSDKClientWithServerResolver(t.TempDir(), "auth.example:8443", resolver)
	if err != nil {
		t.Fatal(err)
	}
	resolved := make([]RepositoryRef, 0, 3)
	for _, authority := range []string{
		"server-a.example:41337", "server-b.example:41337", "server-c.example:41337",
	} {
		repository, resolveErr := client.transportRepositoryRef(t.Context(), RepositoryRef{
			CacheKey: "repository-1",
			URL:      "lores://" + authority + "/0123456789abcdef0123456789abcdef",
		})
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		resolved = append(resolved, repository)
	}
	paths := make([]string, 0, len(resolved))
	for _, repository := range resolved {
		path, pathErr := client.credentialCachePath(repository, credential)
		if pathErr != nil {
			t.Fatal(pathErr)
		}
		paths = append(paths, path)
	}
	endpointKey := func(path string) string {
		return filepath.Base(filepath.Dir(filepath.Dir(path)))
	}
	if endpointKey(paths[0]) == endpointKey(paths[1]) || endpointKey(paths[0]) == endpointKey(paths[2]) {
		t.Fatalf("Lore server transports shared a credential cache: %q", paths)
	}
	operationPaths := make([]string, 0, len(resolved))
	for _, repository := range resolved {
		path, pathErr := client.operationPath(repository, "merge-1")
		if pathErr != nil {
			t.Fatal(pathErr)
		}
		operationPaths = append(operationPaths, path)
	}
	if operationPaths[0] == operationPaths[1] || operationPaths[0] == operationPaths[2] {
		t.Fatalf("Lore server transports shared a merge workspace: %q", operationPaths)
	}
	if resolved[0].transportAuthority != "lores://shared.example:41337" {
		t.Fatalf("transport authority was not normalized: %q", resolved[0].transportAuthority)
	}
}

func TestSDKClientValidatesDeletionBinaryAndStructuredErrors(t *testing.T) {
	client, err := NewSDKClient(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, binary := range []string{"relative/path/lore", "lore command", "lore\ncommand"} {
		if err := client.ConfigureBinary(binary); err == nil {
			t.Fatalf("invalid Lore binary was accepted: %q", binary)
		}
	}
	if err := client.ConfigureBinary("/usr/local/bin/lore"); err != nil {
		t.Fatalf("absolute Lore binary path was rejected: %v", err)
	}
	if !loreRepositoryNotFound("[Error] Not found\n  at lore-revision/src/repository/delete.rs:21") {
		t.Fatal("exact Lore not-found result was not recognized")
	}
	for _, output := range []string{
		"[Error] Host not found",
		"[Error] Failed to connect: repository not found while resolving host",
		"Repository not found",
	} {
		if loreRepositoryNotFound(output) {
			t.Fatalf("ambiguous Lore deletion output was accepted: %q", output)
		}
	}
	if !loreCommandReportedError("[Info] start\n[Error] denied") {
		t.Fatal("Lore error event was not recognized")
	}
}

func TestObliterateCredentialDoesNotWidenToAdmin(t *testing.T) {
	repository := RepositoryRef{LoreRepositoryID: "0123456789abcdef0123456789abcdef"}
	credential := Credential{
		Partition:           repository.LoreRepositoryID,
		Scope:               ScopeObliterate,
		Identity:            "repository-lifecycle-subject",
		Principal:           ServicePrincipal(ServicePurposeRepositoryLifecycle, "repository-lifecycle-subject"),
		InsecureDevelopment: true,
	}
	if err := ValidateCredential(repository, credential, ScopeObliterate); err != nil {
		t.Fatalf("obliterate credential was rejected: %v", err)
	}
	if err := ValidateCredential(repository, credential, ScopeAdmin); err == nil {
		t.Fatal("obliterate credential widened to repository admin")
	}
	credential.Scope = ScopeAdmin
	if err := ValidateCredential(repository, credential, ScopeObliterate); err == nil {
		t.Fatal("repository admin credential widened to obliterate")
	}
}
