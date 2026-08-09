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
	prompt        string
	exchangeCalls int
	principal     auth.Principal
	exchangeError error
}

func (provider *fakeLoginProvider) AuthorizationURL(
	state string,
	codeChallenge string,
	nonce string,
	prompt string,
) string {
	provider.state = state
	provider.codeChallenge = codeChallenge
	provider.nonce = nonce
	provider.prompt = prompt
	values := url.Values{}
	values.Set("state", state)
	values.Set("code_challenge", codeChallenge)
	values.Set("nonce", nonce)
	if prompt != "" {
		values.Set("prompt", prompt)
	}
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
	transactions     map[string]auth.LoginTransaction
	usedTransactions map[string]bool
	session          auth.Session
	sessionTokenHash []byte
	sessionValid     bool
	revokeCalls      int
}

func (store *fakeAuthenticationStore) CreateLoginTransaction(
	_ context.Context,
	transaction auth.LoginTransaction,
) error {
	if store.transactions == nil {
		store.transactions = make(map[string]auth.LoginTransaction)
		store.usedTransactions = make(map[string]bool)
	}
	store.transaction = transaction
	store.transactionUsed = false
	key := string(transaction.StateDigest)
	store.transactions[key] = transaction
	store.usedTransactions[key] = false
	return nil
}

