package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func TestAccountWithSessionReturnsUserWithoutToken(t *testing.T) {
	handler, sessionToken := accountTestHandler(t, staticAuthenticator{principal: auth.Principal{}})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/account", nil)
	request.AddCookie(&http.Cookie{Name: "lorehub_session", Value: sessionToken})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("account session status = %d, body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		User  accountUser           `json:"user"`
		Token *accountTokenResponse `json:"token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.User != (accountUser{
		ID: "user-1", Username: "alice", DisplayName: "Alice", AvatarURL: "https://app.example/avatar.png",
	}) || payload.Token != nil {
		t.Fatalf("unexpected session account response: %+v", payload)
	}
}

func TestAccountWithPersonalAccessTokenReturnsTokenMetadata(t *testing.T) {
	expiresAt := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	lastUsedAt := time.Date(2026, time.August, 13, 11, 55, 0, 0, time.UTC)
	handler, _ := accountTestHandler(t, staticAuthenticator{principal: auth.Principal{
		InternalUserID:       "user-1",
		CredentialKind:       auth.CredentialPersonalAccessToken,
		CredentialID:         "token-1",
		CredentialPrefix:     "lhp_abcdefgh",
		CredentialExpiresAt:  expiresAt,
		CredentialLastUsedAt: &lastUsedAt,
		Scopes:               []string{auth.ScopeReadAPI},
	}})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/account", nil)
	request.Header.Set("Authorization", "Bearer lhp_presented-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("account PAT status = %d, body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		User  accountUser           `json:"user"`
		Token *accountTokenResponse `json:"token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Token == nil {
		t.Fatalf("PAT account response did not include token: %+v", payload)
	}
	if payload.User.ID != "user-1" || payload.User.Username != "alice" ||
		payload.User.DisplayName != "Alice" || payload.User.AvatarURL != "https://app.example/avatar.png" {
		t.Fatalf("unexpected PAT user: %+v", payload.User)
	}
	if payload.Token.ID != "token-1" || payload.Token.Prefix != "lhp_abcdefgh" ||
		!reflect.DeepEqual(payload.Token.Permissions, []string{auth.ScopeReadAPI}) ||
		!payload.Token.ExpiresAt.Equal(expiresAt) || payload.Token.LastUsedAt == nil ||
		!payload.Token.LastUsedAt.Equal(lastUsedAt) {
		t.Fatalf("unexpected PAT metadata: %+v", payload.Token)
	}
}

func TestAccountRequiresAuthentication(t *testing.T) {
	handler, _ := accountTestHandler(t, accountAuthenticator{err: auth.ErrMissingToken})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/account", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated account status = %d, body=%s", response.Code, response.Body.String())
	}
}

type accountAuthenticator struct {
	principal auth.Principal
	err       error
}

func (authenticator accountAuthenticator) Authenticate(
	_ context.Context,
	_ string,
) (auth.Principal, error) {
	return authenticator.principal, authenticator.err
}

func accountTestHandler(t *testing.T, authenticator auth.Authenticator) (http.Handler, string) {
	t.Helper()
	codec, err := auth.NewSecretCodec("account HTTP test secret")
	if err != nil {
		t.Fatal(err)
	}
	sessionToken := "account-test-session"
	authenticationStore := &fakeAuthenticationStore{
		session: auth.Session{
			ID:          "session-1",
			UserID:      "user-1",
			Username:    "alice",
			DisplayName: "Alice",
			Locale:      "en",
			CSRFDigest:  codec.Digest(codec.CSRFToken(sessionToken)),
			ExpiresAt:   time.Now().UTC().Add(time.Hour),
		},
		sessionTokenHash: codec.Digest(sessionToken),
		sessionValid:     true,
	}
	return New(
		fakeStore{user: platform.User{
			ID: "user-1", Username: "alice", DisplayName: "Alice", AvatarURL: "https://app.example/avatar.png",
		}},
		fakeLore{},
		authenticator,
		healthy{},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithAuthentication(AuthOptions{
			SessionStore: authenticationStore,
			Secrets:      codec,
			PublicOrigin: "https://app.example",
			SessionCookie: SessionCookieOptions{
				Name: "lorehub_session", Path: "/", Secure: true,
			},
		}),
	), sessionToken
}
