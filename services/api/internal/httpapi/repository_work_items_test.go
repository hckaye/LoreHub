package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/collab"
)

func TestRepositoryWorkItemListsReturnViewerCapabilities(t *testing.T) {
	handler, store, cookie := repositoryWorkItemTestHandler(t)
	store.issues = []collab.Issue{{
		ID: "issue-1", Number: 1, State: "open", AuthorID: "user-1",
	}}
	store.mergeRequests = []collab.MergeRequestListItem{{
		MergeRequest: collab.MergeRequest{
			ID: "pull-1", Number: 1, State: "open", AuthorID: "user-2",
		},
	}}

	issueRequest := httptest.NewRequest(
		http.MethodGet, "/api/v1/repositories/acme/private/issues", nil,
	)
	issueRequest.AddCookie(cookie)
	issueResponse := httptest.NewRecorder()
	handler.ServeHTTP(issueResponse, issueRequest)
	if issueResponse.Code != http.StatusOK {
		t.Fatalf("issue page status=%d body=%s", issueResponse.Code, issueResponse.Body.String())
	}
	var issuePage collab.RepositoryIssuePage
	if err := json.Unmarshal(issueResponse.Body.Bytes(), &issuePage); err != nil {
		t.Fatal(err)
	}
	if len(issuePage.Issues) != 1 || !issuePage.Issues[0].ViewerCanUpdate ||
		!issuePage.Issues[0].ViewerCanManageLabels {
		t.Fatalf("issue page body=%s", issueResponse.Body.String())
	}

	pullRequest := httptest.NewRequest(
		http.MethodGet, "/api/v1/repositories/acme/private/merge-requests", nil,
	)
	pullRequest.AddCookie(cookie)
	pullResponse := httptest.NewRecorder()
	handler.ServeHTTP(pullResponse, pullRequest)
	if pullResponse.Code != http.StatusOK {
		t.Fatalf("pull request page status=%d body=%s", pullResponse.Code, pullResponse.Body.String())
	}
	var pullPage collab.RepositoryMergeRequestPage
	if err := json.Unmarshal(pullResponse.Body.Bytes(), &pullPage); err != nil {
		t.Fatal(err)
	}
	if len(pullPage.MergeRequests) != 1 || !pullPage.MergeRequests[0].ViewerCanUpdate ||
		!pullPage.MergeRequests[0].ViewerCanReview {
		t.Fatalf("pull request page body=%s", pullResponse.Body.String())
	}
}

func TestRepositoryIssueListParsesProductionFilters(t *testing.T) {
	handler, store, cookie := repositoryWorkItemTestHandler(t)
	target := "/api/v1/repositories/acme/private/issues" +
		"?state=all&q=renderer&author=alice&assignee=bob&label=Bug&label=bug" +
		"&milestone=12&sort=comments&direction=asc&page=2&per_page=50"
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("issue list status = %d, body=%s", response.Code, response.Body.String())
	}
	query := store.issueQuery
	if query.State != "all" || query.Search != "renderer" || query.Author != "alice" ||
		query.Assignee != "bob" || len(query.Labels) != 1 || query.Labels[0] != "Bug" ||
		query.MilestoneNumber == nil || *query.MilestoneNumber != 12 ||
		query.Sort != "comments" || query.Direction != "asc" ||
		query.Page != 2 || query.PerPage != 50 {
		t.Fatalf("issue query = %#v", query)
	}
}

func TestRepositoryPullRequestListParsesProductionFilters(t *testing.T) {
	handler, store, cookie := repositoryWorkItemTestHandler(t)
	target := "/api/v1/repositories/acme/private/merge-requests" +
		"?state=open&q=renderer&assignee=none&label=Ready&milestone=none" +
		"&source=feature%2Frender&target=main&draft=true&sort=created&direction=desc"
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("pull request list status = %d, body=%s", response.Code, response.Body.String())
	}
	query := store.mergeRequestQuery
	if query.State != "open" || query.Search != "renderer" || query.Assignee != "none" ||
		len(query.Labels) != 1 || query.Labels[0] != "Ready" || !query.WithoutMilestone ||
		query.SourceBranch != "feature/render" || query.TargetBranch != "main" ||
		query.Draft == nil || !*query.Draft || query.Sort != "created" ||
		query.Direction != "desc" || query.Page != 1 || query.PerPage != 25 {
		t.Fatalf("pull request query = %#v", query)
	}
}

func TestRepositoryWorkItemListRejectsAmbiguousAndUnknownFilters(t *testing.T) {
	handler, _, cookie := repositoryWorkItemTestHandler(t)
	targets := []string{
		"/api/v1/repositories/acme/private/issues?state=open&state=closed",
		"/api/v1/repositories/acme/private/issues?unknown=value",
		"/api/v1/repositories/acme/private/issues?page=0",
		"/api/v1/repositories/acme/private/issues?milestone=none&milestone=1",
		"/api/v1/repositories/acme/private/merge-requests?draft=maybe",
		"/api/v1/repositories/acme/private/merge-requests?per_page=101",
	}
	for _, target := range targets {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("target %q status = %d, body=%s", target, response.Code, response.Body.String())
		}
	}
}

func repositoryWorkItemTestHandler(
	t *testing.T,
) (http.Handler, *authCollabStore, *http.Cookie) {
	t.Helper()
	codec, err := auth.NewSecretCodec("test-secret")
	if err != nil {
		t.Fatal(err)
	}
	authenticationStore := &fakeAuthenticationStore{}
	store := &authCollabStore{}
	handler := newCollabAuthTestHandler(
		auth.DisabledAuthenticator{}, authenticationStore, codec, store,
	)
	cookie, _ := prepareSessionCookie(t, authenticationStore, codec)
	return handler, store, cookie
}
