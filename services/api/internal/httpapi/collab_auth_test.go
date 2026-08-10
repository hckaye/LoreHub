package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type authCollabStore struct {
	updateCalls int
}

func (store *authCollabStore) EnsureUser(context.Context, auth.Principal) (platform.User, error) {
	return platform.User{ID: "user-1", Username: "alice"}, nil
}

func (store *authCollabStore) LookupRepository(
	_ context.Context,
	actor *platform.User,
	owner, slug string,
) (collab.Repository, error) {
	if actor == nil {
		return collab.Repository{}, platform.ErrNotFound
	}
	return collab.Repository{
		ID: "private-repo", OrganizationID: "org-1", Owner: owner, Slug: slug,
		Visibility: "private", LoreRepositoryID: "0123456789abcdef0123456789abcdef",
		LoreURL: "lore://lore.example/0123456789abcdef0123456789abcdef", DefaultBranch: "main",
	}, nil
}

func (*authCollabStore) RepositoryPermission(
	context.Context, platform.User, collab.Repository,
) (collab.Access, error) {
	return collab.Access{Permission: collab.PermWrite}, nil
}

func (*authCollabStore) GetIssue(context.Context, string, int64) (collab.Issue, error) {
	return collab.Issue{ID: "issue-1", Number: 1, Title: "Issue", State: "open"}, nil
}

func (store *authCollabStore) UpdateIssue(
	context.Context,
	platform.User,
	string,
	int64,
	collab.UpdateIssueInput,
) (collab.Issue, error) {
	store.updateCalls++
	return collab.Issue{ID: "issue-1", Number: 1, Title: "Updated", State: "open"}, nil
}

func (*authCollabStore) ListIssueComments(
	context.Context, string, int64, collab.Page,
) (collab.Result[collab.IssueComment], error) {
	return collab.Result[collab.IssueComment]{Items: []collab.IssueComment{}}, nil
}

func (*authCollabStore) CreateIssueComment(
	context.Context, platform.User, string, int64, string,
) (collab.IssueComment, error) {
	return collab.IssueComment{}, platform.ErrNotFound
}

func (*authCollabStore) UpdateIssueComment(
	context.Context, platform.User, string, int64, string, string,
) (collab.IssueComment, error) {
	return collab.IssueComment{}, platform.ErrNotFound
}

func (*authCollabStore) DeleteIssueComment(
	context.Context, platform.User, string, int64, string,
) error {
	return platform.ErrNotFound
}

func (*authCollabStore) ListLabels(
	context.Context, string, collab.Page,
) (collab.Result[collab.Label], error) {
	return collab.Result[collab.Label]{Items: []collab.Label{}}, nil
}

func (*authCollabStore) CreateLabel(
	context.Context, platform.User, string, collab.LabelInput,
) (collab.Label, error) {
	return collab.Label{}, platform.ErrNotFound
}

func (*authCollabStore) UpdateLabel(
	context.Context, platform.User, string, string, collab.LabelInput,
) (collab.Label, error) {
	return collab.Label{}, platform.ErrNotFound
}

func (*authCollabStore) DeleteLabel(context.Context, platform.User, string, string) error {
	return platform.ErrNotFound
}

func (*authCollabStore) ApplyLabel(
	context.Context, platform.User, string, int64, string,
) (collab.Label, bool, error) {
	return collab.Label{}, false, platform.ErrNotFound
}

func (*authCollabStore) RemoveLabel(context.Context, platform.User, string, int64, string) error {
	return platform.ErrNotFound
}

func (*authCollabStore) GetMergeRequest(context.Context, string, int64) (collab.MergeRequest, error) {
	return collab.MergeRequest{}, platform.ErrNotFound
}

func (*authCollabStore) UpdateMergeRequest(
	context.Context, platform.User, string, int64, collab.UpdateMergeRequestInput,
) (collab.MergeRequest, error) {
	return collab.MergeRequest{}, platform.ErrNotFound
}

func (*authCollabStore) ListReviews(context.Context, string, int64) (collab.ReviewSummary, error) {
	return collab.ReviewSummary{}, platform.ErrNotFound
}

func (*authCollabStore) CreateReview(
	context.Context, platform.User, string, int64, collab.ReviewInput,
) (collab.Review, bool, error) {
	return collab.Review{}, false, platform.ErrNotFound
}

func (*authCollabStore) ListBranchRules(context.Context, string) ([]collab.BranchRule, error) {
	return []collab.BranchRule{}, nil
}

