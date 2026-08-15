package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type fakePasswordStore struct {
	credential      platform.PasswordCredential
	hasCredential   bool
	failures        int
	lockedUntil     *time.Time
	created         *platform.PasswordUserInput
	createError     error
	setPasswordHash string
	revokedOthers   int
}

func (store *fakePasswordStore) CreatePasswordUser(
	_ context.Context,
	input platform.PasswordUserInput,
) (platform.User, error) {
	if store.createError != nil {
		return platform.User{}, store.createError
	}
	store.created = &input
	return platform.User{ID: "user-1", Username: input.Username, Email: input.Email}, nil
}

func (store *fakePasswordStore) PasswordCredential(
	_ context.Context,
	identifier string,
) (platform.PasswordCredential, error) {
	if !store.hasCredential ||
		identifier != store.credential.Email && identifier != "alice" {
		return platform.PasswordCredential{}, platform.ErrNotFound
	}
	credential := store.credential
	credential.FailedAttempts = store.failures
	credential.LockedUntil = store.lockedUntil
	return credential, nil
}

func (store *fakePasswordStore) PasswordCredentialForUser(
	_ context.Context,
	userID string,
) (platform.PasswordCredential, error) {
	if !store.hasCredential || userID != store.credential.UserID {
		return platform.PasswordCredential{}, platform.ErrNotFound
	}
	credential := store.credential
	credential.FailedAttempts = store.failures
	credential.LockedUntil = store.lockedUntil
	return credential, nil
}

func (store *fakePasswordStore) RecordPasswordFailure(_ context.Context, _ string) (int, error) {
	store.failures++
	return store.failures, nil
}

func (store *fakePasswordStore) LockPasswordCredential(_ context.Context, _ string, until time.Time) error {
	store.lockedUntil = &until
	return nil
}

func (store *fakePasswordStore) ClearPasswordFailures(context.Context, string) error {
	store.failures = 0
	store.lockedUntil = nil
	return nil
}

func (store *fakePasswordStore) SetPassword(_ context.Context, _ string, passwordHash string) error {
	store.setPasswordHash = passwordHash
	return nil
}

func (store *fakePasswordStore) RevokeOtherSessions(
	context.Context, string, []byte, time.Time,
) error {
	store.revokedOthers++
	return nil
}

func newPasswordTestHandler(
	passwordStore *fakePasswordStore,
	authenticationStore *fakeAuthenticationStore,
	secretCodec *auth.SecretCodec,
	registration bool,
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
		auth.DisabledAuthenticator{},
		healthy{},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithAuthentication(AuthOptions{
			LoginStore:   authenticationStore,
			SessionStore: authenticationStore,
			CleanupStore: authenticationStore,
			Secrets:      secretCodec,
			PublicOrigin: "https://app.example",
			SessionTTL:   time.Hour,
			SessionCookie: SessionCookieOptions{
				Name:   "lorehub_session",
				Path:   "/",
				Secure: true,
			},
		}),
		WithPasswordAuthentication(passwordStore, registration),
	)
}

