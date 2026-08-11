package lore

import (
	"errors"
	"os"
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
