package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type fakeLoginProvider struct {
	state         string
	codeChallenge string
	codeVerifier  string
	nonce         string
	exchangeCalls int
	principal     auth.Principal
	exchangeError error
}

func (provider *fakeLoginProvider) AuthorizationURL(state string, codeChallenge string, nonce string) string {
	provider.state = state
	provider.codeChallenge = codeChallenge
	provider.nonce = nonce
	values := url.Values{}
	values.Set("state", state)
	values.Set("code_challenge", codeChallenge)
	values.Set("nonce", nonce)
	return "https://identity.example/authorize?" + values.Encode()
}

func (provider *fakeLoginProvider) Exchange(
	_ context.Context,
	_ string,
	codeVerifier string,
	nonce string,
) (auth.Principal, error) {
	provider.exchangeCalls++
	provider.codeVerifier = codeVerifier
	if provider.exchangeError != nil {
		return auth.Principal{}, provider.exchangeError
	}
	if codeVerifier == "" || nonce != provider.nonce {
		return auth.Principal{}, errors.New("unexpected OIDC transaction values")
	}
	return provider.principal, nil
}

type fakeAuthenticationStore struct {
	transaction      auth.LoginTransaction
	transactionUsed  bool
	session          auth.Session
	sessionTokenHash []byte
	sessionValid     bool
	revokeCalls      int
}

func (store *fakeAuthenticationStore) CreateLoginTransaction(
	_ context.Context,
	transaction auth.LoginTransaction,
) error {
	store.transaction = transaction
	store.transactionUsed = false
	return nil
}

func (store *fakeAuthenticationStore) ConsumeLoginTransaction(
	_ context.Context,
	stateDigest []byte,
	now time.Time,
) (auth.LoginTransaction, error) {
	if store.transactionUsed || store.transaction.ExpiresAt.Before(now) ||
		!bytes.Equal(stateDigest, store.transaction.StateDigest) {
		return auth.LoginTransaction{}, auth.ErrInvalidTransaction
	}
	store.transactionUsed = true
	return store.transaction, nil
}

func (store *fakeAuthenticationStore) CreateSession(
	_ context.Context,
	userID string,
	tokenDigest []byte,
	csrfDigest []byte,
	expiresAt time.Time,
) (auth.Session, error) {
	store.session = auth.Session{
		ID:          "session-1",
		UserID:      userID,
		Username:    "alice",
		DisplayName: "Alice",
		Email:       "alice@example.com",
		Locale:      "en",
		CSRFDigest:  append([]byte(nil), csrfDigest...),
		CreatedAt:   time.Now().UTC(),
		ExpiresAt:   expiresAt,
		LastSeenAt:  time.Now().UTC(),
	}
	store.sessionTokenHash = append([]byte(nil), tokenDigest...)
	store.sessionValid = true
	return store.session, nil
}

func (store *fakeAuthenticationStore) LookupSession(
	_ context.Context,
	tokenDigest []byte,
	now time.Time,
) (auth.Session, error) {
	if !store.sessionValid || store.session.ExpiresAt.Before(now) || !bytes.Equal(tokenDigest, store.sessionTokenHash) {
		return auth.Session{}, auth.ErrInvalidSession
	}
	return store.session, nil
}

func (store *fakeAuthenticationStore) RevokeSession(
	_ context.Context,
	tokenDigest []byte,
	_ time.Time,
) error {
	if bytes.Equal(tokenDigest, store.sessionTokenHash) {
		store.revokeCalls++
		store.sessionValid = false
	}
	return nil
}

func (store *fakeAuthenticationStore) CleanupExpiredAuthentication(context.Context, time.Time) error {
	return nil
}

type staticAuthenticator struct {
	principal auth.Principal
}

func (authenticator staticAuthenticator) Authenticate(context.Context, string) (auth.Principal, error) {
	return authenticator.principal, nil
}

