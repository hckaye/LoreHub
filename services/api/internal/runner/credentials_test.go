package runner

import (
	"context"
	"strings"
	"testing"
	"time"
)

type recordingCredentialIssuer struct {
	request    CredentialRequest
	credential LoreCredential
}

func (issuer *recordingCredentialIssuer) Issue(
	_ context.Context,
	request CredentialRequest,
) (LoreCredential, error) {
	issuer.request = request
	return issuer.credential, nil
}

func TestCredentialIssuerContractCarriesExactResourceAndScope(t *testing.T) {
	issuer := &recordingCredentialIssuer{credential: LoreCredential{
		RepositoryID: "repository-a",
		Scope:        ReadLoreScope,
		Identity:     "short-lived-identity",
		ExpiresAt:    time.Now().UTC().Add(time.Minute),
	}}
	principal := CredentialPrincipal{Kind: "service", Subject: "runner-a"}
	identity, err := issueLoreIdentity(
		context.Background(), issuer, principal, "repository-a", "lore://repository-a",
	)
	if err != nil || identity != "short-lived-identity" {
		t.Fatalf("unexpected issued identity: %q, %v", identity, err)
	}
	if issuer.request.Principal != principal || issuer.request.RepositoryID != "repository-a" ||
		issuer.request.LoreURL != "lore://repository-a" || issuer.request.Scope != ReadLoreScope {
		t.Fatalf("issuer request did not preserve the exact contract: %#v", issuer.request)
	}
}

func TestCredentialIssuerRejectsWrongPartitionScopeOrExpiry(t *testing.T) {
	tests := []struct {
		name       string
		credential LoreCredential
	}{
		{
			name: "wrong repository",
			credential: LoreCredential{
				RepositoryID: "repository-b", Scope: ReadLoreScope, Identity: "identity",
				ExpiresAt: time.Now().Add(time.Minute),
			},
		},
		{
			name: "wrong scope",
			credential: LoreCredential{
				RepositoryID: "repository-a", Scope: "write", Identity: "identity",
				ExpiresAt: time.Now().Add(time.Minute),
			},
		},
		{
			name: "expired",
			credential: LoreCredential{
				RepositoryID: "repository-a", Scope: ReadLoreScope, Identity: "identity",
				ExpiresAt: time.Now().Add(-time.Minute),
			},
		},
		{
			name: "too long",
			credential: LoreCredential{
				RepositoryID: "repository-a", Scope: ReadLoreScope, Identity: "identity",
				ExpiresAt: time.Now().Add(16 * time.Minute),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			issuer := &recordingCredentialIssuer{credential: test.credential}
			_, err := issueLoreCredential(
				context.Background(), issuer,
				CredentialPrincipal{Kind: "service", Subject: "runner-a"},
				"repository-a", "lore://repository-a",
			)
			if err == nil {
				t.Fatal("invalid credential was accepted")
			}
		})
	}
}

func TestCredentialIssuerRequiresUsableCurrentLoreMaterial(t *testing.T) {
	issuer := &recordingCredentialIssuer{credential: LoreCredential{
		RepositoryID: "repository-a",
		Scope:        ReadLoreScope,
		Token:        "token",
		ExpiresAt:    time.Now().UTC().Add(time.Minute),
	}}
	_, err := issueLoreIdentity(
		context.Background(), issuer,
		CredentialPrincipal{Kind: "service", Subject: "runner-a"},
		"repository-a", "lore://repository-a",
	)
	if err == nil || !strings.Contains(err.Error(), "requires an identity") {
		t.Fatalf("token-only credential was not rejected by the current Lore adapter: %v", err)
	}
}

func TestFailClosedCredentialIssuerCannotIssue(t *testing.T) {
	_, err := issueLoreCredential(
		context.Background(), NewFailClosedCredentialIssuer(),
		CredentialPrincipal{Kind: "service", Subject: "runner-a"},
		"repository-a", "lore://repository-a",
	)
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("fail-closed issuer returned %v", err)
	}
}

func TestDevelopmentCredentialIssuerIsExplicitAndShortLived(t *testing.T) {
	before := time.Now().UTC()
	credential, err := NewDevelopmentCredentialIssuer("development-only").Issue(
		context.Background(), CredentialRequest{
			Principal:    CredentialPrincipal{Kind: "service", Subject: "test"},
			RepositoryID: "repository-a",
			LoreURL:      "lore://repository-a",
			Scope:        ReadLoreScope,
		},
	)
	if err != nil || credential.Identity != "development-only" || !credential.ExpiresAt.After(before) {
		t.Fatalf("unexpected development credential: %#v, %v", credential, err)
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
