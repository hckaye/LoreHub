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
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type repositoryInvitationHTTPStore struct {
	AuthorizationStore
	page              platform.RepositoryInvitationPage
	created           platform.CreateRepositoryInvitationInput
	createdOwner      string
	createdRepository string
	responseID        string
	responseAccepted  bool
	revokedID         string
	updatedUsername   string
	updatedRole       string
	revokedUsername   string
}

func (store *repositoryInvitationHTTPStore) ListRepositoryInvitations(
	context.Context,
	platform.User,
	string,
	string,
	int,
	int,
) (platform.RepositoryInvitationPage, error) {
	return store.page, nil
}

func (store *repositoryInvitationHTTPStore) ListRepositoryInvitationsForUser(
	context.Context,
	platform.User,
	int,
	int,
) (platform.RepositoryInvitationPage, error) {
	return store.page, nil
}

func (store *repositoryInvitationHTTPStore) CreateRepositoryInvitation(
	_ context.Context,
	_ platform.User,
	owner string,
	repository string,
	input platform.CreateRepositoryInvitationInput,
) (platform.RepositoryInvitation, error) {
	store.createdOwner = owner
	store.createdRepository = repository
	store.created = input
	return store.page.Invitations[0], nil
}

func (store *repositoryInvitationHTTPStore) RevokeRepositoryInvitation(
	_ context.Context,
	_ platform.User,
	_ string,
	_ string,
	invitationID string,
) error {
	store.revokedID = invitationID
	return nil
}

func (store *repositoryInvitationHTTPStore) RespondRepositoryInvitation(
	_ context.Context,
	_ platform.User,
	invitationID string,
	accept bool,
) (platform.RepositoryInvitation, error) {
	store.responseID = invitationID
	store.responseAccepted = accept
	return store.page.Invitations[0], nil
}

func (store *repositoryInvitationHTTPStore) UpdateRepositoryCollaboratorRole(
	_ context.Context,
	_ platform.User,
	_ string,
	_ string,
	username string,
	role string,
) (platform.Collaborator, error) {
	store.updatedUsername = username
	store.updatedRole = role
	return platform.Collaborator{Username: username, Role: role, Active: true, Source: "direct"}, nil
}

func (store *repositoryInvitationHTTPStore) RevokeRepositoryCollaborator(
	_ context.Context,
	_ platform.User,
	_ string,
	_ string,
	username string,
) (platform.Collaborator, error) {
	store.revokedUsername = username
	return platform.Collaborator{Username: username, Role: "read", Active: false, Source: "direct"}, nil
}

func TestRepositoryInvitationHTTPUsesStrictInputAndCSRF(t *testing.T) {
	handler, sessionCookie, csrf, store := repositoryInvitationTestHandler(t)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/repositories/acme/lore/invitations?page=1&per_page=20&extra=true",
		nil,
	)
	request.AddCookie(sessionCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown invitation query response=%d %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(
		http.MethodGet,
		"/api/v1/repositories/acme/lore/invitations?page=1&page=2",
		nil,
	)
	request.AddCookie(sessionCookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("duplicate invitation query response=%d %s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/repositories/acme/lore/invitations", nil)
	request.URL.RawQuery = "page=%zz"
	request.AddCookie(sessionCookie)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("malformed invitation query response=%d %s", response.Code, response.Body.String())
	}
	create := func(body string, withCSRF bool) *httptest.ResponseRecorder {
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/repositories/acme/lore/invitations",
			strings.NewReader(body),
		)
		request.Header.Set("Content-Type", "application/json")
		if withCSRF {
			request.Header.Set("X-CSRF-Token", csrf)
		}
		request.AddCookie(sessionCookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	response = create(`{"username":"bob","role":"write"}`, false)
	if response.Code != http.StatusForbidden {
		t.Fatalf("invitation without CSRF response=%d %s", response.Code, response.Body.String())
	}
	response = create(`{"username":"bob","role":"write","active":true}`, true)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invitation with unknown field response=%d %s", response.Code, response.Body.String())
	}
	response = create(`{"username":"bob","role":"write"}`, true)
	if response.Code != http.StatusCreated || store.createdOwner != "acme" ||
		store.createdRepository != "lore" || store.created.Username != "bob" || store.created.Role != "write" {
		t.Fatalf("create repository invitation response=%d store=%+v body=%s", response.Code, store, response.Body)
	}
}

