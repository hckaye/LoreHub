package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
	"github.com/lorehub/lorehub/services/api/internal/runner"
)

type fakeActionsContextStore struct {
	entries      []runner.ExecutionContextEntry
	listErr      error
	upsertErr    error
	deleteErr    error
	listedActor  string
	mutatedActor string
	deletedActor string
	scope        runner.ExecutionContextScope
	name         string
	value        string
	secret       bool
}

func (store *fakeActionsContextStore) ListExecutionContextEntries(
	_ context.Context,
	actorID string,
	scope runner.ExecutionContextScope,
) ([]runner.ExecutionContextEntry, error) {
	store.listedActor = actorID
	store.scope = scope
	return append([]runner.ExecutionContextEntry(nil), store.entries...), store.listErr
}

func (store *fakeActionsContextStore) UpsertVariable(
	_ context.Context,
	scope runner.ExecutionContextScope,
	name string,
	value string,
	actorID string,
) (runner.ExecutionContextEntry, error) {
	store.recordUpsert(scope, name, value, actorID, false)
	return runner.ExecutionContextEntry{Name: name, Value: value}, store.upsertErr
}

func (store *fakeActionsContextStore) UpsertSecret(
	_ context.Context,
	scope runner.ExecutionContextScope,
	name string,
	value string,
	actorID string,
) (runner.ExecutionContextEntry, error) {
	store.recordUpsert(scope, name, value, actorID, true)
	return runner.ExecutionContextEntry{Name: name, Secret: true, Value: "must-not-leak"}, store.upsertErr
}

func (store *fakeActionsContextStore) DeleteExecutionContextEntry(
	_ context.Context,
	actorID string,
	scope runner.ExecutionContextScope,
	name string,
	secret bool,
) error {
	store.deletedActor = actorID
	store.scope = scope
	store.name = name
	store.secret = secret
	return store.deleteErr
}

func (store *fakeActionsContextStore) recordUpsert(
	scope runner.ExecutionContextScope,
	name string,
	value string,
	actorID string,
	secret bool,
) {
	store.mutatedActor = actorID
	store.scope = scope
	store.name = name
	store.value = value
	store.secret = secret
}

type fakeActionsContextIdentityStore struct {
	IdentityStore
	organization platform.OrganizationView
	err          error
	actor        *platform.User
}

func (store *fakeActionsContextIdentityStore) Organization(
	_ context.Context,
	actor *platform.User,
	_ string,
) (platform.OrganizationView, error) {
	store.actor = actor
	return store.organization, store.err
}

type fakeActionsContextRepositoryStore struct {
	ActionsStore
	access  runner.RepositoryAccess
	err     error
	actorID string
}

func (store *fakeActionsContextRepositoryStore) RepositoryForActions(
	_ context.Context,
	_ string,
	_ string,
	actorID string,
) (runner.RepositoryAccess, error) {
	store.actorID = actorID
	return store.access, store.err
}

func TestOrganizationActionsSettingsListHidesSecretValues(t *testing.T) {
	contextStore := &fakeActionsContextStore{entries: []runner.ExecutionContextEntry{
		{
			ID: "variable-1", Scope: runner.ExecutionContextScope{
				Kind: "organization", OrganizationID: "organization-1",
			},
			Name: "PUBLIC_NAME", Value: "visible",
		},
		{
			ID: "secret-1", Scope: runner.ExecutionContextScope{
				Kind: "organization", OrganizationID: "organization-1",
			},
			Name: "TOKEN", Secret: true, Value: "must-not-leak", KeyID: "primary",
		},
	}}
	identityStore := &fakeActionsContextIdentityStore{
		organization: platform.OrganizationView{ID: "organization-1", Slug: "acme"},
	}
	handler := newActionsContextHandler(t, contextStore, identityStore, nil)
	response := serveActionsContextRequest(
		handler,
		http.MethodGet,
		"/api/v1/organizations/acme/actions/settings",
		"",
	)
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d, body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Entries) != 2 || payload.Entries[0]["value"] != "visible" {
		t.Fatalf("unexpected entries: %#v", payload.Entries)
	}
	if _, exists := payload.Entries[1]["value"]; exists {
		t.Fatalf("secret value was returned: %#v", payload.Entries[1])
	}
	if payload.Entries[1]["id"] != "secret-1" || payload.Entries[1]["name"] != "TOKEN" ||
		payload.Entries[1]["keyId"] != "primary" {
		t.Fatalf("metadata keys are not lowerCamel: %#v", payload.Entries[1])
	}
	if _, exists := payload.Entries[1]["ID"]; exists {
		t.Fatalf("legacy exported-field JSON key was returned: %#v", payload.Entries[1])
	}
	scope, ok := payload.Entries[1]["scope"].(map[string]any)
	if !ok || scope["kind"] != "organization" || scope["organizationId"] != "organization-1" {
		t.Fatalf("scope keys are not lowerCamel: %#v", payload.Entries[1]["scope"])
	}
	if contextStore.listedActor != "user-1" || contextStore.scope.OrganizationID != "organization-1" {
		t.Fatalf("list identity was not resolved: actor=%q scope=%#v",
			contextStore.listedActor, contextStore.scope)
	}
	if identityStore.actor == nil || identityStore.actor.ID != "user-1" {
		t.Fatalf("organization lookup actor = %#v", identityStore.actor)
	}
}