func newAuthTestHandler(
	provider *fakeLoginProvider,
	authenticationStore *fakeAuthenticationStore,
	secretCodec *auth.SecretCodec,
	authenticator auth.Authenticator,
) http.Handler {
	return New(
		fakeStore{user: platform.User{
			ID:          "user-1",
			Username:    "alice",
			DisplayName: "Alice",
			Email:       "alice@example.com",
			Locale:      "en",
		}},
		fakeLore{},
		authenticator,
		healthy{},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithAuthentication(AuthOptions{
			LoginProvider: provider,
			LoginStore:    authenticationStore,
			SessionStore:  authenticationStore,
			CleanupStore:  authenticationStore,
			Secrets:       secretCodec,
			PublicOrigin:  "https://app.example",
			SessionTTL:    time.Hour,
			SessionCookie: SessionCookieOptions{
				Name:   "lorehub_session",
				Path:   "/",
				Secure: true,
			},
		}),
	)
}

func TestAuthorizationCodeFlowCreatesSessionAndProtectsCookieRequests(t *testing.T) {
	codec, err := auth.NewSecretCodec("test authentication secret")
	if err != nil {
		t.Fatal(err)
	}
	provider := &fakeLoginProvider{principal: auth.Principal{
		Issuer:  "https://identity.example",
		Subject: "subject-1",
		Name:    "Alice",
	}}
	authenticationStore := &fakeAuthenticationStore{}
	handler := newAuthTestHandler(provider, authenticationStore, codec, auth.DisabledAuthenticator{})

	loginRequest := httptest.NewRequest(http.MethodGet, "/auth/login?return_to=%2Fdashboard", nil)
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusFound {
		t.Fatalf("expected login redirect, got %d", loginResponse.Code)
	}
	loginURL, err := url.Parse(loginResponse.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if provider.codeChallenge != codec.CodeChallenge(codec.CodeVerifier(provider.state)) {
		t.Fatal("login did not send the S256 PKCE challenge")
	}
	if loginURL.Host != "identity.example" || loginURL.Query().Get("state") != provider.state {
		t.Fatalf("unexpected provider redirect: %s", loginURL)
	}

	callbackRequest := httptest.NewRequest(
		http.MethodGet,
		"/auth/callback?state="+url.QueryEscape(provider.state)+"&code=authorization-code",
		nil,
	)
	callbackResponse := httptest.NewRecorder()
	handler.ServeHTTP(callbackResponse, callbackRequest)
	if callbackResponse.Code != http.StatusSeeOther || callbackResponse.Header().Get("Location") != "/dashboard" {
		t.Fatalf("expected callback redirect to dashboard, got %d and %q", callbackResponse.Code,
			callbackResponse.Header().Get("Location"))
	}
	if provider.codeVerifier != codec.CodeVerifier(provider.state) {
		t.Fatal("callback did not send the transaction's PKCE verifier")
	}
	cookies := callbackResponse.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "lorehub_session" || !cookies[0].HttpOnly || !cookies[0].Secure ||
		cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Path != "/" || cookies[0].MaxAge <= 0 {
		t.Fatalf("session cookie is not secure: %#v", cookies)
	}
	sessionCookie := cookies[0]
	if bytes.Equal(authenticationStore.transaction.StateDigest, []byte(provider.state)) ||
		bytes.Equal(authenticationStore.sessionTokenHash, []byte(sessionCookie.Value)) {
		t.Fatal("opaque authentication values were stored without a digest")
	}

	sessionRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	sessionRequest.AddCookie(sessionCookie)
	sessionResponse := httptest.NewRecorder()
	handler.ServeHTTP(sessionResponse, sessionRequest)
	var sessionBody authSessionResponse
	if err := json.NewDecoder(sessionResponse.Body).Decode(&sessionBody); err != nil {
		t.Fatal(err)
	}
	if sessionResponse.Code != http.StatusOK || !sessionBody.Authenticated || sessionBody.User == nil ||
		sessionBody.Session == nil || sessionBody.CSRFToken == "" {
		t.Fatalf("unexpected authenticated session response: %#v", sessionBody)
	}

	requestBody := strings.NewReader(`{"displayName":"Lore","slug":"lore","visibility":"public"}`)
	withoutCSRF := httptest.NewRequest(http.MethodPost, "/api/v1/organizations", requestBody)
	withoutCSRF.Header.Set("Content-Type", "application/json")
	withoutCSRF.AddCookie(sessionCookie)
	withoutCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(withoutCSRFResponse, withoutCSRF)
	if withoutCSRFResponse.Code != http.StatusForbidden {
		t.Fatalf("expected cookie request without CSRF to be rejected, got %d", withoutCSRFResponse.Code)
	}

	wrongOrigin := httptest.NewRequest(http.MethodPost, "/api/v1/organizations", strings.NewReader(
		`{"displayName":"Lore","slug":"lore","visibility":"public"}`,
	))
	wrongOrigin.Header.Set("Content-Type", "application/json")
	wrongOrigin.Header.Set("Origin", "https://evil.example")
	wrongOrigin.Header.Set("X-CSRF-Token", sessionBody.CSRFToken)
	wrongOrigin.AddCookie(sessionCookie)
	wrongOriginResponse := httptest.NewRecorder()
	handler.ServeHTTP(wrongOriginResponse, wrongOrigin)
	if wrongOriginResponse.Code != http.StatusForbidden {
		t.Fatalf("expected cross-origin cookie request to be rejected, got %d", wrongOriginResponse.Code)
	}

	withCSRF := httptest.NewRequest(http.MethodPost, "/api/v1/organizations", strings.NewReader(
		`{"displayName":"Lore","slug":"lore","visibility":"public"}`,
	))
	withCSRF.Header.Set("Content-Type", "application/json")
	withCSRF.Header.Set("X-CSRF-Token", sessionBody.CSRFToken)
	withCSRF.AddCookie(sessionCookie)
	withCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(withCSRFResponse, withCSRF)
	if withCSRFResponse.Code != http.StatusCreated {
		t.Fatalf("expected cookie request with CSRF to succeed, got %d", withCSRFResponse.Code)
	}
	if provider.exchangeCalls != 1 {
		t.Fatalf("expected one authorization-code exchange, got %d", provider.exchangeCalls)
	}
}

func TestAuthenticationRejectsOpenRedirectsAndReplayedOrExpiredState(t *testing.T) {
	codec, _ := auth.NewSecretCodec("test authentication secret")
	provider := &fakeLoginProvider{principal: auth.Principal{Issuer: "issuer", Subject: "subject"}}
	authenticationStore := &fakeAuthenticationStore{}
	handler := newAuthTestHandler(provider, authenticationStore, codec, auth.DisabledAuthenticator{})

	for _, value := range []string{"https://evil.example/", "//evil.example/", "/%2f%2fevil.example/"} {
		request := httptest.NewRequest(http.MethodGet, "/auth/login?return_to="+url.QueryEscape(value), nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("expected open redirect %q to be rejected, got %d", value, response.Code)
		}
	}

	loginRequest := httptest.NewRequest(http.MethodGet, "/auth/login?return_to=%2Fhome", nil)
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, loginRequest)
	state := provider.state
	callback := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+state+"&code=code", nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	if response := callback(); response.Code != http.StatusSeeOther {
		t.Fatalf("expected first callback to succeed, got %d", response.Code)
	}
	if response := callback(); response.Code != http.StatusBadRequest {
		t.Fatalf("expected replayed callback to fail, got %d", response.Code)
	}

	loginRequest = httptest.NewRequest(http.MethodGet, "/auth/login?return_to=%2Fhome", nil)
	loginResponse = httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, loginRequest)
	authenticationStore.transaction.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	request := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+provider.state+"&code=code", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected expired callback to fail, got %d", response.Code)
	}

	loginRequest = httptest.NewRequest(http.MethodGet, "/auth/login?return_to=%2Fhome", nil)
	loginResponse = httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, loginRequest)
	authenticationStore.transaction.NonceDigest = codec.Digest("wrong-nonce")
	request = httptest.NewRequest(http.MethodGet, "/auth/callback?state="+provider.state+"&code=code", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected a nonce transaction mismatch to fail, got %d", response.Code)
	}
}

