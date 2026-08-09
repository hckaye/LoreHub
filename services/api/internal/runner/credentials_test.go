package runner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileCredentialProviderPartitionsByRepositoryAndScope(t *testing.T) {
	root := t.TempDir()
	repositoryID := "repository-a"
	credentialPath := filepath.Join(root, repositoryID, ReadLoreScope)
	if err := os.MkdirAll(filepath.Dir(credentialPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentialPath, []byte("identity-a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := NewFileCredentialProvider(root)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := provider.Read(context.Background(), CredentialSubject{
		RepositoryID: repositoryID,
		LoreURL:      "lore://repository-a",
	}, ReadLoreScope)
	if err != nil || identity != "identity-a" {
		t.Fatalf("unexpected repository credential: %q, %v", identity, err)
	}
	if _, err := provider.Read(context.Background(), CredentialSubject{
		RepositoryID: "repository-b",
		LoreURL:      "lore://repository-b",
	}, ReadLoreScope); err == nil {
		t.Fatal("credential provider crossed repository partitions")
	}
	if _, err := provider.Read(context.Background(), CredentialSubject{
		RepositoryID: repositoryID,
		LoreURL:      "lore://repository-a",
	}, "write"); err == nil {
		t.Fatal("credential provider accepted an unsupported scope")
	}
}

func TestFileCredentialProviderRequiresAnExistingDirectory(t *testing.T) {
	if _, err := NewFileCredentialProvider(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing Lore credential directory was accepted")
	}
}

func TestFileCredentialProviderRejectsSymlinkedCredential(t *testing.T) {
	root := t.TempDir()
	partition := filepath.Join(root, "repository-a")
	if err := os.MkdirAll(partition, 0o750); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "identity")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(partition, ReadLoreScope)); err != nil {
		t.Fatal(err)
	}
	provider, err := NewFileCredentialProvider(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Read(context.Background(), CredentialSubject{
		RepositoryID: "repository-a",
		LoreURL:      "lore://repository-a",
	}, ReadLoreScope); err == nil {
		t.Fatal("symlinked credential was accepted")
	}
}

func TestNormalizePageRejectsOffsetOverflow(t *testing.T) {
	if _, _, err := normalizePage(PageRequest{Page: int64(^uint64(0) >> 1), PerPage: 100}); err != ErrActionInvalid {
		t.Fatalf("offset overflow was not rejected: %v", err)
	}
	page, offset, err := normalizePage(PageRequest{})
	if err != nil || page.Page != 1 || page.PerPage != 30 || offset != 0 {
		t.Fatalf("unexpected default page: %#v offset=%d error=%v", page, offset, err)
	}
}
