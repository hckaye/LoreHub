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

type searchHTTPStore struct {
	IdentityStore
	query   string
	kind    string
	page    int
	perPage int
	result  platform.SearchResults
}

func (store *searchHTTPStore) Search(
	_ context.Context,
	_ *platform.User,
	query string,
	kind string,
	page int,
	perPage int,
) (platform.SearchResults, error) {
	store.query = query
	store.kind = kind
	store.page = page
	store.perPage = perPage
	return store.result, nil
}

func TestSearchHTTPForwardsPagingAndTypes(t *testing.T) {
	store := &searchHTTPStore{result: platform.SearchResults{
		Repositories: []platform.Repository{}, Organizations: []platform.OrganizationView{},
		Users: []platform.UserSearchResult{}, Issues: []platform.GlobalWorkItem{},
		PullRequests: []platform.GlobalWorkItem{}, Page: 2, PerPage: 7,
	}}
	handler := newIdentityTestAPI(t, store)
	request := httptest.NewRequest(
		http.MethodGet, "/api/v1/search?q=needle&type=issues&page=2&per_page=7", nil,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if store.query != "needle" || store.kind != "issues" || store.page != 2 || store.perPage != 7 {
		t.Fatalf("search input = query %q kind %q page %d perPage %d",
			store.query, store.kind, store.page, store.perPage)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"repositories", "organizations", "users", "issues", "pullRequests", "counts", "page", "perPage",
	} {
		if _, ok := body[key]; !ok {
			t.Errorf("response omitted %q: %s", key, response.Body.String())
		}
	}
}

func TestSearchHTTPLimitAliasAndPerPagePrecedence(t *testing.T) {
	store := &searchHTTPStore{result: platform.SearchResults{}}
	handler := newIdentityTestAPI(t, store)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/search?q=x&limit=9", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || store.perPage != 9 {
		t.Fatalf("limit alias status = %d, perPage = %d", response.Code, store.perPage)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/search?q=x&per_page=4", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || store.perPage != 4 {
		t.Fatalf("per_page precedence status = %d, perPage = %d", response.Code, store.perPage)
	}
}

func TestSearchHTTPRejectsInvalidQueries(t *testing.T) {
	store := &searchHTTPStore{}
	handler := newIdentityTestAPI(t, store)
	queries := []string{
		"q=", "q=x&q=y", "q=x&unknown=y", "q=x&type=bad", "q=x&page=0",
		"q=nul%00query", "q=line%0Aquery", "q=control%C2%85query",
		"q=x&page=100001", "q=x&limit=9&per_page=4",
		"q=x&page=999999999999999999999999999999999", "q=x&per_page=0",
		"q=x&per_page=51", "q=" + strings.Repeat("界", 161), "q=x&type=all&type=issues",
	}
	for _, query := range queries {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/search?"+query, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Errorf("query %q status = %d, body = %s", query, response.Code, response.Body.String())
		}
	}
}

func TestParseSearchParametersTypes(t *testing.T) {
	for _, kind := range []string{"all", "repositories", "organizations", "users", "issues", "pulls"} {
		parameters, err := parseSearchParameters(map[string][]string{"q": {"needle"}, "type": {kind}})
		if err != nil || parameters.Kind != kind || parameters.Page != 1 || parameters.PerPage != 20 {
			t.Errorf("kind %q parameters = %#v, error = %v", kind, parameters, err)
		}
	}
}

func newIdentityTestAPI(t *testing.T, store IdentityStore) http.Handler {
	t.Helper()
	return New(
		fakeStore{}, fakeLore{}, auth.DisabledAuthenticator{}, healthy{}, "",
		slog.New(slog.NewTextHandler(io.Discard, nil)), WithIdentityStore(store),
	)
}