func (store *fakeAuthenticationStore) ConsumeLoginTransaction(
	_ context.Context,
	stateDigest []byte,
	now time.Time,
) (auth.LoginTransaction, error) {
	key := string(stateDigest)
	transaction, found := store.transactions[key]
	if key == string(store.transaction.StateDigest) {
		transaction = store.transaction
		found = true
	}
	used := store.usedTransactions[key]
	if key == string(store.transaction.StateDigest) {
		used = store.transactionUsed
	}
	if !found || used || transaction.ExpiresAt.Before(now) || !bytes.Equal(stateDigest, transaction.StateDigest) {
		return auth.LoginTransaction{}, auth.ErrInvalidTransaction
	}
	store.usedTransactions[key] = true
	if key == string(store.transaction.StateDigest) {
		store.transactionUsed = true
	}
	return transaction, nil
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

func cookieNamed(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("cookie %q was not set; got %#v", name, cookies)
	return &http.Cookie{}
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
	if loginURL.Query().Get("prompt") != "" {
		t.Fatal("normal login unexpectedly requested registration")
	}
	bindingCookie := cookieNamed(t, loginResponse.Result().Cookies(), "lorehub_login_binding")
	if bindingCookie.HttpOnly == false || bindingCookie.Secure == false || bindingCookie.Path != "/auth" ||
		bindingCookie.SameSite != http.SameSiteLaxMode || bindingCookie.MaxAge != 600 ||
		bindingCookie.Value != provider.state ||
		bindingCookie.Expires.Unix() != authenticationStore.transaction.ExpiresAt.Unix() {
		t.Fatalf("binding cookie is not narrowly scoped and expiring: %#v", bindingCookie)
	}

	callbackRequest := httptest.NewRequest(
		http.MethodGet,
		"/auth/callback?state="+url.QueryEscape(provider.state)+"&code=authorization-code",
		nil,
	)
	callbackRequest.AddCookie(bindingCookie)
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
	sessionCookie := cookieNamed(t, cookies, "lorehub_session")
	clearedBinding := cookieNamed(t, cookies, "lorehub_login_binding")
	if !sessionCookie.HttpOnly || !sessionCookie.Secure || sessionCookie.SameSite != http.SameSiteLaxMode ||
		sessionCookie.Path != "/" || sessionCookie.MaxAge <= 0 || clearedBinding.MaxAge >= 0 ||
		clearedBinding.Path != "/auth" {
		t.Fatalf("session cookie is not secure: %#v", cookies)
	}
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

func TestCallbackRequiresBindingAndClearsMatchingTerminalCookies(t *testing.T) {
	codec, _ := auth.NewSecretCodec("test authentication secret")
	provider := &fakeLoginProvider{principal: auth.Principal{Issuer: "issuer", Subject: "subject"}}
	authenticationStore := &fakeAuthenticationStore{}
	handler := newAuthTestHandler(provider, authenticationStore, codec, auth.DisabledAuthenticator{})
	startLogin := func() (string, *http.Cookie) {
		request := httptest.NewRequest(http.MethodGet, "/auth/login", nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return provider.state, cookieNamed(t, response.Result().Cookies(), "lorehub_login_binding")
	}
	callback := func(state string, binding *http.Cookie, suffix string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+state+suffix, nil)
		if binding != nil {
			request.AddCookie(binding)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	state, binding := startLogin()
	response := callback(state, nil, "&code=code")
	if response.Code != http.StatusBadRequest || authenticationStore.transactionUsed ||
		len(response.Result().Cookies()) != 0 {
		t.Fatalf("callback without binding was not rejected safely: status=%d used=%t cookies=%#v", response.Code,
			authenticationStore.transactionUsed, response.Result().Cookies())
	}

	state, secondBinding := startLogin()
	response = callback(state, binding, "&code=code")
	if response.Code != http.StatusBadRequest || authenticationStore.transactionUsed ||
		len(response.Result().Cookies()) != 0 {
		t.Fatalf("mismatched binding was not rejected safely: status=%d used=%t cookies=%#v", response.Code,
			authenticationStore.transactionUsed, response.Result().Cookies())
	}
	response = callback(state, secondBinding, "&code=code")
	if response.Code != http.StatusSeeOther || provider.exchangeCalls != 1 {
		t.Fatalf("matching binding did not complete login: status=%d exchanges=%d", response.Code,
			provider.exchangeCalls)
	}
	response = callback(state, secondBinding, "&code=code")
	if response.Code != http.StatusBadRequest ||
		cookieNamed(t, response.Result().Cookies(), "lorehub_login_binding").MaxAge >= 0 {
		t.Fatalf("replayed matching callback did not clear its binding: status=%d cookies=%#v", response.Code,
			response.Result().Cookies())
	}

	state, binding = startLogin()
	response = callback(state, binding, "&error=access_denied")
	if response.Code != http.StatusBadRequest || !authenticationStore.transactionUsed ||
		cookieNamed(t, response.Result().Cookies(), "lorehub_login_binding").MaxAge >= 0 {
		t.Fatalf("provider-error callback did not consume and clear binding: status=%d used=%t", response.Code,
			authenticationStore.transactionUsed)
	}

	state, binding = startLogin()
	provider.exchangeError = errors.New("provider exchange failed")
	response = callback(state, binding, "&code=code")
	provider.exchangeError = nil
	if response.Code != http.StatusUnauthorized || !authenticationStore.transactionUsed ||
		cookieNamed(t, response.Result().Cookies(), "lorehub_login_binding").MaxAge >= 0 {
		t.Fatalf("exchange-error callback did not consume and clear binding: status=%d used=%t", response.Code,
			authenticationStore.transactionUsed)
	}

	state, binding = startLogin()
	authenticationStore.transaction.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	response = callback(state, binding, "&code=code")
	if response.Code != http.StatusBadRequest || authenticationStore.transactionUsed ||
		cookieNamed(t, response.Result().Cookies(), "lorehub_login_binding").MaxAge >= 0 {
		t.Fatalf("expired callback did not clear matching binding: status=%d used=%t", response.Code,
			authenticationStore.transactionUsed)
	}
}

func TestLoginRegistrationPromptIsStrictlyValidated(t *testing.T) {
	codec, _ := auth.NewSecretCodec("test authentication secret")
	provider := &fakeLoginProvider{principal: auth.Principal{Issuer: "issuer", Subject: "subject"}}
	handler := newAuthTestHandler(provider, &fakeAuthenticationStore{}, codec, auth.DisabledAuthenticator{})
	start := func(path string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	response := start("/auth/login")
	if response.Code != http.StatusFound || provider.prompt != "" {
		t.Fatalf("normal login unexpectedly requested registration: status=%d prompt=%q", response.Code, provider.prompt)
	}
	if query, _ := url.Parse(response.Header().Get("Location")); query.Query().Get("prompt") != "" {
		t.Fatal("normal login forwarded a prompt")
	}

	response = start("/auth/login?prompt=create")
	if response.Code != http.StatusFound || provider.prompt != auth.RegistrationPrompt {
		t.Fatalf("prompt=create did not start registration: status=%d prompt=%q", response.Code, provider.prompt)
	}
	registrationURL, _ := url.Parse(response.Header().Get("Location"))
	if registrationURL.Query().Get("prompt") != auth.RegistrationPrompt || registrationURL.Query().Get("kc_action") != "" {
		t.Fatalf("registration request was not narrowed to prompt=create: %s", registrationURL)
	}

	response = start("/auth/login?kc_action=register")
	if response.Code != http.StatusFound || provider.prompt != auth.RegistrationPrompt {
		t.Fatalf("kc_action=register was not mapped to prompt=create: status=%d prompt=%q", response.Code,
			provider.prompt)
	}
	for _, path := range []string{"/auth/login?prompt=login", "/auth/login?kc_action=login",
		"/auth/login?prompt=create&kc_action=login"} {
		response = start(path)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("unsupported registration input %q returned %d", path, response.Code)
		}
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
	bindingCookie := cookieNamed(t, loginResponse.Result().Cookies(), "lorehub_login_binding")
	callback := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+state+"&code=code", nil)
		request.AddCookie(bindingCookie)
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
	bindingCookie = cookieNamed(t, loginResponse.Result().Cookies(), "lorehub_login_binding")
	authenticationStore.transaction.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	request := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+provider.state+"&code=code", nil)
	request.AddCookie(bindingCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected expired callback to fail, got %d", response.Code)
	}

	loginRequest = httptest.NewRequest(http.MethodGet, "/auth/login?return_to=%2Fhome", nil)
	loginResponse = httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, loginRequest)
	bindingCookie = cookieNamed(t, loginResponse.Result().Cookies(), "lorehub_login_binding")
	authenticationStore.transaction.NonceDigest = codec.Digest("wrong-nonce")
	request = httptest.NewRequest(http.MethodGet, "/auth/callback?state="+provider.state+"&code=code", nil)
	request.AddCookie(bindingCookie)
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
	bindingCookie := cookieNamed(t, loginResponse.Result().Cookies(), "lorehub_login_binding")
	callbackRequest := httptest.NewRequest(http.MethodGet, "/auth/callback?state="+provider.state+"&code=code", nil)
	callbackRequest.AddCookie(bindingCookie)
	callbackResponse := httptest.NewRecorder()
	handler.ServeHTTP(callbackResponse, callbackRequest)
	sessionCookie := cookieNamed(t, callbackResponse.Result().Cookies(), "lorehub_session")
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
	clearCookie := cookieNamed(t, logoutResponse.Result().Cookies(), "lorehub_session")
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