func jsonRequest(method string, path string, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func TestPasswordLoginCreatesSessionAndLocksAfterRepeatedFailures(t *testing.T) {
	codec, err := auth.NewSecretCodec("test authentication secret")
	if err != nil {
		t.Fatal(err)
	}
	passwordHash, err := auth.HashPassword("Sufficient-1Password")
	if err != nil {
		t.Fatal(err)
	}
	passwordStore := &fakePasswordStore{
		credential: platform.PasswordCredential{
			UserID:       "user-1",
			Email:        "alice@example.com",
			PasswordHash: passwordHash,
		},
		hasCredential: true,
	}
	authenticationStore := &fakeAuthenticationStore{}
	handler := newPasswordTestHandler(passwordStore, authenticationStore, codec, true)

	login := func(body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, jsonRequest(http.MethodPost, "/auth/password/login", body))
		return response
	}

	response := login(`{"identifier":"ALICE@example.com","password":"Sufficient-1Password"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("expected login to succeed, got %d: %s", response.Code, response.Body)
	}
	sessionCookie := cookieNamed(t, response.Result().Cookies(), "lorehub_session")
	if !sessionCookie.HttpOnly || !sessionCookie.Secure || sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie is not secure: %#v", sessionCookie)
	}
	sessionRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	sessionRequest.AddCookie(sessionCookie)
	sessionResponse := httptest.NewRecorder()
	handler.ServeHTTP(sessionResponse, sessionRequest)
	var sessionBody authSessionResponse
	if err := json.NewDecoder(sessionResponse.Body).Decode(&sessionBody); err != nil {
		t.Fatal(err)
	}
	if !sessionBody.Authenticated || sessionBody.CSRFToken == "" {
		t.Fatalf("password login did not establish a session: %#v", sessionBody)
	}

	if response := login(`{"identifier":"missing@example.com","password":"Sufficient-1Password"}`); response.Code !=
		http.StatusUnauthorized {
		t.Fatalf("unknown identifier returned %d", response.Code)
	}
	for attempt := 0; attempt < 5; attempt++ {
		if response := login(`{"identifier":"alice@example.com","password":"Wrong-1Password!"}`); response.Code !=
			http.StatusUnauthorized {
			t.Fatalf("wrong password returned %d", response.Code)
		}
	}
	if passwordStore.lockedUntil == nil {
		t.Fatal("five failures did not lock the account")
	}
	response = login(`{"identifier":"alice@example.com","password":"Sufficient-1Password"}`)
	if response.Code != http.StatusForbidden {
		t.Fatalf("locked account allowed login: %d", response.Code)
	}
	past := time.Now().UTC().Add(-time.Minute)
	passwordStore.lockedUntil = &past
	if response = login(`{"identifier":"alice@example.com","password":"Sufficient-1Password"}`); response.Code !=
		http.StatusOK {
		t.Fatalf("expired lock still blocked login: %d", response.Code)
	}
	if passwordStore.failures != 0 || passwordStore.lockedUntil != nil {
		t.Fatal("successful login did not clear failures")
	}
}

func TestPasswordLoginRejectsCrossSiteShapedRequests(t *testing.T) {
	codec, _ := auth.NewSecretCodec("test authentication secret")
	passwordStore := &fakePasswordStore{}
	handler := newPasswordTestHandler(passwordStore, &fakeAuthenticationStore{}, codec, true)

	formRequest := httptest.NewRequest(http.MethodPost, "/auth/password/login",
		strings.NewReader(`{"identifier":"alice","password":"Sufficient-1Password"}`))
	formRequest.Header.Set("Content-Type", "text/plain")
	formResponse := httptest.NewRecorder()
	handler.ServeHTTP(formResponse, formRequest)
	if formResponse.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("non-JSON content type returned %d", formResponse.Code)
	}

	crossOrigin := jsonRequest(http.MethodPost, "/auth/password/login",
		`{"identifier":"alice","password":"Sufficient-1Password"}`)
	crossOrigin.Header.Set("Origin", "https://evil.example")
	crossOriginResponse := httptest.NewRecorder()
	handler.ServeHTTP(crossOriginResponse, crossOrigin)
	if crossOriginResponse.Code != http.StatusForbidden {
		t.Fatalf("cross-origin request returned %d", crossOriginResponse.Code)
	}

	sameOrigin := jsonRequest(http.MethodPost, "/auth/password/login",
		`{"identifier":"alice","password":"Sufficient-1Password"}`)
	sameOrigin.Header.Set("Origin", "https://app.example")
	sameOriginResponse := httptest.NewRecorder()
	handler.ServeHTTP(sameOriginResponse, sameOrigin)
	if sameOriginResponse.Code != http.StatusUnauthorized {
		t.Fatalf("same-origin request with unknown user returned %d", sameOriginResponse.Code)
	}
}

func TestPasswordRegistrationValidatesInputAndCreatesSession(t *testing.T) {
	codec, _ := auth.NewSecretCodec("test authentication secret")
	passwordStore := &fakePasswordStore{}
	authenticationStore := &fakeAuthenticationStore{}
	handler := newPasswordTestHandler(passwordStore, authenticationStore, codec, true)
	register := func(body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, jsonRequest(http.MethodPost, "/auth/password/register", body))
		return response
	}

	for body, expected := range map[string]int{
		`{"username":"Bad_Name","email":"bob@example.com","password":"Sufficient-1Password"}`: http.StatusBadRequest,
		`{"username":"bob","email":"not-an-email","password":"Sufficient-1Password"}`:         http.StatusBadRequest,
		`{"username":"bob","email":"bob@example.com","password":"weak"}`:                      http.StatusBadRequest,
		`{"username":"bob","email":"bob@example.com","password":"Has-bob-Inside-1!"}`:         http.StatusBadRequest,
	} {
		if response := register(body); response.Code != expected {
			t.Fatalf("register %s returned %d, expected %d", body, response.Code, expected)
		}
	}
	if passwordStore.created != nil {
		t.Fatal("invalid registration reached the store")
	}

	response := register(`{"username":"BOB","email":"Bob@Example.com","password":"Sufficient-1Password","locale":"ja"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("expected registration to succeed, got %d: %s", response.Code, response.Body)
	}
	if passwordStore.created == nil || passwordStore.created.Username != "bob" ||
		passwordStore.created.Email != "bob@example.com" || passwordStore.created.Locale != "ja" {
		t.Fatalf("registration input was not normalized: %#v", passwordStore.created)
	}
	if passwordStore.created.PasswordHash == "" ||
		strings.Contains(passwordStore.created.PasswordHash, "Sufficient-1Password") {
		t.Fatal("password was not hashed before storage")
	}
	cookieNamed(t, response.Result().Cookies(), "lorehub_session")

	passwordStore.createError = platform.ErrUsernameTaken
	if response := register(`{"username":"bob","email":"bob@example.com","password":"Sufficient-1Password"}`); response.Code != http.StatusConflict {
		t.Fatalf("duplicate username returned %d", response.Code)
	}
	passwordStore.createError = platform.ErrEmailTaken
	if response := register(`{"username":"bob","email":"bob@example.com","password":"Sufficient-1Password"}`); response.Code != http.StatusConflict {
		t.Fatalf("duplicate email returned %d", response.Code)
	}
}

func TestPasswordRegistrationCanBeDisabled(t *testing.T) {
	codec, _ := auth.NewSecretCodec("test authentication secret")
	handler := newPasswordTestHandler(&fakePasswordStore{}, &fakeAuthenticationStore{}, codec, false)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, jsonRequest(http.MethodPost, "/auth/password/register",
		`{"username":"bob","email":"bob@example.com","password":"Sufficient-1Password"}`))
	if response.Code != http.StatusForbidden {
		t.Fatalf("disabled registration returned %d", response.Code)
	}
}

