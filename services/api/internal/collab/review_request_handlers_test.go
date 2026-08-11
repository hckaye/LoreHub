package collab

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type reviewRequestHandlerStore struct {
	*fakeStore
	list       []ReviewRequest
	candidates []ReviewCandidate
	created    ReviewRequest
	removed    bool
}

func (store *reviewRequestHandlerStore) ListReviewRequests(
	context.Context, string, int64,
) ([]ReviewRequest, error) {
	return store.list, nil
}

func (store *reviewRequestHandlerStore) ListReviewCandidates(
	context.Context, platform.User, string, int64, string,
) ([]ReviewCandidate, error) {
	return store.candidates, nil
}

func (store *reviewRequestHandlerStore) RequestUserReview(
	context.Context, platform.User, string, int64, string,
) (ReviewRequest, bool, error) {
	return store.created, true, nil
}

func (store *reviewRequestHandlerStore) RequestTeamReview(
	context.Context, platform.User, string, int64, string,
) (ReviewRequest, bool, error) {
	return store.created, true, nil
}

func (store *reviewRequestHandlerStore) RemoveUserReviewRequest(
	context.Context, platform.User, string, int64, string,
) error {
	store.removed = true
	return nil
}

func (store *reviewRequestHandlerStore) RemoveTeamReviewRequest(
	context.Context, platform.User, string, int64, string,
) error {
	store.removed = true
	return nil
}

func TestReviewRequestRoutes(t *testing.T) {
	user := platform.User{ID: uuidNew(), Username: "alice"}
	repository := Repository{ID: uuidNew(), OrganizationID: uuidNew(), Owner: "acme", Slug: "game"}
	base := &fakeStore{
		user: user,
		lookupRepo: func(_ *platform.User, _, _ string) (Repository, error) {
			return repository, nil
		},
		permission: func(platform.User, Repository) (Access, error) {
			return Access{Permission: PermTriage}, nil
		},
		getMR: func(string, int64) (MergeRequest, error) {
			return MergeRequest{ID: uuidNew(), AuthorID: user.ID, State: "open"}, nil
		},
	}
	store := &reviewRequestHandlerStore{
		fakeStore: base,
		list: []ReviewRequest{{
			ID: uuidNew(), Kind: "user", Slug: "bob", DisplayName: "Bob", Status: "pending",
		}},
		candidates: []ReviewCandidate{{Kind: "team", Slug: "reviewers", DisplayName: "Reviewers"}},
		created: ReviewRequest{
			ID: uuidNew(), Kind: "user", Slug: "bob", DisplayName: "Bob", Status: "pending",
		},
	}
	mux := http.NewServeMux()
	Register(mux, store, testActorResolver{store: store}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	list := reviewRequestHTTP(t, mux, http.MethodGet,
		"/api/v1/repositories/acme/game/merge-requests/7/review-requests", "alice")
	if list.Code != http.StatusOK || !containsAll(list.Body.String(), "viewerCanManage", "bob") {
		t.Fatalf("list response = %d %s", list.Code, list.Body.String())
	}
	candidates := reviewRequestHTTP(t, mux, http.MethodGet,
		"/api/v1/repositories/acme/game/merge-requests/7/review-candidates", "alice")
	if candidates.Code != http.StatusOK || !containsAll(candidates.Body.String(), "reviewers", "team") {
		t.Fatalf("candidate response = %d %s", candidates.Code, candidates.Body.String())
	}
	created := reviewRequestHTTP(t, mux, http.MethodPut,
		"/api/v1/repositories/acme/game/merge-requests/7/review-requests/users/bob", "alice")
	if created.Code != http.StatusCreated || !containsAll(created.Body.String(), "pending", "bob") {
		t.Fatalf("create response = %d %s", created.Code, created.Body.String())
	}
	removed := reviewRequestHTTP(t, mux, http.MethodDelete,
		"/api/v1/repositories/acme/game/merge-requests/7/review-requests/users/bob", "alice")
	if removed.Code != http.StatusNoContent || !store.removed {
		t.Fatalf("remove response = %d, removed = %t", removed.Code, store.removed)
	}
}

func reviewRequestHTTP(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	actor string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	if actor != "" {
		request.Header.Set("Authorization", "Bearer "+actor)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func containsAll(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(value, fragment) {
			return false
		}
	}
	return true
}