func TestRepositoryActionsSettingsMutationsUseResolvedScope(t *testing.T) {
	repositoryStore := &fakeActionsContextRepositoryStore{access: runner.RepositoryAccess{
		ID: "repository-1", OrganizationID: "organization-1", Owner: "acme", Slug: "demo",
	}}
	for _, testCase := range []struct {
		name         string
		path         string
		wantKind     string
		wantEnv      string
		wantSecret   bool
		wantResponse string
		absent       string
	}{
		{
			name:         "repository variable",
			path:         "/api/v1/repositories/acme/demo/actions/settings/repository/variable/BUILD_MODE",
			wantKind:     "repository",
			wantResponse: `"value":"release"`,
		},
		{
			name:       "environment secret",
			path:       "/api/v1/repositories/acme/demo/actions/settings/environment/secret/TOKEN?environment=prod",
			wantKind:   "environment",
			wantEnv:    "prod",
			wantSecret: true,
			absent:     `"value"`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			contextStore := &fakeActionsContextStore{}
			handler := newActionsContextHandler(t, contextStore, nil, repositoryStore)
			response := serveActionsContextRequest(handler, http.MethodPut, testCase.path, `{"value":"release"}`)
			if response.Code != http.StatusOK {
				t.Fatalf("upsert status = %d, body=%s", response.Code, response.Body.String())
			}
			if testCase.wantResponse != "" && !strings.Contains(response.Body.String(), testCase.wantResponse) {
				t.Fatalf("upsert response = %s", response.Body.String())
			}
			if testCase.absent != "" && strings.Contains(response.Body.String(), testCase.absent) {
				t.Fatalf("secret response exposed a value: %s", response.Body.String())
			}
			if contextStore.scope.Kind != testCase.wantKind ||
				contextStore.scope.OrganizationID != "organization-1" ||
				contextStore.scope.RepositoryID != "repository-1" ||
				contextStore.scope.Environment != testCase.wantEnv ||
				contextStore.secret != testCase.wantSecret {
				t.Fatalf("unexpected mutation scope: %#v secret=%t", contextStore.scope, contextStore.secret)
			}
			if contextStore.mutatedActor != "user-1" || repositoryStore.actorID != "user-1" {
				t.Fatalf("mutation actors = %q, %q", contextStore.mutatedActor, repositoryStore.actorID)
			}
		})
	}
}

func TestRepositoryActionsSettingsRejectInvalidInput(t *testing.T) {
	repositoryStore := &fakeActionsContextRepositoryStore{access: runner.RepositoryAccess{
		ID: "repository-1", OrganizationID: "organization-1",
	}}
	for _, testCase := range []struct {
		name string
		path string
		body string
	}{
		{
			name: "environment name required",
			path: "/api/v1/repositories/acme/demo/actions/settings/environment/variable/NAME",
			body: `{"value":"ok"}`,
		},
		{
			name: "scope kind restricted",
			path: "/api/v1/repositories/acme/demo/actions/settings/organization/variable/NAME",
			body: `{"value":"ok"}`,
		},
		{
			name: "value kind restricted",
			path: "/api/v1/repositories/acme/demo/actions/settings/repository/token/NAME",
			body: `{"value":"ok"}`,
		},
		{
			name: "unknown JSON field",
			path: "/api/v1/repositories/acme/demo/actions/settings/repository/variable/NAME",
			body: `{"value":"ok","extra":true}`,
		},
		{
			name: "value required",
			path: "/api/v1/repositories/acme/demo/actions/settings/repository/variable/NAME",
			body: `{}`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			contextStore := &fakeActionsContextStore{}
			handler := newActionsContextHandler(t, contextStore, nil, repositoryStore)
			response := serveActionsContextRequest(handler, http.MethodPut, testCase.path, testCase.body)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("invalid request status = %d, body=%s", response.Code, response.Body.String())
			}
			if contextStore.mutatedActor != "" {
				t.Fatal("invalid request reached the execution context mutation store")
			}
		})
	}
}

