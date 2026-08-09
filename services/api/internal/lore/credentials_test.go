package lore

import (
	"context"
	"errors"
	"testing"
)

func TestCredentialProviderPartitionsReadAndWriteScopes(t *testing.T) {
	provider, err := NewCredentialProvider("production", map[string]string{"repo-1": "svc-1"}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	ref := RepositoryRef{LoreRepositoryID: "repo-1"}
	read, err := provider.ForRepository(context.Background(), ref, ScopeRead)
	if err != nil {
		t.Fatal(err)
	}
	if read.Partition != ref.LoreRepositoryID || read.Identity != "svc-1" || read.Scope != ScopeRead {
		t.Fatalf("read credential = %#v", read)
	}
	write, err := provider.ForRepository(context.Background(), ref, ScopeWrite)
	if err != nil {
		t.Fatal(err)
	}
	if write.Identity != read.Identity || write.Scope != ScopeWrite {
		t.Fatalf("write credential = %#v", write)
	}
	unknown := RepositoryRef{LoreRepositoryID: "repo-2"}
	if _, err := provider.ForRepository(context.Background(), unknown, ScopeRead); !errors.Is(err,
		ErrCredentialUnavailable) {
		t.Fatalf("unknown partition error = %v, want ErrCredentialUnavailable", err)
	}
}

func TestCredentialProviderRejectsDevelopmentFallbackOutsideDevelopment(t *testing.T) {
	if _, err := NewCredentialProvider("production", nil, "fixture", true); err == nil {
		t.Fatal("production accepted development fallback")
	}
	provider := NewDevelopmentCredentialProvider("fixture")
	credential, err := provider.ForRepository(context.Background(), RepositoryRef{LoreRepositoryID: "repo-1"}, ScopeWrite)
	if err != nil || credential.Identity != "fixture" || credential.Scope != ScopeWrite {
		t.Fatalf("development credential = %#v, %v", credential, err)
	}
}
