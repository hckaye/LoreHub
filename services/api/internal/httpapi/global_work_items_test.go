package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type globalWorkItemStoreFake struct {
	issuePage  platform.GlobalWorkItemPage
	pullPage   platform.GlobalWorkItemPage
	issueActor platform.User
	pullActor  platform.User
	issueInput platform.GlobalWorkItemFilter
	pullInput  platform.GlobalWorkItemFilter
	err        error
}

func (store *globalWorkItemStoreFake) ListGlobalIssues(
	_ context.Context,
	actor platform.User,
	filter platform.GlobalWorkItemFilter,
) (platform.GlobalWorkItemPage, error) {
	store.issueActor = actor
	store.issueInput = filter
	return store.issuePage, store.err
}

func (store *globalWorkItemStoreFake) ListGlobalPullRequests(
	_ context.Context,
	actor platform.User,
	filter platform.GlobalWorkItemFilter,
) (platform.GlobalWorkItemPage, error) {
	store.pullActor = actor
	store.pullInput = filter
	return store.pullPage, store.err
}

func TestGlobalWorkItemRoutesRequireAuthenticationAndForwardFilters(t *testing.T) {
	store := &globalWorkItemStoreFake{
		issuePage: platform.GlobalWorkItemPage{Items: []platform.GlobalWorkItem{{ID: "issue-1"}}},
		pullPage:  platform.GlobalWorkItemPage{Items: []platform.GlobalWorkItem{{ID: "pull-1"}}},
	}
	handler := newGlobalWorkItemHandler(store)

	issueRequest := httptest.NewRequest(http.MethodGet,
		"/api/v1/issues?state=closed&scope=assigned&q=renderer&limit=50", nil)
	issueRequest.Header.Set("Authorization", "Bearer test")
	issueResponse := httptest.NewRecorder()
	handler.ServeHTTP(issueResponse, issueRequest)
	if issueResponse.Code != http.StatusOK {
		t.Fatalf("issue response status = %d, body = %s", issueResponse.Code, issueResponse.Body.String())
	}
	if store.issueActor.Username != "alice" || store.issueInput.State != "closed" ||
		store.issueInput.Scope != "assigned" || store.issueInput.Query != "renderer" ||
		store.issueInput.Limit != 50 {
		t.Fatalf("issue request was not forwarded: actor=%+v filter=%+v", store.issueActor, store.issueInput)
	}
	var issuePage platform.GlobalWorkItemPage
	if err := json.Unmarshal(issueResponse.Body.Bytes(), &issuePage); err != nil || len(issuePage.Items) != 1 {
		t.Fatalf("decode issue page: page=%+v err=%v", issuePage, err)
	}

	pullRequest := httptest.NewRequest(http.MethodGet,
		"/api/v1/pulls?state=open&scope=review_requested", nil)
	pullRequest.Header.Set("Authorization", "Bearer test")
	pullResponse := httptest.NewRecorder()
	handler.ServeHTTP(pullResponse, pullRequest)
	if pullResponse.Code != http.StatusOK {
		t.Fatalf("pull response status = %d, body = %s", pullResponse.Code, pullResponse.Body.String())
	}
	if store.pullActor.Username != "alice" || store.pullInput.Scope != "review_requested" {
		t.Fatalf("pull request was not forwarded: actor=%+v filter=%+v", store.pullActor, store.pullInput)
	}
}

func TestGlobalWorkItemRoutesRejectInvalidQueries(t *testing.T) {
	store := &globalWorkItemStoreFake{}
	handler := newGlobalWorkItemHandler(store)
	paths := []string{
		"/api/v1/issues?scope=review_requested",
		"/api/v1/issues?state=merged",
		"/api/v1/pulls?scope=unknown",
		"/api/v1/pulls?limit=101",
		"/api/v1/issues?state=open&state=closed",
		"/api/v1/issues?unknown=value",
	}
	for _, path := range paths {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer test")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400", path, response.Code)
		}
	}
	if store.issueActor.ID != "" || store.pullActor.ID != "" {
		t.Fatal("invalid queries reached the work item store")
	}
}

func TestGlobalWorkItemRoutesFailClosedWithoutStore(t *testing.T) {
	handler := newGlobalWorkItemHandler(nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/issues", nil)
	request.Header.Set("Authorization", "Bearer test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}
}

func newGlobalWorkItemHandler(store GlobalWorkItemStore) http.Handler {
	options := []Option{}
	if store != nil {
		options = append(options, WithGlobalWorkItems(store))
	}
	return New(
		fakeStore{user: platform.User{ID: "user-1", Username: "alice", DisplayName: "Alice"}},
		fakeLore{},
		staticAuthenticator{principal: auth.Principal{Issuer: "test", Subject: "alice"}},
		healthy{},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		options...,
	)
}