func TestAnonymousSessionResponseIsStable(t *testing.T) {
	codec, _ := auth.NewSecretCodec("test authentication secret")
	handler := newAuthTestHandler(&fakeLoginProvider{}, &fakeAuthenticationStore{}, codec,
		auth.DisabledAuthenticator{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var body authSessionResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || body.Authenticated || body.User != nil || body.Session != nil ||
		body.CSRFToken != "" {
		t.Fatalf("unexpected anonymous response: %#v", body)
	}
}

func TestLogoutRequiresCSRFAndBearerRequestsRemainCompatible(t *testing.T) {
	codec, _ := auth.NewSecretCodec("test authentication secret")
	provider := &fakeLoginProvider{principal: auth.Principal{Issuer: "issuer", Subject: "subject"}}
	authenticationStore := &fakeAuthenticationStore{}
	handler := newAuthTestHandler(provider, authenticationStore, codec, staticAuthenticator{
		principal: auth.Principal{Issuer: "issuer", Subject: "subject"},
	})

	loginRequest := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, loginRequest)
	callbackRequest := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+provider.state+"&code=code", nil)
	callbackResponse := httptest.NewRecorder()
	handler.ServeHTTP(callbackResponse, callbackRequest)
	sessionCookie := callbackResponse.Result().Cookies()[0]
	sessionRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	sessionRequest.AddCookie(sessionCookie)
	sessionResponse := httptest.NewRecorder()
	handler.ServeHTTP(sessionResponse, sessionRequest)
	var body authSessionResponse
	if err := json.NewDecoder(sessionResponse.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}

	logout := httptest.NewRequest(http.MethodPost, "/auth/logout?return_to=%2Fgood", nil)
	logout.AddCookie(sessionCookie)
	logoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(logoutResponse, logout)
	if logoutResponse.Code != http.StatusForbidden || authenticationStore.revokeCalls != 0 {
		t.Fatalf("logout without CSRF was not rejected: status=%d revokes=%d", logoutResponse.Code,
			authenticationStore.revokeCalls)
	}

	logout = httptest.NewRequest(http.MethodPost, "/auth/logout?return_to=%2Fgood", nil)
	logout.Header.Set("X-CSRF-Token", body.CSRFToken)
	logout.AddCookie(sessionCookie)
	logoutResponse = httptest.NewRecorder()
	handler.ServeHTTP(logoutResponse, logout)
	if logoutResponse.Code != http.StatusSeeOther || logoutResponse.Header().Get("Location") != "/good" ||
		authenticationStore.revokeCalls != 1 {
		t.Fatalf("logout with CSRF failed: status=%d location=%q revokes=%d", logoutResponse.Code,
			logoutResponse.Header().Get("Location"), authenticationStore.revokeCalls)
	}
	clearCookie := logoutResponse.Result().Cookies()[0]
	if clearCookie.MaxAge >= 0 || !clearCookie.HttpOnly || clearCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("logout did not clear a secure session cookie: %#v", clearCookie)
	}

	bearerRequest := httptest.NewRequest(http.MethodPost, "/api/v1/organizations", bytes.NewBufferString(
		`{"displayName":"Lore","slug":"lore","visibility":"public"}`,
	))
	bearerRequest.Header.Set("Authorization", "Bearer opaque-api-token")
	bearerRequest.Header.Set("Content-Type", "application/json")
	bearerResponse := httptest.NewRecorder()
	handler.ServeHTTP(bearerResponse, bearerRequest)
	if bearerResponse.Code != http.StatusCreated {
		t.Fatalf("bearer API compatibility failed: got %d", bearerResponse.Code)
	}
}