func TestRepositoryInvitationHTTPResponsesAndCollaboratorUpdate(t *testing.T) {
	handler, sessionCookie, csrf, store := repositoryInvitationTestHandler(t)
	mutate := func(method string, target string, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", csrf)
		request.AddCookie(sessionCookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	invitationID := "11111111-1111-4111-8111-111111111111"
	response := mutate(
		http.MethodPost,
		"/api/v1/account/repository-invitations/"+invitationID+"/accept",
		"{}",
	)
	if response.Code != http.StatusOK || store.responseID != invitationID || !store.responseAccepted {
		t.Fatalf("accept invitation response=%d store=%+v body=%s", response.Code, store, response.Body)
	}
	response = mutate(
		http.MethodPost,
		"/api/v1/account/repository-invitations/"+invitationID+"/decline",
		"{}",
	)
	if response.Code != http.StatusOK || store.responseID != invitationID || store.responseAccepted {
		t.Fatalf("decline invitation response=%d store=%+v body=%s", response.Code, store, response.Body)
	}
	response = mutate(
		http.MethodPost,
		"/api/v1/account/repository-invitations/"+invitationID+"/accept",
		`{"unexpected":true}`,
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("accept invitation unknown body response=%d body=%s", response.Code, response.Body)
	}
	response = mutate(
		http.MethodDelete,
		"/api/v1/repositories/acme/lore/invitations/"+invitationID,
		"",
	)
	if response.Code != http.StatusNoContent || store.revokedID != invitationID {
		t.Fatalf("revoke invitation response=%d store=%+v body=%s", response.Code, store, response.Body)
	}
	response = mutate(
		http.MethodPut,
		"/api/v1/repositories/acme/lore/collaborators/bob",
		`{"role":"maintain","active":true}`,
	)
	if response.Code != http.StatusOK || store.updatedUsername != "bob" || store.updatedRole != "maintain" {
		t.Fatalf("update collaborator response=%d store=%+v body=%s", response.Code, store, response.Body)
	}
	response = mutate(
		http.MethodDelete,
		"/api/v1/repositories/acme/lore/collaborators/bob",
		"",
	)
	if response.Code != http.StatusOK || store.revokedUsername != "bob" {
		t.Fatalf("revoke collaborator response=%d store=%+v body=%s", response.Code, store, response.Body)
	}
}

func repositoryInvitationTestHandler(
	t *testing.T,
) (http.Handler, *http.Cookie, string, *repositoryInvitationHTTPStore) {
	t.Helper()
	codec, err := auth.NewSecretCodec("repository invitation HTTP test secret")
	if err != nil {
		t.Fatal(err)
	}
	authenticationStore := &fakeAuthenticationStore{}
	sessionCookie, csrf := prepareSessionCookie(t, authenticationStore, codec)
	invitation := platform.RepositoryInvitation{
		ID:                    "11111111-1111-4111-8111-111111111111",
		OrganizationID:        "22222222-2222-4222-8222-222222222222",
		RepositoryID:          "33333333-3333-4333-8333-333333333333",
		Owner:                 "acme",
		Repository:            "lore",
		RepositoryDisplayName: "Lore",
		InviteeUserID:         "44444444-4444-4444-8444-444444444444",
		InviteeUsername:       "bob",
		InviteeDisplayName:    "Bob",
		InvitedByUserID:       "user-1",
		InvitedByUsername:     "alice",
		InvitedByDisplayName:  "Alice",
		Role:                  "write",
		Status:                "pending",
		ExpiresAt:             time.Now().Add(7 * 24 * time.Hour),
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}
	store := &repositoryInvitationHTTPStore{page: platform.RepositoryInvitationPage{
		Invitations: []platform.RepositoryInvitation{invitation}, Total: 1, Page: 1, PerPage: 20,
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
				Name: "lorehub_session", Path: "/",
			},
		}),
		WithAuthorization(store),
	)
	return handler, sessionCookie, csrf, store
}
