package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type recordingCredentialProvider struct {
	mu       sync.Mutex
	requests []loreclient.CredentialRequest
}

func (provider *recordingCredentialProvider) ForRepository(
	_ context.Context,
	request loreclient.CredentialRequest,
) (loreclient.Credential, error) {
	provider.mu.Lock()
	provider.requests = append(provider.requests, request)
	provider.mu.Unlock()
	return loreclient.Credential{
		Partition:           request.Repository.CanonicalPartition(),
		Scope:               request.Scope,
		Identity:            "fixture",
		Principal:           request.Principal,
		InsecureDevelopment: true,
	}, nil
}

func (provider *recordingCredentialProvider) lastRequest() loreclient.CredentialRequest {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return provider.requests[len(provider.requests)-1]
}

type codeTestLore struct {
	fakeLore
}

func (codeTestLore) Branches(
	context.Context,
	loreclient.RepositoryRef,
	loreclient.Credential,
) ([]loreclient.Branch, error) {
	return []loreclient.Branch{{Name: "main", LatestRevision: testRevision}}, nil
}

func (codeTestLore) Tree(
	context.Context,
	loreclient.RepositoryRef,
	string,
	string,
	loreclient.Credential,
	int,
) (loreclient.Tree, error) {
	return loreclient.Tree{Revision: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}, nil
}

func (codeTestLore) File(
	context.Context,
	loreclient.RepositoryRef,
	string,
	string,
	loreclient.Credential,
	int64,
) (loreclient.File, []byte, error) {
	return loreclient.File{}, nil, nil
}

func (codeTestLore) RevisionHistory(
	context.Context,
	loreclient.RepositoryRef,
	string,
	string,
	loreclient.Credential,
	int,
) ([]loreclient.RevisionHistoryEntry, error) {
	return nil, nil
}

func (codeTestLore) FileHistory(
	context.Context,
	loreclient.RepositoryRef,
	string,
	string,
	string,
	loreclient.Credential,
	int,
) ([]loreclient.FileHistoryEntry, error) {
	return nil, nil
}

func (codeTestLore) RevisionInfo(
	context.Context,
	loreclient.RepositoryRef,
	string,
	loreclient.Credential,
) (loreclient.Revision, error) {
	return loreclient.Revision{}, nil
}

func (codeTestLore) RevisionDiff(
	context.Context,
	loreclient.RepositoryRef,
	string,
	string,
	[]string,
	loreclient.Credential,
	int,
	int,
) (loreclient.Diff, error) {
	return loreclient.Diff{}, nil
}

type publicCodeStore struct {
	authCollabStore
}

func (store *publicCodeStore) LookupRepository(
	_ context.Context,
	actor *platform.User,
	owner, slug string,
) (collab.Repository, error) {
	return collab.Repository{
		ID: "public-repo", OrganizationID: "org-1", Owner: owner, Slug: slug,
		Visibility: "public", LoreRepositoryID: "0123456789abcdef0123456789abcdef",
		LoreURL: "lore://public", DefaultBranch: "main",
	}, nil
}

func newCodeCredentialHandler(
	store collab.Store,
	provider *recordingCredentialProvider,
	authenticationStore *fakeAuthenticationStore,
	codec *auth.SecretCodec,
) http.Handler {
	return New(
		fakeStore{user: platform.User{ID: "user-1", Username: "alice"}},
		codeTestLore{}, auth.DisabledAuthenticator{}, healthy{}, "",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithAuthentication(AuthOptions{SessionStore: authenticationStore, Secrets: codec}),
		WithCollaboration(store), WithLoreCredentials(provider), WithLoreServiceSubjects(loreclient.ServiceSubjects{
			PublicReader: "public-reader-subject",
		}),
	)
}

func TestCodeAPIUsesAuthenticatedUserPrincipal(t *testing.T) {
	codec, err := auth.NewSecretCodec("test-secret")
	if err != nil {
		t.Fatal(err)
	}
	authenticationStore := &fakeAuthenticationStore{}
	cookie, _ := prepareSessionCookie(t, authenticationStore, codec)
	provider := &recordingCredentialProvider{}
	handler := newCodeCredentialHandler(&authCollabStore{}, provider, authenticationStore, codec)
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/repositories/acme/private/tree?branch=main", nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("authenticated code read status=%d body=%s", response.Code, response.Body.String())
	}
	requestValue := provider.lastRequest()
	if requestValue.Principal != (loreclient.Principal{UserID: "user-1"}) ||
		requestValue.Repository.LoreRepositoryID != "0123456789abcdef0123456789abcdef" ||
		requestValue.Partition != "0123456789abcdef0123456789abcdef" ||
		requestValue.Scope != loreclient.ScopeRead {
		t.Fatal("code read did not use the authenticated user principal and canonical partition")
	}
}

func TestCodeAPIPublicAnonymousReadUsesPublicReaderPurpose(t *testing.T) {
	provider := &recordingCredentialProvider{}
	handler := newCodeCredentialHandler(&publicCodeStore{}, provider, &fakeAuthenticationStore{}, nil)
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/repositories/acme/public/tree?branch=main", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("anonymous public code read status=%d body=%s", response.Code, response.Body.String())
	}
	requestValue := provider.lastRequest()
	if requestValue.Principal != (loreclient.Principal{
		ServicePurpose: loreclient.ServicePurposePublicReader, Subject: "public-reader-subject",
	}) ||
		requestValue.Partition != "0123456789abcdef0123456789abcdef" {
		t.Fatal("anonymous public code read did not use the public-reader purpose and canonical partition")
	}
}
