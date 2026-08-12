package discussions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/collab"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type discussionHTTPStore struct {
	categories []Category
	deleted    bool
	created    bool
}

func (store *discussionHTTPStore) ListCategories(context.Context, string) ([]Category, error) {
	return store.categories, nil
}

func (store *discussionHTTPStore) CreateCategory(
	context.Context, platform.User, RepositoryRef, CategoryInput,
) (Category, error) {
	return Category{}, nil
}

func (store *discussionHTTPStore) UpdateCategory(
	context.Context, platform.User, RepositoryRef, string, CategoryInput,
) (Category, error) {
	return Category{}, nil
}

func (store *discussionHTTPStore) DeleteCategory(context.Context, platform.User, RepositoryRef, string) error {
	return nil
}

func (store *discussionHTTPStore) List(context.Context, string, string, ListFilter) (Page, error) {
	return Page{}, nil
}

func (store *discussionHTTPStore) Get(context.Context, string, int64, string, int, int) (Discussion, error) {
	return Discussion{}, nil
}

func (store *discussionHTTPStore) Create(
	context.Context, platform.User, RepositoryRef, CreateInput,
) (Discussion, error) {
	store.created = true
	return Discussion{}, nil
}

func (store *discussionHTTPStore) Update(
	context.Context, platform.User, RepositoryRef, int64, UpdateInput,
) (Discussion, error) {
	return Discussion{}, nil
}

func (store *discussionHTTPStore) Delete(context.Context, platform.User, RepositoryRef, int64) error {
	store.deleted = true
	return nil
}

func (store *discussionHTTPStore) CreateComment(
	context.Context, platform.User, RepositoryRef, int64, *string, string,
) (Comment, error) {
	return Comment{}, nil
}

func (store *discussionHTTPStore) UpdateComment(
	context.Context, platform.User, RepositoryRef, int64, string, string,
) (Comment, error) {
	return Comment{}, nil
}

func (store *discussionHTTPStore) DeleteComment(
	context.Context, platform.User, RepositoryRef, int64, string,
) error {
	return nil
}

func (store *discussionHTTPStore) SetAnswer(
	context.Context, platform.User, RepositoryRef, int64, string, bool,
) (Discussion, error) {
	return Discussion{}, nil
}

func (store *discussionHTTPStore) SetVote(
	context.Context, platform.User, RepositoryRef, int64, bool,
) (Summary, error) {
	return Summary{}, nil
}

type discussionHTTPRepositories struct {
	access collab.Access
}

func (repositories discussionHTTPRepositories) LookupRepository(
	context.Context, *platform.User, string, string,
) (collab.Repository, error) {
	return collab.Repository{
		ID:             "00000000-0000-4000-8000-000000000001",
		OrganizationID: "00000000-0000-4000-8000-000000000002",
		Owner:          "acme", Slug: "game", Visibility: "private",
	}, nil
}

func (repositories discussionHTTPRepositories) RepositoryPermission(
	context.Context, platform.User, collab.Repository,
) (collab.Access, error) {
	return repositories.access, nil
}

type discussionHTTPActors struct{ actor platform.User }

func (actors discussionHTTPActors) ResolveActor(http.ResponseWriter, *http.Request) (platform.User, bool) {
	return actors.actor, true
}

func (actors discussionHTTPActors) ResolveOptionalActor(
	http.ResponseWriter, *http.Request,
) (*platform.User, bool) {
	return &actors.actor, true
}

func TestDiscussionCategoriesExposeModerationFlags(t *testing.T) {
	store := &discussionHTTPStore{categories: []Category{{Slug: "general", Name: "General"}}}
	handler := discussionHTTPHandler(store, collab.Access{Permission: collab.PermWrite})
	request := httptest.NewRequest(http.MethodGet, discussionPath()+"/categories", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("categories status = %d, body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Categories        []Category `json:"categories"`
		ViewerCanManage   bool       `json:"viewerCanManage"`
		ViewerCanModerate bool       `json:"viewerCanModerate"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Categories) != 1 || !body.ViewerCanModerate || body.ViewerCanManage {
		t.Fatalf("category response = %+v", body)
	}
}

func TestDiscussionDeleteRouteAndStrictJSON(t *testing.T) {
	store := &discussionHTTPStore{}
	handler := discussionHTTPHandler(store, collab.Access{Permission: collab.PermWrite})
	request := httptest.NewRequest(http.MethodDelete, discussionPath()+"/7", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || !store.deleted {
		t.Fatalf("delete status = %d, deleted = %v", response.Code, store.deleted)
	}

	store.deleted = false
	request = httptest.NewRequest(http.MethodPost, discussionPath(), bytes.NewBufferString(
		`{"category":"general","title":"A","body":"","unknown":true}`,
	))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || store.created {
		t.Fatalf("strict JSON status = %d, created = %v", response.Code, store.created)
	}
}

func TestDiscussionListFilterRejectsUnknownValues(t *testing.T) {
	for query := range map[string]string{"state=bogus": "state", "sort=bogus": "sort"} {
		request := httptest.NewRequest(http.MethodGet, discussionPath()+"?"+query, nil)
		response := httptest.NewRecorder()
		filter, ok := listFilter(response, request)
		if ok || response.Code != http.StatusBadRequest || filter != (ListFilter{}) {
			t.Fatalf("query %q filter = %+v, ok=%v, status=%d", query, filter, ok, response.Code)
		}
	}
}

func TestDiscussionListFilterAcceptsClientSortValues(t *testing.T) {
	for _, sort := range []string{"newest", "oldest", "most-commented", "most-voted"} {
		request := httptest.NewRequest(http.MethodGet, discussionPath()+"?sort="+sort, nil)
		response := httptest.NewRecorder()
		filter, ok := listFilter(response, request)
		if !ok || response.Code != http.StatusOK || filter.Sort != sort {
			t.Fatalf("sort %q filter = %+v, ok=%v, status=%d", sort, filter, ok, response.Code)
		}
	}
}

func TestDiscussionCategoryFormatsUseDiscussionName(t *testing.T) {
	if _, err := normalizeCategoryInput(CategoryInput{
		Slug: "general", Name: "General", Format: "discussion",
	}); err != nil {
		t.Fatalf("discussion category format rejected: %v", err)
	}
	if _, err := normalizeCategoryInput(CategoryInput{
		Slug: "general", Name: "General", Format: "open",
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("legacy open category format error = %v, want invalid input", err)
	}
}

func TestDiscussionSummaryVotePermissionIsPresent(t *testing.T) {
	if _, err := normalizeCategoryInput(CategoryInput{
		Slug: "general", Name: "General", Format: "discussion",
	}); err != nil {
		t.Fatal(err)
	}
	summary := Summary{Author: Author{ID: "author"}}
	decorateSummary(&summary, &platform.User{ID: "viewer"}, collab.Access{}, false)
	if !summary.ViewerCanVote {
		t.Fatal("active viewer should be allowed to vote")
	}
	decorateSummary(&summary, &platform.User{ID: "viewer"}, collab.Access{}, true)
	if summary.ViewerCanVote {
		t.Fatal("archived viewer should not be allowed to vote")
	}
}

func discussionHTTPHandler(store Store, access collab.Access) http.Handler {
	mux := http.NewServeMux()
	actor := platform.User{ID: uuid.NewString(), Username: "http-user", DisplayName: "HTTP User"}
	Register(mux, store, discussionHTTPRepositories{access: access}, discussionHTTPActors{actor: actor},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	return mux
}

func discussionPath() string { return "/api/v1/repositories/acme/game/discussions" }
