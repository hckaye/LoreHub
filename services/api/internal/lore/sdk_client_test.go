package lore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

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
	repository := RepositoryRef{CacheKey: "repository-1", LoreRepositoryID: "partition-1"}
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

func TestSDKClientRewritesOnlyTheDataPlaneAuthority(t *testing.T) {
	client, err := NewSDKClientWithEndpoints(
		t.TempDir(),
		"auth.lorehub.example:8443",
		"lores://lore.lorehub.example:41337",
	)
	if err != nil {
		t.Fatal(err)
	}
	transportURL, err := client.transportRepositoryURL(
		"lores://lorehub.example:41341/0123456789abcdef0123456789abcdef",
	)
	if err != nil {
		t.Fatal(err)
	}
	if transportURL != "lores://lore.lorehub.example:41337/0123456789abcdef0123456789abcdef" {
		t.Fatalf("unexpected Lore transport URL: %q", transportURL)
	}
	if _, err := NewSDKClientWithEndpoints(t.TempDir(), "auth.lorehub.example:8443", "https://lore"); err == nil {
		t.Fatal("non-Lore data-plane origin was accepted")
	}
}

func TestCredentialCachePathChangesWithTheDataPlaneOrigin(t *testing.T) {
	repository := RepositoryRef{
		CacheKey:         "repository-1",
		URL:              "lores://lorehub.example:41337/0123456789abcdef0123456789abcdef",
		LoreRepositoryID: "0123456789abcdef0123456789abcdef",
	}
	credential := Credential{Principal: UserPrincipal("user-a")}
	publicClient, err := NewSDKClient(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	internalClient, err := NewSDKClientWithEndpoints(
		t.TempDir(), "auth.lorehub.example:8443", "lores://lore.lorehub.example:41337",
	)
	if err != nil {
		t.Fatal(err)
	}
	publicPath, err := publicClient.credentialCachePath(repository, credential)
	if err != nil {
		t.Fatal(err)
	}
	internalPath, err := internalClient.credentialCachePath(repository, credential)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(filepath.Dir(filepath.Dir(publicPath))) ==
		filepath.Base(filepath.Dir(filepath.Dir(internalPath))) {
		t.Fatalf("public and internal endpoints shared a Lore cache: %q %q", publicPath, internalPath)
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
