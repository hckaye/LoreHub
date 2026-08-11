package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type fakePersonalAccessTokenStore struct {
	tokens    []platform.PersonalAccessToken
	created   platform.CreatePersonalAccessTokenInput
	revokedID string
	listErr   error
	createErr error
	revokeErr error
}

func (store *fakePersonalAccessTokenStore) ListPersonalAccessTokens(
	context.Context,
	platform.User,
) ([]platform.PersonalAccessToken, error) {
	return append([]platform.PersonalAccessToken(nil), store.tokens...), store.listErr
}

func (store *fakePersonalAccessTokenStore) CreatePersonalAccessToken(
	_ context.Context,
	_ platform.User,
	input platform.CreatePersonalAccessTokenInput,
) (platform.PersonalAccessToken, error) {
	store.created = input
	return platform.PersonalAccessToken{
		ID:        "00000000-0000-4000-8000-000000000001",
		Name:      input.Name,
		Prefix:    input.Prefix,
		Scopes:    input.Scopes,
		ExpiresAt: input.ExpiresAt,
		CreatedAt: time.Now().UTC(),
	}, store.createErr
}

func (store *fakePersonalAccessTokenStore) RevokePersonalAccessToken(
	_ context.Context,
	_ platform.User,
	tokenID string,
) error {
	store.revokedID = tokenID
	return store.revokeErr
}

func personalAccessTokenTestHandler(
	t *testing.T,
	tokenStore *fakePersonalAccessTokenStore,
) (http.Handler, string, string) {
	t.Helper()
	codec, err := auth.NewSecretCodec("personal access token HTTP test secret")
	if err != nil {
		t.Fatal(err)
	}
	sessionToken := "personal-access-token-test-session"
	csrfToken := codec.CSRFToken(sessionToken)
	authenticationStore := &fakeAuthenticationStore{
		session: auth.Session{
			ID:          "session-1",
			UserID:      "user-1",
			Username:    "alice",
			DisplayName: "Alice",
			Locale:      "en",
			CSRFDigest:  codec.Digest(csrfToken),
			ExpiresAt:   time.Now().UTC().Add(time.Hour),
		},
		sessionTokenHash: codec.Digest(sessionToken),
		sessionValid:     true,
	}
	handler := New(
		fakeStore{user: platform.User{ID: "user-1", Username: "alice", DisplayName: "Alice", Locale: "en"}},
		fakeLore{},
		auth.DisabledAuthenticator{},
		healthy{},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithAuthentication(AuthOptions{
			SessionStore: authenticationStore,
			Secrets:      codec,
			PublicOrigin: "https://app.example",
			SessionCookie: SessionCookieOptions{
				Name:   "lorehub_session",
				Path:   "/",
				Secure: true,
			},
		}),
		WithPersonalAccessTokens(tokenStore, codec),
	)
	return handler, sessionToken, csrfToken
}

func TestPersonalAccessTokenHTTPCreateListAndRevoke(t *testing.T) {
	store := &fakePersonalAccessTokenStore{tokens: []platform.PersonalAccessToken{{
		ID:        "00000000-0000-4000-8000-000000000001",
		Name:      "Existing token",
		Prefix:    "lhp_abcdefgh",
		Scopes:    []string{auth.ScopeReadAPI},
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
		CreatedAt: time.Now().UTC(),
	}}}
	handler, sessionToken, csrfToken := personalAccessTokenTestHandler(t, store)
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/account/personal-access-tokens", nil)
	listRequest.AddCookie(&http.Cookie{Name: "lorehub_session", Value: sessionToken})
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || listResponse.Header().Get("Cache-Control") != "no-store" ||
		bytes.Contains(listResponse.Body.Bytes(), []byte("token_digest")) {
		t.Fatalf("list response: status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	expiresAt := time.Now().UTC().Add(30 * 24 * time.Hour).Truncate(time.Second)
	body, _ := json.Marshal(map[string]any{
		"name": "Developer workstation", "scopes": []string{auth.ScopeAPI}, "expiresAt": expiresAt,
	})
	withoutCSRF := httptest.NewRequest(http.MethodPost, "/api/v1/account/personal-access-tokens",
		bytes.NewReader(body))
	withoutCSRF.AddCookie(&http.Cookie{Name: "lorehub_session", Value: sessionToken})
	withoutCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(withoutCSRFResponse, withoutCSRF)
	if withoutCSRFResponse.Code != http.StatusForbidden {
		t.Fatalf("create without CSRF status = %d", withoutCSRFResponse.Code)
	}
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/account/personal-access-tokens",
		bytes.NewReader(body))
	createRequest.AddCookie(&http.Cookie{Name: "lorehub_session", Value: sessionToken})
	createRequest.Header.Set("X-CSRF-Token", csrfToken)
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated || createResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("create response: status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
	var created struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(createResponse.Body.Bytes(), &created); err != nil ||
		!auth.ValidPersonalAccessToken(created.Value) || len(store.created.Digest) != 32 ||
		store.created.Prefix != created.Value[:12] {
		t.Fatalf("created token response is invalid: body=%s error=%v", createResponse.Body.String(), err)
	}
	deleteRequest := httptest.NewRequest(http.MethodDelete,
		"/api/v1/account/personal-access-tokens/00000000-0000-4000-8000-000000000001", nil)
	deleteRequest.AddCookie(&http.Cookie{Name: "lorehub_session", Value: sessionToken})
	deleteRequest.Header.Set("X-CSRF-Token", csrfToken)
	deleteResponse := httptest.NewRecorder()
	handler.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusNoContent ||
		store.revokedID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("revoke response: status=%d id=%q body=%s", deleteResponse.Code, store.revokedID,
			deleteResponse.Body.String())
	}
}

func TestPersonalAccessTokenManagementRequiresBrowserSession(t *testing.T) {
	handler, _, _ := personalAccessTokenTestHandler(t, &fakePersonalAccessTokenStore{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/account/personal-access-tokens", nil)
	request.Header.Set("Authorization", "Bearer lhp_not-a-browser-session")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("bearer management status = %d, body=%s", response.Code, response.Body.String())
	}
}

func TestPersonalAccessTokenRESTScopes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		scopes []string
		method string
		ok     bool
	}{
		{name: "read API reads", scopes: []string{auth.ScopeReadAPI}, method: http.MethodGet, ok: true},
		{name: "read API cannot write", scopes: []string{auth.ScopeReadAPI}, method: http.MethodPost},
		{name: "API writes", scopes: []string{auth.ScopeAPI}, method: http.MethodPost, ok: true},
		{name: "repository scope cannot use REST", scopes: []string{auth.ScopeReadRepository}, method: http.MethodGet},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			api := &API{
				store: fakeStore{user: platform.User{ID: "user-1", Username: "alice"}},
				authenticator: staticAuthenticator{principal: auth.Principal{
					InternalUserID: "user-1",
					CredentialKind: auth.CredentialPersonalAccessToken,
					CredentialID:   "00000000-0000-4000-8000-000000000001",
					Scopes:         testCase.scopes,
				}},
				logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			}
			response := httptest.NewRecorder()
			_, ok := api.actor(response, httptest.NewRequest(testCase.method, "/api/v1/example", nil))
			if ok != testCase.ok {
				t.Fatalf("actor resolved=%v, status=%d", ok, response.Code)
			}
			if !testCase.ok && response.Code != http.StatusForbidden {
				t.Fatalf("insufficient scope status = %d", response.Code)
			}
		})
	}
}
