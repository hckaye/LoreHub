package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type insightsHTTPStore struct {
	IdentityStore
	result platform.RepositoryInsights
	err    error
	actor  *platform.User
	owner  string
	repo   string
	days   int
}

func (store *insightsHTTPStore) RepositoryInsights(
	_ context.Context,
	actor *platform.User,
	owner string,
	repository string,
	days int,
) (platform.RepositoryInsights, error) {
	store.actor = actor
	store.owner = owner
	store.repo = repository
	store.days = days
	return store.result, store.err
}

func TestRepositoryInsightsHTTPValidatesPeriodAndPreservesVisibility(t *testing.T) {
	newHandler := func(store *insightsHTTPStore) http.Handler {
		return New(
			fakeStore{}, fakeLore{}, auth.DisabledAuthenticator{}, healthy{}, "",
			slog.New(slog.NewTextHandler(io.Discard, nil)),
			WithIdentityStore(store),
		)
	}
	request := func(store *insightsHTTPStore, path string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		newHandler(store).ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		return response
	}

	store := &insightsHTTPStore{result: platform.RepositoryInsights{Activity: []platform.RepositoryInsightDay{}}}
	response := request(store, "/api/v1/repositories/acme/game/insights?days=7")
	if response.Code != http.StatusOK || store.actor != nil || store.owner != "acme" || store.repo != "game" ||
		store.days != 7 {
		t.Fatalf("insights response=%d actor=%+v owner=%q repo=%q days=%d", response.Code, store.actor,
			store.owner, store.repo, store.days)
	}
	if response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("insights cache policy = %q", response.Header().Get("Cache-Control"))
	}

	for _, path := range []string{
		"/api/v1/repositories/acme/game/insights?days=14",
		"/api/v1/repositories/acme/game/insights?days=",
		"/api/v1/repositories/acme/game/insights?days=7&days=30",
		"/api/v1/repositories/acme/game/insights?period=7",
	} {
		if response := request(&insightsHTTPStore{}, path); response.Code != http.StatusBadRequest {
			t.Fatalf("invalid insights path %q response = %d %s", path, response.Code, response.Body.String())
		}
	}

	response = request(&insightsHTTPStore{err: platform.ErrNotFound},
		"/api/v1/repositories/acme/private/insights")
	if response.Code != http.StatusNotFound {
		t.Fatalf("private insights response = %d %s", response.Code, response.Body.String())
	}
}
