package runner

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
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
	credential, err := issueLoreCredential(
		context.Background(), issuer, principal, "repository-a", "lore://repository-a",
	)
	if err != nil || credential.Identity != "short-lived-identity" {
		t.Fatalf("unexpected issued credential: %#v, %v", credential, err)
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

func TestCredentialIssuerAcceptsControlPlaneTokenAndAuthURLForms(t *testing.T) {
	issuer := &recordingCredentialIssuer{credential: LoreCredential{
		RepositoryID: "repository-a",
		Scope:        ReadLoreScope,
		Token:        "token",
		AuthURL:      "ucs-auth://auth.example",
		ExpiresAt:    time.Now().UTC().Add(time.Minute),
	}}
	credential, err := issueLoreCredential(
		context.Background(), issuer,
		CredentialPrincipal{Kind: "service", Subject: "runner-a"},
		"repository-a", "lore://repository-a",
	)
	if err != nil || credential.Token != "token" || credential.AuthURL == "" {
		t.Fatalf("control-plane credential forms were not preserved: %#v, %v", credential, err)
	}
}

type recordingCredentialRevisionClient struct {
	credential loreclient.Credential
	revision   string
}

func (client *recordingCredentialRevisionClient) CloneRevision(
	context.Context,
	loreclient.RepositoryRef,
	string,
	string,
	string,
) error {
	return errors.New("legacy Lore credential path was used")
}

func (client *recordingCredentialRevisionClient) CloneRevisionWithCredential(
	_ context.Context,
	_ loreclient.RepositoryRef,
	credential loreclient.Credential,
	revision string,
	destination string,
) error {
	client.credential = credential
	client.revision = revision
	return os.MkdirAll(destination, 0o750)
}

func TestWorkerPassesTokenAndAuthURLToCredentialAwareLoreClient(t *testing.T) {
	issuer := &recordingCredentialIssuer{credential: LoreCredential{
		RepositoryID: "repository-a", Scope: ReadLoreScope, Token: "short-token",
		AuthURL: "https://auth.example/token", ExpiresAt: time.Now().UTC().Add(time.Minute),
	}}
	lore := &recordingCredentialRevisionClient{}
	worker := &Worker{config: WorkerConfig{
		CredentialIssuer:    issuer,
		CredentialPrincipal: CredentialPrincipal{Kind: "service", Subject: "runner"},
		RevisionClient:      lore,
	}}
	destination := t.TempDir() + "/workspace"
	err := worker.cloneRevision(context.Background(), Job{
		RepositoryID: "repository-a", LoreURL: "lore://repository-a", Revision: "revision-a",
	}, destination, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if lore.credential.Token != "short-token" || lore.credential.AuthURL != "https://auth.example/token" ||
		lore.revision != "revision-a" {
		t.Fatalf("credential-aware Lore contract was not preserved: %#v revision=%q", lore.credential, lore.revision)
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
