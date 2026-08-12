package collab

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type fakeEngagementStore struct {
	Store
	star  func(platform.User, string, bool) (RepositoryEngagement, error)
	watch func(platform.User, string, bool) (RepositoryEngagement, error)
}

func TestRepositoryEngagementAllowsArchivedRepositories(t *testing.T) {
	base := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner string, slug string) (Repository, error) {
			repository := repoFor(owner, slug)
			archivedAt := time.Now()
			repository.ArchivedAt = &archivedAt
			return repository, nil
		},
	}
	called := false
	store := &fakeEngagementStore{
		Store: base,
		star: func(platform.User, string, bool) (RepositoryEngagement, error) {
			called = true
			return RepositoryEngagement{ViewerHasStarred: true}, nil
		},
	}
	recorder := doRequest(newTestAPI(store), http.MethodPut,
		"/api/v1/repositories/acme/lore/star", "", "Authorization", "Bearer alice")
	if recorder.Code != http.StatusOK || !called {
		t.Fatalf("response = %d %s, called = %t", recorder.Code, recorder.Body.String(), called)
	}
}

func (store *fakeEngagementStore) SetRepositoryStar(
	_ context.Context,
	actor platform.User,
	repositoryID string,
	enabled bool,
) (RepositoryEngagement, error) {
	return store.star(actor, repositoryID, enabled)
}

func (store *fakeEngagementStore) SetRepositoryWatch(
	_ context.Context,
	actor platform.User,
	repositoryID string,
	enabled bool,
) (RepositoryEngagement, error) {
	return store.watch(actor, repositoryID, enabled)
}

func TestRepositoryEngagementHandlers(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		path    string
		kind    string
		enabled bool
	}{
		{name: "star", method: http.MethodPut, path: "/star", kind: "star", enabled: true},
		{name: "unstar", method: http.MethodDelete, path: "/star", kind: "star"},
		{name: "watch", method: http.MethodPut, path: "/watch", kind: "watch", enabled: true},
		{name: "unwatch", method: http.MethodDelete, path: "/watch", kind: "watch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := &fakeStore{
				user: alice(),
				lookupRepo: func(_ *platform.User, owner string, slug string) (Repository, error) {
					return repoFor(owner, slug), nil
				},
			}
			called := false
			mutate := func(
				actor platform.User, repositoryID string, enabled bool,
			) (RepositoryEngagement, error) {
				called = true
				if actor.ID != alice().ID || repositoryID != "repo-1" || enabled != test.enabled {
					t.Fatalf("mutation = actor %q repository %q enabled %t", actor.ID, repositoryID, enabled)
				}
				return RepositoryEngagement{
					StarCount: 3, WatcherCount: 2,
					ViewerHasStarred: test.kind == "star" && test.enabled,
					ViewerIsWatching: test.kind == "watch" && test.enabled,
				}, nil
			}
			store := &fakeEngagementStore{Store: base, star: mutate, watch: mutate}
			recorder := doRequest(newTestAPI(store), test.method,
				"/api/v1/repositories/acme/lore"+test.path, "",
				"Authorization", "Bearer alice")
			if recorder.Code != http.StatusOK || !called {
				t.Fatalf("response = %d %s, called = %t", recorder.Code, recorder.Body.String(), called)
			}
			var response RepositoryEngagement
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if response.StarCount != 3 || response.WatcherCount != 2 {
				t.Fatalf("response = %+v", response)
			}
		})
	}
}

func TestRepositoryEngagementFailsClosedWithoutStore(t *testing.T) {
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner string, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
	}
	recorder := doRequest(newTestAPI(store), http.MethodPut,
		"/api/v1/repositories/acme/lore/star", "", "Authorization", "Bearer alice")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
}
