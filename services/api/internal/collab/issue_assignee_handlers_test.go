package collab

import (
	"context"
	"net/http"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type assigneeHandlerStore struct {
	Store
	items          Result[Assignee]
	query          string
	page           Page
	issueNumber    int64
	username       string
	assigned       bool
	assignmentErr  error
	removeErr      error
	assignmentCall int
	removeCall     int
}

func (store *assigneeHandlerStore) ListAssignableUsers(
	_ context.Context,
	_ string,
	query string,
	page Page,
) (Result[Assignee], error) {
	store.query = query
	store.page = page
	return store.items, nil
}

func (store *assigneeHandlerStore) AssignIssueUser(
	_ context.Context,
	_ platform.User,
	_ string,
	issueNumber int64,
	username string,
) (Assignee, bool, error) {
	store.assignmentCall++
	store.issueNumber = issueNumber
	store.username = username
	return Assignee{ID: "user-1", Username: username}, store.assigned, store.assignmentErr
}

func (store *assigneeHandlerStore) RemoveIssueUser(
	_ context.Context,
	_ platform.User,
	_ string,
	issueNumber int64,
	username string,
) error {
	store.removeCall++
	store.issueNumber = issueNumber
	store.username = username
	return store.removeErr
}

func TestAssignableUsersRequireTriageAndPreservePagination(t *testing.T) {
	readerStore := newAssigneeHandlerStore(PermRead)
	response := doRequest(
		newTestAPI(readerStore), http.MethodGet,
		"/api/v1/repositories/acme/game/assignees", "",
		"Authorization", "Bearer alice",
	)
	if response.Code != http.StatusForbidden || readerStore.query != "" {
		t.Fatalf("reader status = %d, query = %q", response.Code, readerStore.query)
	}

	triageStore := newAssigneeHandlerStore(PermTriage)
	triageStore.items = Result[Assignee]{Items: []Assignee{{Username: "bob"}}}
	response = doRequest(
		newTestAPI(triageStore), http.MethodGet,
		"/api/v1/repositories/acme/game/assignees?query=bob&limit=5", "",
		"Authorization", "Bearer alice",
	)
	if response.Code != http.StatusOK || triageStore.query != "bob" || triageStore.page.Limit != 5 {
		t.Fatalf("triage status = %d, query = %q, page = %#v",
			response.Code, triageStore.query, triageStore.page)
	}
}

func TestIssueAssigneeMutationStatusAndPaths(t *testing.T) {
	store := newAssigneeHandlerStore(PermTriage)
	store.assigned = true
	handler := newTestAPI(store)
	path := "/api/v1/repositories/acme/game/issues/7/assignees/bob"
	response := doRequest(handler, http.MethodPut, path, "", "Authorization", "Bearer alice")
	if response.Code != http.StatusCreated || store.issueNumber != 7 || store.username != "bob" {
		t.Fatalf("create status = %d, issue = %d, username = %q",
			response.Code, store.issueNumber, store.username)
	}
	store.assigned = false
	response = doRequest(handler, http.MethodPut, path, "", "Authorization", "Bearer alice")
	if response.Code != http.StatusOK {
		t.Fatalf("idempotent assignment status = %d", response.Code)
	}
	response = doRequest(handler, http.MethodDelete, path, "", "Authorization", "Bearer alice")
	if response.Code != http.StatusNoContent || store.removeCall != 1 {
		t.Fatalf("remove status = %d, calls = %d", response.Code, store.removeCall)
	}
	response = doRequest(
		handler, http.MethodPut,
		"/api/v1/repositories/acme/game/issues/7/assignees/%20", "",
		"Authorization", "Bearer alice",
	)
	if response.Code != http.StatusNotFound || store.assignmentCall != 2 {
		t.Fatalf("invalid username status = %d, calls = %d", response.Code, store.assignmentCall)
	}
}

func TestIssueAssigneeLimitHasStableConflict(t *testing.T) {
	store := newAssigneeHandlerStore(PermTriage)
	store.assignmentErr = ErrAssigneeLimit
	response := doRequest(
		newTestAPI(store), http.MethodPut,
		"/api/v1/repositories/acme/game/issues/1/assignees/bob", "",
		"Authorization", "Bearer alice",
	)
	if response.Code != http.StatusConflict {
		t.Fatalf("assignee limit status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestIssueAssigneeRoutesFailClosedWithoutStore(t *testing.T) {
	response := doRequest(
		newTestAPI(assigneeBaseStore(PermTriage)), http.MethodGet,
		"/api/v1/repositories/acme/game/assignees", "",
		"Authorization", "Bearer alice",
	)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing assignee store status = %d", response.Code)
	}
}

func newAssigneeHandlerStore(permission Permission) *assigneeHandlerStore {
	return &assigneeHandlerStore{Store: assigneeBaseStore(permission)}
}

func assigneeBaseStore(permission Permission) *fakeStore {
	return &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		permission: func(platform.User, Repository) (Access, error) {
			return Access{Permission: permission}, nil
		},
	}
}