func (*authCollabStore) CreateBranchRule(
	context.Context, platform.User, string, collab.BranchRuleInput,
) (collab.BranchRule, error) {
	return collab.BranchRule{}, platform.ErrNotFound
}

func (*authCollabStore) UpdateBranchRule(
	context.Context, platform.User, string, string, collab.BranchRuleInput,
) (collab.BranchRule, error) {
	return collab.BranchRule{}, platform.ErrNotFound
}

func (*authCollabStore) DeleteBranchRule(context.Context, platform.User, string, string) error {
	return platform.ErrNotFound
}

func newCollabAuthTestHandler(
	authenticator auth.Authenticator,
	authenticationStore *fakeAuthenticationStore,
	codec *auth.SecretCodec,
	collabStore collab.Store,
) http.Handler {
	return New(
		fakeStore{user: platform.User{ID: "user-1", Username: "alice"}},
		fakeLore{}, authenticator, healthy{}, "",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithAuthentication(AuthOptions{
			SessionStore: authenticationStore,
			Secrets:      codec,
			PublicOrigin: "https://app.example",
			SessionCookie: SessionCookieOptions{
				Name: "lorehub_session", Path: "/",
			},
		}),
		WithDevelopmentLoreCredentials("fixture"),
		WithCollaboration(collabStore),
	)
}

func prepareSessionCookie(
	t *testing.T, store *fakeAuthenticationStore, codec *auth.SecretCodec,
) (*http.Cookie, string) {
	t.Helper()
	token := "valid-session-token"
	csrf := codec.CSRFToken(token)
	store.session = auth.Session{
		ID: "session-1", UserID: "user-1", Username: "alice",
		CSRFDigest: codec.Digest(csrf), ExpiresAt: time.Now().Add(time.Hour),
	}
	store.sessionTokenHash = codec.Digest(token)
	store.sessionValid = true
	return &http.Cookie{Name: "lorehub_session", Value: token, Path: "/"}, csrf
}

