package projects

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/collab"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type fakeProjectStore struct {
	list         func(string) ([]ProjectSummary, error)
	create       func(platform.User, RepositoryRef, ProjectInput) (Project, error)
	deleteColumn func(platform.User, RepositoryRef, int64, string) error
}

func (store *fakeProjectStore) List(_ context.Context, repoID string) ([]ProjectSummary, error) {
	return store.list(repoID)
}

func (*fakeProjectStore) Get(context.Context, string, int64) (Project, error) {
	return Project{}, platform.ErrNotFound
}

func (store *fakeProjectStore) Create(
	_ context.Context, actor platform.User, repo RepositoryRef, input ProjectInput,
) (Project, error) {
	return store.create(actor, repo, input)
}

func (*fakeProjectStore) Update(
	context.Context, platform.User, RepositoryRef, int64, ProjectUpdate,
) (Project, error) {
	return Project{}, platform.ErrNotFound
}

func (*fakeProjectStore) Delete(context.Context, platform.User, RepositoryRef, int64) error {
	return platform.ErrNotFound
}

func (*fakeProjectStore) CreateColumn(
	context.Context, platform.User, RepositoryRef, int64, ColumnInput,
) (Project, error) {
	return Project{}, platform.ErrNotFound
}

func (*fakeProjectStore) UpdateColumn(
	context.Context, platform.User, RepositoryRef, int64, string, ColumnInput,
) (Project, error) {
	return Project{}, platform.ErrNotFound
}

func (store *fakeProjectStore) DeleteColumn(
	_ context.Context, actor platform.User, repo RepositoryRef, number int64, columnID string,
) error {
	return store.deleteColumn(actor, repo, number, columnID)
}

func (*fakeProjectStore) CreateItem(
	context.Context, platform.User, RepositoryRef, int64, ItemInput,
) (Project, error) {
	return Project{}, platform.ErrNotFound
}

func (*fakeProjectStore) UpdateItem(
	context.Context, platform.User, RepositoryRef, int64, string, ItemUpdate,
) (Project, error) {
	return Project{}, platform.ErrNotFound
}

func (*fakeProjectStore) DeleteItem(context.Context, platform.User, RepositoryRef, int64, string) error {
	return platform.ErrNotFound
}

type fakeRepositories struct {
	permission collab.Permission
}

func (fakeRepositories) LookupRepository(
	_ context.Context, _ *platform.User, owner string, slug string,
) (collab.Repository, error) {
	return collab.Repository{
		ID: uuid.NewString(), OrganizationID: uuid.NewString(), Owner: owner, Slug: slug, Visibility: "public",
	}, nil
}

func (repositories fakeRepositories) RepositoryPermission(
	context.Context, platform.User, collab.Repository,
) (collab.Access, error) {
	return collab.Access{Permission: repositories.permission}, nil
}

type fakeActors struct {
	user *platform.User
}

func (actors fakeActors) ResolveActor(writer http.ResponseWriter, _ *http.Request) (platform.User, bool) {
	if actors.user == nil {
		writeProblem(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required")
		return platform.User{}, false
	}
	return *actors.user, true
}

func (actors fakeActors) ResolveOptionalActor(
	_ http.ResponseWriter, _ *http.Request,
) (*platform.User, bool) {
	return actors.user, true
}

func TestListProjectsAllowsAnonymousRepositoryReader(t *testing.T) {
	store := &fakeProjectStore{list: func(repoID string) ([]ProjectSummary, error) {
		if repoID == "" {
			t.Fatal("repository id is empty")
		}
		return []ProjectSummary{{Number: 3, Title: "Release"}}, nil
	}}
	handler := projectHandler(store, fakeRepositories{permission: collab.PermRead}, fakeActors{})
	response := requestProject(t, handler, http.MethodGet, "/api/v1/repositories/acme/game/projects", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"title":"Release"`) ||
		!strings.Contains(response.Body.String(), `"viewerCanWrite":false`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestCreateProjectRequiresWritePermission(t *testing.T) {
	user := &platform.User{ID: uuid.NewString(), Username: "reader"}
	store := &fakeProjectStore{create: func(
		platform.User, RepositoryRef, ProjectInput,
	) (Project, error) {
		t.Fatal("create must not be called")
		return Project{}, nil
	}}
	handler := projectHandler(store, fakeRepositories{permission: collab.PermRead}, fakeActors{user: user})
	response := requestProject(t, handler, http.MethodPost, "/api/v1/repositories/acme/game/projects",
		`{"title":"Release","description":"","state":"open"}`)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestCreateProjectReturnsLocation(t *testing.T) {
	user := &platform.User{ID: uuid.NewString(), Username: "writer"}
	store := &fakeProjectStore{create: func(
		actor platform.User, repo RepositoryRef, input ProjectInput,
	) (Project, error) {
		if actor.ID != user.ID || repo.ID == "" || input.Title != "Release" {
			t.Fatalf("unexpected create input: actor=%+v repo=%+v input=%+v", actor, repo, input)
		}
		return Project{ProjectSummary: ProjectSummary{Number: 4, Title: input.Title}}, nil
	}}
	handler := projectHandler(store, fakeRepositories{permission: collab.PermWrite}, fakeActors{user: user})
	path := "/api/v1/repositories/acme/game/projects"
	response := requestProject(t, handler, http.MethodPost, path,
		`{"title":"Release","description":"Ship","state":"open"}`)
	if response.Code != http.StatusCreated || response.Header().Get("Location") != path+"/4" {
		t.Fatalf("response = %d location=%q body=%s", response.Code,
			response.Header().Get("Location"), response.Body.String())
	}
}

func TestCreateProjectRejectsUnknownJSONFields(t *testing.T) {
	user := &platform.User{ID: uuid.NewString(), Username: "writer"}
	store := &fakeProjectStore{create: func(
		platform.User, RepositoryRef, ProjectInput,
	) (Project, error) {
		t.Fatal("create must not be called")
		return Project{}, nil
	}}
	handler := projectHandler(store, fakeRepositories{permission: collab.PermWrite}, fakeActors{user: user})
	response := requestProject(t, handler, http.MethodPost, "/api/v1/repositories/acme/game/projects",
		`{"title":"Release","unexpected":true}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestDeleteNonEmptyColumnReturnsConflict(t *testing.T) {
	user := &platform.User{ID: uuid.NewString(), Username: "writer"}
	store := &fakeProjectStore{deleteColumn: func(
		platform.User, RepositoryRef, int64, string,
	) error {
		return ErrColumnNotEmpty
	}}
	handler := projectHandler(store, fakeRepositories{permission: collab.PermWrite}, fakeActors{user: user})
	path := "/api/v1/repositories/acme/game/projects/1/columns/" + uuid.NewString()
	response := requestProject(t, handler, http.MethodDelete, path, "")
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "column_not_empty") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func projectHandler(store Store, repositories RepositoryStore, actors collab.ActorResolver) http.Handler {
	mux := http.NewServeMux()
	Register(mux, store, repositories, actors, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return mux
}

func requestProject(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
