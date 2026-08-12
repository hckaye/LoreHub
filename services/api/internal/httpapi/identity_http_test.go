package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
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
	trimmed := pointerToString("  Repository name  ")
	trimOptionalText(trimmed)
	if *trimmed != "Repository name" {
		t.Fatalf("trimmed repository setting = %q", *trimmed)
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
	err                 error
	repository          platform.Repository
	calledActor         platform.User
	input               platform.UpdateRepositorySettingsInput
	archiveOwner        string
	archiveRepository   string
	archiveConfirmation string
	archiveState        bool
}

func (store *repositorySettingsHTTPIdentityStore) SetRepositoryArchived(
	_ context.Context,
	actor platform.User,
	owner string,
	repository string,
	archived bool,
	confirmation string,
) (platform.Repository, error) {
	store.calledActor = actor
	store.archiveOwner = owner
	store.archiveRepository = repository
	store.archiveState = archived
	store.archiveConfirmation = confirmation
	if store.err != nil {
		return platform.Repository{}, store.err
	}
	return store.repository, nil
}

func (store *repositorySettingsHTTPIdentityStore) RepositoryForSettings(
	_ context.Context,
	actor platform.User,
	_ string,
	_ string,
) (platform.Repository, error) {
	store.calledActor = actor
	if store.err != nil {
		return platform.Repository{}, store.err
	}
	return store.repository, nil
}

func (store *repositorySettingsHTTPIdentityStore) UpdateRepositorySettings(
	_ context.Context,
	actor platform.User,
	_ string,
	_ string,
	input platform.UpdateRepositorySettingsInput,
) (platform.Repository, error) {
	store.calledActor = actor
	store.input = input
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
	requestBody := `{"displayName":"Updated repository","topics":["lore","ci-runner"]}`
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
	if allowedStore.input.Topics == nil || !slices.Equal(*allowedStore.input.Topics, []string{"lore", "ci-runner"}) {
		t.Fatalf("settings topics = %v", allowedStore.input.Topics)
	}
	invalidStore := &repositorySettingsHTTPIdentityStore{err: platform.ErrInvalidInput}
	response = requestFor(newHandler(invalidStore))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "invalid_input") {
		t.Fatalf("invalid topic response = %d %s", response.Code, response.Body.String())
	}
	cookie, csrf := prepareSessionCookie(t, authenticationStore, codec)
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/repositories/acme/lore/settings",
		strings.NewReader(`{"displayName":"   "}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	newHandler(allowedStore).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("blank repository name response = %d %s", response.Code, response.Body.String())
	}
	readFor := func(identityStore *repositorySettingsHTTPIdentityStore) *httptest.ResponseRecorder {
		cookie, _ := prepareSessionCookie(t, authenticationStore, codec)
		request := httptest.NewRequest(http.MethodGet, "/api/v1/repositories/acme/lore/settings", nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		newHandler(identityStore).ServeHTTP(response, request)
		return response
	}
	response = readFor(deniedStore)
	if response.Code != http.StatusForbidden {
		t.Fatalf("denied repository settings read = %d %s", response.Code, response.Body.String())
	}
	response = readFor(privateStore)
	if response.Code != http.StatusNotFound {
		t.Fatalf("private repository settings read = %d %s", response.Code, response.Body.String())
	}
	response = readFor(allowedStore)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "repository-1") {
		t.Fatalf("allowed repository settings read = %d %s", response.Code, response.Body.String())
	}
}

func TestRepositoryArchiveHTTPRequiresSessionCSRFAndConfirmation(t *testing.T) {
	codec, err := auth.NewSecretCodec("repository archive HTTP test secret")
	if err != nil {
		t.Fatal(err)
	}
	authenticationStore := &fakeAuthenticationStore{}
	identityStore := &repositorySettingsHTTPIdentityStore{
		repository: platform.Repository{ID: "repository-1", Owner: "acme", Slug: "lore"},
	}
	handler := New(
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
	requestFor := func(method string, body string, withCSRF bool) *httptest.ResponseRecorder {
		cookie, csrf := prepareSessionCookie(t, authenticationStore, codec)
		request := httptest.NewRequest(method, "/api/v1/repositories/acme/lore/archive", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		if withCSRF {
			request.Header.Set("X-CSRF-Token", csrf)
		}
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	response := requestFor(http.MethodPut, `{"confirmation":"acme/lore"}`, false)
	if response.Code != http.StatusForbidden {
		t.Fatalf("archive without CSRF = %d %s", response.Code, response.Body.String())
	}
	response = requestFor(http.MethodPut, `{"confirmation":"acme/lore","extra":true}`, true)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("archive with unknown JSON field = %d %s", response.Code, response.Body.String())
	}
	response = requestFor(http.MethodPut, `{"confirmation":"acme/lore"}`, true)
	if response.Code != http.StatusOK || !identityStore.archiveState {
		t.Fatalf("archive response = %d %s", response.Code, response.Body.String())
	}
	if identityStore.calledActor.ID != "user-1" || identityStore.archiveOwner != "acme" ||
		identityStore.archiveRepository != "lore" || identityStore.archiveConfirmation != "acme/lore" {
		t.Fatalf("archive request = %+v", identityStore)
	}
	response = requestFor(http.MethodDelete, `{"confirmation":"acme/lore"}`, true)
	if response.Code != http.StatusOK || identityStore.archiveState {
		t.Fatalf("unarchive response = %d %s", response.Code, response.Body.String())
	}
}