func TestCollaborationUsesCommonCookieSessionAndCSRF(t *testing.T) {
	t.Parallel()
	codec, err := auth.NewSecretCodec("test-secret")
	if err != nil {
		t.Fatal(err)
	}
	authenticationStore := &fakeAuthenticationStore{}
	collabStore := &authCollabStore{}
	handler := newCollabAuthTestHandler(
		auth.DisabledAuthenticator{}, authenticationStore, codec, collabStore,
	)
	cookie, csrf := prepareSessionCookie(t, authenticationStore, codec)
	target := "/api/v1/repositories/acme/private/issues/1"

	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("cookie-authenticated private read: status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPatch, target, strings.NewReader(`{"title":"new"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "csrf_failed") {
		t.Fatalf("missing CSRF: status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPatch, target, strings.NewReader(`{"title":"new"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "wrong")
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "csrf_failed") {
		t.Fatalf("wrong CSRF: status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPatch, target, strings.NewReader(`{"title":"new"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || collabStore.updateCalls != 1 {
		t.Fatalf("valid CSRF mutation: status=%d calls=%d body=%s",
			response.Code, collabStore.updateCalls, response.Body.String())
	}
}

func TestCollaborationPrivateReadAnonymousAndBearerCompatible(t *testing.T) {
	t.Parallel()
	codec, err := auth.NewSecretCodec("test-secret")
	if err != nil {
		t.Fatal(err)
	}
	authenticationStore := &fakeAuthenticationStore{}
	collabStore := &authCollabStore{}
	principal := auth.Principal{Issuer: "test", Subject: "alice", Username: "alice"}
	handler := newCollabAuthTestHandler(
		staticAuthenticator{principal: principal}, authenticationStore, codec, collabStore,
	)
	target := "/api/v1/repositories/acme/private/issues/1"

	request := httptest.NewRequest(http.MethodGet, target, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("anonymous private read leaked: status=%d body=%s", response.Code, response.Body.String())
	}

	expiredCookie := &http.Cookie{Name: "lorehub_session", Value: "expired-session"}
	authenticationStore.sessionTokenHash = codec.Digest(expiredCookie.Value)
	authenticationStore.session.ExpiresAt = time.Now().Add(-time.Minute)
	authenticationStore.sessionValid = true
	request = httptest.NewRequest(http.MethodGet, target, nil)
	request.AddCookie(expiredCookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expired cookie was not rejected: status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPatch, target, strings.NewReader(`{"title":"new"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer token")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("bearer mutation compatibility: status=%d body=%s", response.Code, response.Body.String())
	}
}

type authCodeLore struct{ fakeLore }

func (authCodeLore) Tree(
	context.Context, loreclient.RepositoryRef, string, string, loreclient.Credential, int,
) (loreclient.Tree, error) {
	return loreclient.Tree{Revision: testRevision, Entries: []loreclient.TreeEntry{{
		Name: "README.md", Path: "README.md", Kind: "file", Size: 12,
	}}}, nil
}

func (authCodeLore) File(
	context.Context, loreclient.RepositoryRef, string, string, loreclient.Credential, int64,
) (loreclient.File, []byte, error) {
	body := []byte("# private\n")
	return loreclient.File{Path: "README.md", Revision: testRevision, Kind: "file", Size: uint64(len(body))}, body, nil
}

func (authCodeLore) RevisionHistory(
	context.Context, loreclient.RepositoryRef, string, string, loreclient.Credential, int,
) ([]loreclient.RevisionHistoryEntry, error) {
	return []loreclient.RevisionHistoryEntry{{Revision: testRevision}}, nil
}

func (authCodeLore) FileHistory(
	context.Context, loreclient.RepositoryRef, string, string, string, loreclient.Credential, int,
) ([]loreclient.FileHistoryEntry, error) {
	return []loreclient.FileHistoryEntry{{Path: "README.md", Revision: testRevision}}, nil
}

func (authCodeLore) RevisionInfo(
	context.Context, loreclient.RepositoryRef, string, loreclient.Credential,
) (loreclient.Revision, error) {
	return loreclient.Revision{Revision: testRevision, Number: 1}, nil
}

func (authCodeLore) RevisionDiff(
	context.Context, loreclient.RepositoryRef, string, string, []string, loreclient.Credential, int, int,
) (loreclient.Diff, error) {
	return loreclient.Diff{Source: testRevision, Target: testRevision}, nil
}

const testRevision = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestCodeBrowserUsesPrivateSessionAndKeepsAnonymousNotFound(t *testing.T) {
	t.Parallel()
	codec, err := auth.NewSecretCodec("test-secret")
	if err != nil {
		t.Fatal(err)
	}
	authenticationStore := &fakeAuthenticationStore{}
	collabStore := &authCollabStore{}
	handler := New(
		fakeStore{user: platform.User{ID: "user-1", Username: "alice"}}, authCodeLore{},
		auth.DisabledAuthenticator{}, healthy{}, "",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithAuthentication(AuthOptions{
			SessionStore: authenticationStore, Secrets: codec, PublicOrigin: "https://app.example",
			SessionCookie: SessionCookieOptions{Name: "lorehub_session", Path: "/"},
		}),
		WithDevelopmentLoreCredentials("fixture"),
		WithCollaboration(collabStore),
	)
	target := "/api/v1/repositories/acme/private/tree?revision=" + testRevision
	request := httptest.NewRequest(http.MethodGet, target, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("anonymous private code read leaked: status=%d body=%s", response.Code, response.Body.String())
	}

	cookie, _ := prepareSessionCookie(t, authenticationStore, codec)
	request = httptest.NewRequest(http.MethodGet, target, nil)
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("cookie private code read: status=%d content-type=%q body=%s", response.Code,
			response.Header().Get("Content-Type"), response.Body.String())
	}
}

func TestCodeBrowserBearerReadRemainsCompatible(t *testing.T) {
	codec, err := auth.NewSecretCodec("test-secret")
	if err != nil {
		t.Fatal(err)
	}
	handler := New(
		fakeStore{user: platform.User{ID: "user-1", Username: "alice"}}, authCodeLore{},
		staticAuthenticator{principal: auth.Principal{Issuer: "issuer", Subject: "subject", Username: "alice"}},
		healthy{}, "", slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithAuthentication(AuthOptions{
			SessionStore: &fakeAuthenticationStore{}, Secrets: codec, PublicOrigin: "https://app.example",
			SessionCookie: SessionCookieOptions{Name: "lorehub_session", Path: "/"},
		}),
		WithDevelopmentLoreCredentials("fixture"),
		WithCollaboration(&authCollabStore{}),
	)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/repositories/acme/private/file?revision="+testRevision+"&path=README.md",
		nil,
	)
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("bearer private code read: status=%d body=%s", response.Code, response.Body.String())
	}
}