func TestActionsSettingsMutationBodyIsBounded(t *testing.T) {
	contextStore := &fakeActionsContextStore{}
	repositoryStore := &fakeActionsContextRepositoryStore{access: runner.RepositoryAccess{
		ID: "repository-1", OrganizationID: "organization-1",
	}}
	handler := newActionsContextHandler(t, contextStore, nil, repositoryStore)
	body := `{"value":"` + strings.Repeat("x", maxRequestBody) + `"}`
	response := serveActionsContextRequest(
		handler,
		http.MethodPut,
		"/api/v1/repositories/acme/demo/actions/settings/repository/variable/NAME",
		body,
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("oversized request status = %d, body=%s", response.Code, response.Body.String())
	}
	if contextStore.mutatedActor != "" {
		t.Fatal("oversized request reached the execution context mutation store")
	}
}

func TestActionsSettingsMutationPreservesCookieCSRF(t *testing.T) {
	codec, err := auth.NewSecretCodec("Actions settings CSRF test secret")
	if err != nil {
		t.Fatal(err)
	}
	authenticationStore := &fakeAuthenticationStore{}
	contextStore := &fakeActionsContextStore{}
	repositoryStore := &fakeActionsContextRepositoryStore{access: runner.RepositoryAccess{
		ID: "repository-1", OrganizationID: "organization-1",
	}}
	handler := New(
		fakeStore{user: platform.User{ID: "user-1", Username: "alice"}},
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
				Name: "lorehub_session", Path: "/", Secure: true,
			},
		}),
		WithActionsExecutionContext(contextStore),
		WithActions(repositoryStore),
	)
	cookie, csrf := prepareSessionCookie(t, authenticationStore, codec)
	path := "/api/v1/repositories/acme/demo/actions/settings/repository/variable/NAME"

	request := httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"value":"release"}`))
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "csrf_failed") {
		t.Fatalf("missing CSRF status = %d, body=%s", response.Code, response.Body.String())
	}
	if contextStore.mutatedActor != "" {
		t.Fatal("request without CSRF reached the execution context mutation store")
	}

	request = httptest.NewRequest(http.MethodPut, path, strings.NewReader(`{"value":"release"}`))
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(cookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("valid CSRF status = %d, body=%s", response.Code, response.Body.String())
	}
}

func TestActionsSettingsErrorsRemainDistinct(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		err    error
		status int
	}{
		{"invalid", runner.ErrExecutionContextInvalid, http.StatusBadRequest},
		{"unauthorized", runner.ErrExecutionContextUnauthorized, http.StatusForbidden},
		{"not found", runner.ErrExecutionContextEntryNotFound, http.StatusNotFound},
		{"internal", errors.New("database unavailable"), http.StatusInternalServerError},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			contextStore := &fakeActionsContextStore{listErr: testCase.err}
			identityStore := &fakeActionsContextIdentityStore{
				organization: platform.OrganizationView{ID: "organization-1"},
			}
			handler := newActionsContextHandler(t, contextStore, identityStore, nil)
			response := serveActionsContextRequest(
				handler,
				http.MethodGet,
				"/api/v1/organizations/acme/actions/settings",
				"",
			)
			if response.Code != testCase.status {
				t.Fatalf("error status = %d, want %d, body=%s",
					response.Code, testCase.status, response.Body.String())
			}
		})
	}
}

func TestActionsSettingsDeleteMapsEntryNotFound(t *testing.T) {
	contextStore := &fakeActionsContextStore{deleteErr: runner.ErrExecutionContextEntryNotFound}
	repositoryStore := &fakeActionsContextRepositoryStore{access: runner.RepositoryAccess{
		ID: "repository-1", OrganizationID: "organization-1",
	}}
	handler := newActionsContextHandler(t, contextStore, nil, repositoryStore)
	response := serveActionsContextRequest(
		handler,
		http.MethodDelete,
		"/api/v1/repositories/acme/demo/actions/settings/repository/secret/TOKEN",
		"",
	)
	if response.Code != http.StatusNotFound {
		t.Fatalf("delete status = %d, body=%s", response.Code, response.Body.String())
	}
	if contextStore.deletedActor != "user-1" || !contextStore.secret {
		t.Fatalf("delete identity or kind lost: actor=%q secret=%t",
			contextStore.deletedActor, contextStore.secret)
	}
}

func newActionsContextHandler(
	t *testing.T,
	contextStore ActionsExecutionContextStore,
	identityStore IdentityStore,
	repositoryStore ActionsStore,
) http.Handler {
	t.Helper()
	options := []Option{WithActionsExecutionContext(contextStore)}
	if identityStore != nil {
		options = append(options, WithIdentityStore(identityStore))
	}
	if repositoryStore != nil {
		options = append(options, WithActions(repositoryStore))
	}
	return New(
		fakeStore{user: platform.User{ID: "user-1", Username: "alice"}},
		fakeLore{},
		staticAuthenticator{principal: auth.Principal{Subject: "subject-1"}},
		healthy{},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		options...,
	)
}

func serveActionsContextRequest(
	handler http.Handler,
	method string,
	path string,
	body string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
