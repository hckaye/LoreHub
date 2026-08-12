package httpapi

import (
	"context"
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

type fakeActionsEnvironmentStore struct {
	environments []runner.EnvironmentRecord
	deployments  []runner.DeploymentRecord
	access       runner.RepositoryAccess
	actorID      string
	name         string
	input        runner.EnvironmentInput
	approved     bool
	deleted      bool
}

func (store *fakeActionsEnvironmentStore) ListEnvironments(
	_ context.Context,
	access runner.RepositoryAccess,
	actorID string,
) ([]runner.EnvironmentRecord, error) {
	store.access = access
	store.actorID = actorID
	return store.environments, nil
}

func (store *fakeActionsEnvironmentStore) UpsertEnvironment(
	_ context.Context,
	access runner.RepositoryAccess,
	actorID string,
	name string,
	input runner.EnvironmentInput,
) (runner.EnvironmentRecord, error) {
	store.access = access
	store.actorID = actorID
	store.name = name
	store.input = input
	return runner.EnvironmentRecord{ID: "environment-1", Name: name}, nil
}

func (store *fakeActionsEnvironmentStore) DeleteEnvironment(
	_ context.Context,
	access runner.RepositoryAccess,
	actorID string,
	name string,
) error {
	store.access = access
	store.actorID = actorID
	store.name = name
	store.deleted = true
	return nil
}

func (store *fakeActionsEnvironmentStore) ListDeployments(
	_ context.Context,
	access runner.RepositoryAccess,
	actorID string,
	_ int,
) ([]runner.DeploymentRecord, error) {
	store.access = access
	store.actorID = actorID
	return store.deployments, nil
}

func (store *fakeActionsEnvironmentStore) ReviewDeployment(
	_ context.Context,
	access runner.RepositoryAccess,
	actorID string,
	deploymentID string,
	approved bool,
) (runner.DeploymentRecord, error) {
	store.access = access
	store.actorID = actorID
	store.name = deploymentID
	store.approved = approved
	return runner.DeploymentRecord{ID: deploymentID, Status: "queued"}, nil
}

func TestActionsEnvironmentHTTPRoutes(t *testing.T) {
	store := &fakeActionsEnvironmentStore{
		environments: []runner.EnvironmentRecord{{ID: "environment-1", Name: "Production"}},
		deployments:  []runner.DeploymentRecord{{ID: "deployment-1", EnvironmentName: "Production"}},
	}
	handler := newActionsEnvironmentHandler(t, store)

	response := serveActionsEnvironmentRequest(
		handler, http.MethodGet, "/api/v1/repositories/acme/demo/actions/environments", "",
	)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Production") {
		t.Fatalf("environment list status = %d, body=%s", response.Code, response.Body.String())
	}
	response = serveActionsEnvironmentRequest(
		handler,
		http.MethodPut,
		"/api/v1/repositories/acme/demo/actions/environments/Production",
		`{"waitTimerMinutes":15,"preventSelfReview":true,"reviewers":["alice"]}`,
	)
	if response.Code != http.StatusOK || store.actorID != "user-1" || store.name != "Production" ||
		store.input.WaitTimerMinutes != 15 || len(store.input.ReviewerUsernames) != 1 {
		t.Fatalf("environment update status = %d, store=%#v, body=%s", response.Code, store, response.Body.String())
	}
	response = serveActionsEnvironmentRequest(
		handler,
		http.MethodPost,
		"/api/v1/repositories/acme/demo/actions/deployments/deployment-1/reviews",
		`{"state":"approved"}`,
	)
	if response.Code != http.StatusOK || !store.approved || store.name != "deployment-1" {
		t.Fatalf("deployment review status = %d, store=%#v, body=%s", response.Code, store, response.Body.String())
	}
}

func TestActionsEnvironmentHTTPRejectsUnknownJSON(t *testing.T) {
	store := &fakeActionsEnvironmentStore{}
	handler := newActionsEnvironmentHandler(t, store)
	response := serveActionsEnvironmentRequest(
		handler,
		http.MethodPut,
		"/api/v1/repositories/acme/demo/actions/environments/Production",
		`{"waitTimerMinutes":0,"unknown":true}`,
	)
	if response.Code != http.StatusBadRequest || store.name != "" {
		t.Fatalf("unknown JSON status = %d, store=%#v, body=%s", response.Code, store, response.Body.String())
	}
}

func newActionsEnvironmentHandler(t *testing.T, environments ActionsEnvironmentStore) http.Handler {
	t.Helper()
	repositories := &fakeActionsContextRepositoryStore{access: runner.RepositoryAccess{
		ID: "repository-1", OrganizationID: "organization-1", Owner: "acme", Slug: "demo", CanRead: true,
	}}
	return New(
		fakeStore{user: platform.User{ID: "user-1", Username: "alice"}},
		fakeLore{},
		staticAuthenticator{principal: auth.Principal{Subject: "subject-1"}},
		healthy{},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithActions(repositories),
		WithActionsEnvironments(environments),
	)
}

func serveActionsEnvironmentRequest(
	handler http.Handler,
	method string,
	path string,
	body string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer token")
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
