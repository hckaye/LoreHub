package lore

import (
	"testing"
	"time"
)

func TestValidFileLockPath(t *testing.T) {
	for _, value := range []string{
		"Content/Characters/Hero.uasset",
		"README.md",
		"日本語/背景.psd",
		"file with spaces.blend",
	} {
		if !ValidFileLockPath(value) {
			t.Errorf("valid file lock path %q was rejected", value)
		}
	}
	for _, value := range []string{
		"", "/absolute", "../outside", "folder/../file", "folder//file",
		"folder\\file", " trailing", "trailing ", "control\nfile",
	} {
		if ValidFileLockPath(value) {
			t.Errorf("invalid file lock path %q was accepted", value)
		}
	}
}

func TestCredentialOwnsFileLock(t *testing.T) {
	lock := FileLock{OwnerID: "user-a"}
	credential := Credential{Principal: UserPrincipal("user-a")}
	if !credentialOwnsFileLock(credential, lock) {
		t.Fatal("matching production user should own the lock")
	}
	credential.Principal = UserPrincipal("user-b")
	if credentialOwnsFileLock(credential, lock) {
		t.Fatal("different production user should not own the lock")
	}
	credential.InsecureDevelopment = true
	if !credentialOwnsFileLock(credential, lock) {
		t.Fatal("unauthenticated Lore integration should accept the server owner")
	}
}

func TestSameFileLockRequiresMatchingAcquisition(t *testing.T) {
	lockedAt := time.Date(2026, 8, 12, 1, 2, 3, 0, time.UTC)
	lock := FileLock{BranchID: "branch", Path: "asset.bin", OwnerID: "user-a", LockedAt: lockedAt}
	if !sameFileLock(lock, lock) {
		t.Fatal("identical lock observations should match")
	}
	changed := lock
	changed.LockedAt = lockedAt.Add(time.Millisecond)
	if sameFileLock(lock, changed) {
		t.Fatal("a reacquired lock should not match the stale observation")
	}
}

func TestValidateFileLockRequestRequiresUserForMutation(t *testing.T) {
	partition := "0123456789abcdef0123456789abcdef"
	repository := RepositoryRef{
		CacheKey: "repository", URL: "lore://localhost/" + partition,
		LoreRepositoryID: partition, DefaultBranch: "main",
	}
	readCredential := Credential{
		Partition: partition, Scope: ScopeRead, Identity: "reader",
		Principal: ServicePrincipal(ServicePurposePublicReader, "reader"), InsecureDevelopment: true,
	}
	if err := validateFileLockRequest(repository, "main", "", readCredential, ScopeRead, true); err != nil {
		t.Fatalf("valid read request failed: %v", err)
	}
	writeCredential := readCredential
	writeCredential.Scope = ScopeWrite
	if err := validateFileLockRequest(
		repository, "main", "Content/Hero.uasset", writeCredential, ScopeWrite, false,
	); err == nil {
		t.Fatal("service principal was allowed to mutate a file lock")
	}
	writeCredential.Identity = "user-id"
	writeCredential.Principal = UserPrincipal("user-id")
	if err := validateFileLockRequest(
		repository, "main", "Content/Hero.uasset", writeCredential, ScopeWrite, false,
	); err != nil {
		t.Fatalf("valid user mutation failed: %v", err)
	}
	writeCredential.Scope = ScopeAdmin
	if err := validateFileLockRequest(
		repository, "main", "Content/Hero.uasset", writeCredential, ScopeWrite, false,
	); err != nil {
		t.Fatalf("admin credential did not permit a write mutation: %v", err)
	}
}
