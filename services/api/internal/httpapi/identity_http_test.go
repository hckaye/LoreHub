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

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func TestIdentityQueryHelpers(t *testing.T) {
	t.Parallel()
	if limit, err := queryLimit("", 20, 50); err != nil || limit != 20 {
		t.Fatalf("default limit = %d, %v", limit, err)
	}
	if _, err := queryLimit("0", 20, 50); err == nil {
		t.Fatal("zero limit should be rejected")
	}
	if value, err := optionalBool("true"); err != nil || !value {
		t.Fatalf("optional bool = %t, %v", value, err)
	}
	if validOptionalText(pointerToString("hello"), 4) {
		t.Fatal("overlong optional text should be rejected")
	}
	if !validVisibilityPointer(pointerToString("private")) || validVisibilityPointer(pointerToString("unknown")) {
		t.Fatal("visibility helper accepted an invalid value")
	}
	if !validOptionalURL(pointerToString("https://lore.example/profile")) ||
		validOptionalURL(pointerToString("javascript:alert(1)")) {
		t.Fatal("URL helper accepted an unsafe or rejected a safe URL")
	}
}

func TestConfiguredLoginProvidersAreReturnedWithoutSecrets(t *testing.T) {
	t.Parallel()
	codec, err := auth.NewSecretCodec("test authentication secret")
	if err != nil {
		t.Fatal(err)
	}
	provider := &fakeLoginProvider{}
	authenticationStore := &fakeAuthenticationStore{}
	handler := New(
		fakeStore{},
		fakeLore{},
		auth.DisabledAuthenticator{},
		healthy{},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithAuthentication(AuthOptions{
			LoginProvider: provider,
			LoginStore:    authenticationStore,
			SessionStore:  authenticationStore,
			Secrets:       codec,
			PublicOrigin:  "https://app.example",
			SessionCookie: SessionCookieOptions{Name: "session", Path: "/", Secure: true},
		}),
		WithConfiguredLoginProviders([]string{"google", "github", "facebook", "x", "github", "unknown"}),
	)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("providers status = %d, body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Providers []struct {
			ID string `json:"id"`
		} `json:"providers"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	providerIDs := make([]string, 0, len(payload.Providers))
	for _, provider := range payload.Providers {
		providerIDs = append(providerIDs, provider.ID)
	}
	if strings.Join(providerIDs, ",") != "password,google,github,facebook,x" {
		t.Fatalf("unexpected providers: %+v", payload.Providers)
	}
	for _, providerID := range []string{"google", "github", "facebook", "x"} {
		request = httptest.NewRequest(http.MethodGet, "/auth/login?provider="+providerID, nil)
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusFound || provider.providerHint != providerID {
			t.Fatalf("configured provider %q status=%d hint=%q", providerID, response.Code, provider.providerHint)
		}
		if strings.Contains(response.Body.String(), "secret") || strings.Contains(response.Body.String(), "client") {
			t.Fatalf("provider %q response contained credential text: %s", providerID, response.Body.String())
		}
	}
	request = httptest.NewRequest(http.MethodGet, "/auth/login?provider=unknown", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown provider status = %d, body=%s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/auth/login?provider=github&provider=github", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("duplicate provider status = %d, body=%s", response.Code, response.Body.String())
	}
	provider.providerHint = ""
	request = httptest.NewRequest(http.MethodGet, "/auth/login?provider=password&prompt=create", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusFound || provider.providerHint != "" || provider.prompt != auth.RegistrationPrompt {
		t.Fatalf("password registration status=%d hint=%q prompt=%q", response.Code, provider.providerHint, provider.prompt)
	}
}

func pointerToString(value string) *string {
	return &value
}

type repositorySettingsHTTPIdentityStore struct {
	IdentityStore
	err         error
	repository  platform.Repository
	calledActor platform.User
}

func (store *repositorySettingsHTTPIdentityStore) UpdateRepositorySettings(
	_ context.Context,
	actor platform.User,
	_ string,
	_ string,
	_ platform.UpdateRepositorySettingsInput,
) (platform.Repository, error) {
	store.calledActor = actor
	if store.err != nil {
		return platform.Repository{}, store.err
	}
	return store.repository, nil
}

func TestRepositorySettingsHTTPPreservesRBACDenialAndSuccess(t *testing.T) {
	codec, err := auth.NewSecretCodec("repository settings HTTP test secret")
	if err != nil {
		t.Fatal(err)
	}
	authenticationStore := &fakeAuthenticationStore{}
	deniedStore := &repositorySettingsHTTPIdentityStore{err: platform.ErrForbidden}
	newHandler := func(identityStore *repositorySettingsHTTPIdentityStore) http.Handler {
		return New(
			fakeStore{user: platform.User{ID: "user-1", Username: "alice"}},
			fakeLore{}, auth.DisabledAuthenticator{}, healthy{}, "",
			slog.New(slog.NewTextHandler(io.Discard, nil)),
			WithAuthentication(AuthOptions{
				SessionStore: authenticationStore,
				Secrets:      codec,
				PublicOrigin: "https://app.example",
				SessionCookie: SessionCookieOptions{
					Name: "lorehub_session", Path: "/", Secure: true,
				},
			}),
			WithIdentityStore(identityStore),
		)
	}
	requestBody := `{"displayName":"Updated repository"}`
	requestFor := func(handler http.Handler) *httptest.ResponseRecorder {
		cookie, csrf := prepareSessionCookie(t, authenticationStore, codec)
		request := httptest.NewRequest(http.MethodPatch,
			"/api/v1/repositories/acme/lore/settings", strings.NewReader(requestBody))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", csrf)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	response := requestFor(newHandler(deniedStore))
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "forbidden") {
		t.Fatalf("maintainer denial response = %d %s", response.Code, response.Body.String())
	}
	privateStore := &repositorySettingsHTTPIdentityStore{err: platform.ErrNotFound}
	response = requestFor(newHandler(privateStore))
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "not_found") {
		t.Fatalf("private repository denial response = %d %s", response.Code, response.Body.String())
	}
	allowedStore := &repositorySettingsHTTPIdentityStore{
		repository: platform.Repository{ID: "repository-1", Slug: "lore"},
	}
	response = requestFor(newHandler(allowedStore))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "repository-1") {
		t.Fatalf("allowed repository settings response = %d %s", response.Code, response.Body.String())
	}
	if allowedStore.calledActor.ID != "user-1" {
		t.Fatalf("settings update actor = %q, want authenticated actor", allowedStore.calledActor.ID)
	}
}
