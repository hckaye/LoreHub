package wiki

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type fakeWikiStore struct {
	createErr error
	updateErr error
}

func (store *fakeWikiStore) List(context.Context, string, string, int) ([]PageSummary, error) {
	return []PageSummary{{ID: "page-1", Slug: "guide", Title: "Guide", Version: 1}}, nil
}

func (store *fakeWikiStore) Get(context.Context, string, string) (Page, error) {
	return Page{PageSummary: PageSummary{ID: "page-1", Slug: "guide", Title: "Guide", Version: 1}}, nil
}

func (store *fakeWikiStore) History(context.Context, string, string, int) ([]Revision, error) {
	return []Revision{{Version: 1, Slug: "guide", Title: "Guide"}}, nil
}

func (store *fakeWikiStore) Revision(context.Context, string, string, int) (Revision, error) {
	return Revision{Version: 1, Slug: "guide", Title: "Guide"}, nil
}

func (store *fakeWikiStore) Create(
	_ context.Context,
	actor platform.User,
	_ RepositoryRef,
	input CreateInput,
) (Page, error) {
	if store.createErr != nil {
		return Page{}, store.createErr
	}
	return Page{
		PageSummary: PageSummary{
			ID: "page-1", Slug: "guide", Title: input.Title, Version: 1,
			CreatedBy: actor.Username, UpdatedBy: actor.Username,
		},
		Body: input.Body,
	}, nil
}

func (store *fakeWikiStore) Update(
	context.Context,
	platform.User,
	RepositoryRef,
	string,
	UpdateInput,
) (Page, error) {
	if store.updateErr != nil {
		return Page{}, store.updateErr
	}
	return Page{PageSummary: PageSummary{ID: "page-1", Slug: "guide", Version: 2}}, nil
}

func (store *fakeWikiStore) Delete(context.Context, platform.User, RepositoryRef, string, int) error {
	return nil
}

type fakeWikiRepositories struct {
	permission collab.Permission
}

func (repositories fakeWikiRepositories) LookupRepository(
	context.Context,
	*platform.User,
	string,
	string,
) (collab.Repository, error) {
	return collab.Repository{
		ID: "repository-1", OrganizationID: "organization-1", Owner: "acme", Slug: "game",
	}, nil
}

func (repositories fakeWikiRepositories) RepositoryPermission(
	context.Context,
	platform.User,
	collab.Repository,
) (collab.Access, error) {
	return collab.Access{Permission: repositories.permission}, nil
}

type fakeWikiActors struct {
	actor *platform.User
}

func (actors fakeWikiActors) ResolveActor(writer http.ResponseWriter, _ *http.Request) (platform.User, bool) {
	if actors.actor == nil {
		writeProblem(writer, http.StatusUnauthorized, "unauthorized", "Authentication is required")
		return platform.User{}, false
	}
	return *actors.actor, true
}

func (actors fakeWikiActors) ResolveOptionalActor(
	_ http.ResponseWriter,
	_ *http.Request,
) (*platform.User, bool) {
	return actors.actor, true
}

func TestWikiRoutesExposeAnonymousReadsAndProtectedWrites(t *testing.T) {
	actor := platform.User{ID: "user-1", Username: "alice"}
	store := &fakeWikiStore{}
	anonymous := wikiTestHandler(store, collab.PermNone, nil)
	response := performWikiRequest(anonymous, http.MethodGet, "/api/v1/repositories/acme/game/wiki", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"viewerCanWrite":false`) {
		t.Fatalf("anonymous list = %d %s", response.Code, response.Body.String())
	}
	reader := wikiTestHandler(store, collab.PermRead, &actor)
	response = performWikiRequest(reader, http.MethodPost, "/api/v1/repositories/acme/game/wiki", `{}`)
	if response.Code != http.StatusForbidden {
		t.Fatalf("reader create status = %d, want 403", response.Code)
	}
	writer := wikiTestHandler(store, collab.PermWrite, &actor)
	response = performWikiRequest(
		writer,
		http.MethodPost,
		"/api/v1/repositories/acme/game/wiki",
		`{"title":"Guide","body":"# Guide","editSummary":""}`,
	)
	if response.Code != http.StatusCreated || response.Header().Get("Location") == "" {
		t.Fatalf("writer create = %d %s", response.Code, response.Body.String())
	}
}

func TestWikiRoutesRejectStrictJSONOversizeAndStaleUpdates(t *testing.T) {
	actor := platform.User{ID: "user-1", Username: "alice"}
	store := &fakeWikiStore{updateErr: platform.ErrConflict}
	handler := wikiTestHandler(store, collab.PermWrite, &actor)
	response := performWikiRequest(
		handler,
		http.MethodPost,
		"/api/v1/repositories/acme/game/wiki",
		`{"title":"Guide","unknown":true}`,
	)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d, want 400", response.Code)
	}
	oversize := `{"title":"Guide","body":"` + strings.Repeat("x", maxRequestBytes) + `"}`
	response = performWikiRequest(handler, http.MethodPost, "/api/v1/repositories/acme/game/wiki", oversize)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversize status = %d, want 413", response.Code)
	}
	response = performWikiRequest(
		handler,
		http.MethodPatch,
		"/api/v1/repositories/acme/game/wiki/guide",
		`{"body":"Changed","expectedVersion":1}`,
	)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "version_conflict") {
		t.Fatalf("stale update = %d %s", response.Code, response.Body.String())
	}
}

func wikiTestHandler(store Store, permission collab.Permission, actor *platform.User) http.Handler {
	mux := http.NewServeMux()
	Register(
		mux,
		store,
		fakeWikiRepositories{permission: permission},
		fakeWikiActors{actor: actor},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	return mux
}

func performWikiRequest(handler http.Handler, method string, path string, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