func TestChangePasswordRequiresSessionCSRFAndCurrentPassword(t *testing.T) {
	codec, _ := auth.NewSecretCodec("test authentication secret")
	passwordHash, _ := auth.HashPassword("Sufficient-1Password")
	passwordStore := &fakePasswordStore{
		credential: platform.PasswordCredential{
			UserID:       "user-1",
			Email:        "alice@example.com",
			PasswordHash: passwordHash,
		},
		hasCredential: true,
	}
	authenticationStore := &fakeAuthenticationStore{}
	handler := newPasswordTestHandler(passwordStore, authenticationStore, codec, true)

	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, jsonRequest(http.MethodPost, "/auth/password/login",
		`{"identifier":"alice@example.com","password":"Sufficient-1Password"}`))
	sessionCookie := cookieNamed(t, loginResponse.Result().Cookies(), "lorehub_session")
	csrfToken := codec.CSRFToken(sessionCookie.Value)

	change := func(body string, withCSRF bool) *httptest.ResponseRecorder {
		request := jsonRequest(http.MethodPut, "/api/v1/auth/password", body)
		request.AddCookie(sessionCookie)
		if withCSRF {
			request.Header.Set("X-CSRF-Token", csrfToken)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	anonymous := httptest.NewRecorder()
	handler.ServeHTTP(anonymous, jsonRequest(http.MethodPut, "/api/v1/auth/password",
		`{"currentPassword":"Sufficient-1Password","newPassword":"Another-2Password!"}`))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous change returned %d", anonymous.Code)
	}
	if response := change(`{"currentPassword":"Sufficient-1Password","newPassword":"Another-2Password!"}`,
		false); response.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF returned %d", response.Code)
	}
	if response := change(`{"currentPassword":"Wrong-1Password!","newPassword":"Another-2Password!"}`,
		true); response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current password returned %d", response.Code)
	}
	if response := change(`{"currentPassword":"Sufficient-1Password","newPassword":"weak"}`,
		true); response.Code != http.StatusBadRequest {
		t.Fatalf("weak new password returned %d", response.Code)
	}
	response := change(`{"currentPassword":"Sufficient-1Password","newPassword":"Another-2Password!"}`, true)
	if response.Code != http.StatusOK {
		t.Fatalf("expected password change to succeed, got %d: %s", response.Code, response.Body)
	}
	if passwordStore.setPasswordHash == "" || passwordStore.revokedOthers != 1 {
		t.Fatalf("password change did not persist and revoke other sessions: %#v", passwordStore)
	}
}

func TestProvidersReflectConfiguredAuthentication(t *testing.T) {
	codec, _ := auth.NewSecretCodec("test authentication secret")

	listProviders := func(handler http.Handler) []providerResponse {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers", nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("providers returned %d", response.Code)
		}
		var body struct {
			Providers []providerResponse `json:"providers"`
		}
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return body.Providers
	}

	passwordOnly := newPasswordTestHandler(&fakePasswordStore{}, &fakeAuthenticationStore{}, codec, true)
	providers := listProviders(passwordOnly)
	if len(providers) != 1 || providers[0].ID != "password" || providers[0].Kind != "form" {
		t.Fatalf("unexpected password-only providers: %#v", providers)
	}

	oidcOnly := newAuthTestHandler(&fakeLoginProvider{}, &fakeAuthenticationStore{}, codec,
		auth.DisabledAuthenticator{})
	providers = listProviders(oidcOnly)
	if len(providers) != 1 || providers[0].ID != "password" || providers[0].Kind != "redirect" {
		t.Fatalf("unexpected OIDC-only providers: %#v", providers)
	}
}

func TestOIDCLoginRedirectsToBrandedPageWhenPasswordOnly(t *testing.T) {
	codec, _ := auth.NewSecretCodec("test authentication secret")
	handler := newPasswordTestHandler(&fakePasswordStore{}, &fakeAuthenticationStore{}, codec, true)
	request := httptest.NewRequest(http.MethodGet, "/auth/login?return_to=%2Fdashboard&prompt=create", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", response.Code)
	}
	location := response.Header().Get("Location")
	if !strings.HasPrefix(location, "/auth/start?") || !strings.Contains(location, "return_to=%2Fdashboard") ||
		!strings.Contains(location, "prompt=create") {
		t.Fatalf("unexpected redirect target: %s", location)
	}
}
