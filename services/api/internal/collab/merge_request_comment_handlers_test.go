package collab

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type fakeConversationStore struct {
	Store
	listComments  func(string, int64, Page) (Result[MergeRequestComment], error)
	createComment func(platform.User, string, int64, string) (MergeRequestComment, error)
	updateComment func(platform.User, string, int64, string, string) (MergeRequestComment, error)
	deleteComment func(platform.User, string, int64, string) error
}

func (store *fakeConversationStore) ListMergeRequestComments(
	_ context.Context,
	repoID string,
	number int64,
	page Page,
) (Result[MergeRequestComment], error) {
	return store.listComments(repoID, number, page)
}

func (store *fakeConversationStore) CreateMergeRequestComment(
	_ context.Context,
	actor platform.User,
	repoID string,
	number int64,
	body string,
) (MergeRequestComment, error) {
	return store.createComment(actor, repoID, number, body)
}

func (store *fakeConversationStore) UpdateMergeRequestComment(
	_ context.Context,
	actor platform.User,
	repoID string,
	number int64,
	commentID string,
	body string,
) (MergeRequestComment, error) {
	return store.updateComment(actor, repoID, number, commentID, body)
}

func (store *fakeConversationStore) DeleteMergeRequestComment(
	_ context.Context,
	actor platform.User,
	repoID string,
	number int64,
	commentID string,
) error {
	return store.deleteComment(actor, repoID, number, commentID)
}

func TestGetMergeRequestIncludesViewerCapabilities(t *testing.T) {
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner string, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		permission: func(platform.User, Repository) (Access, error) {
			return Access{Permission: PermRead}, nil
		},
		getMR: func(string, int64) (MergeRequest, error) {
			return MergeRequest{
				ID: "request-1", Number: 2, Title: "Change", State: "open",
				Author: "bob", AuthorID: "bob-id",
			}, nil
		},
	}
	recorder := doRequest(newTestAPI(store), http.MethodGet,
		"/api/v1/repositories/acme/lore/merge-requests/2", "",
		"Authorization", "Bearer alice")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode pull request: %v", err)
	}
	if payload["viewerCanUpdate"] != false || payload["viewerCanReview"] != true {
		t.Fatalf("viewer capabilities = %+v", payload)
	}
	if _, exists := payload["authorId"]; exists {
		t.Fatalf("response leaked authorId: %+v", payload)
	}
}

func TestGetMergedRequestDoesNotAdvertiseUpdateOrReview(t *testing.T) {
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner string, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		permission: func(platform.User, Repository) (Access, error) {
			return Access{Permission: PermAdmin}, nil
		},
		getMR: func(string, int64) (MergeRequest, error) {
			return MergeRequest{
				ID: "request-1", Number: 2, Title: "Change", State: "merged",
				Author: "alice", AuthorID: alice().ID,
			}, nil
		},
	}
	recorder := doRequest(newTestAPI(store), http.MethodGet,
		"/api/v1/repositories/acme/lore/merge-requests/2", "",
		"Authorization", "Bearer alice")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode pull request: %v", err)
	}
	if payload["viewerCanUpdate"] != false || payload["viewerCanReview"] != false {
		t.Fatalf("viewer capabilities = %+v", payload)
	}
}

func TestMergeRequestCommentHandlersSetViewerCapabilities(t *testing.T) {
	base := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner string, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		permission: func(platform.User, Repository) (Access, error) {
			return Access{Permission: PermTriage}, nil
		},
	}
	store := &fakeConversationStore{
		Store: base,
		listComments: func(string, int64, Page) (Result[MergeRequestComment], error) {
			return Result[MergeRequestComment]{Items: []MergeRequestComment{{
				ID: "comment-1", Author: "bob", AuthorID: "bob-id", Body: "Please update",
			}}}, nil
		},
		createComment: func(
			actor platform.User, _ string, _ int64, body string,
		) (MergeRequestComment, error) {
			return MergeRequestComment{ID: "comment-2", Author: actor.Username, Body: body}, nil
		},
	}
	handler := newTestAPI(store)
	list := doRequest(handler, http.MethodGet,
		"/api/v1/repositories/acme/lore/merge-requests/2/comments", "",
		"Authorization", "Bearer alice")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"viewerCanUpdate":true`) {
		t.Fatalf("list response = %d %s", list.Code, list.Body.String())
	}
	if strings.Contains(list.Body.String(), "authorId") {
		t.Fatalf("list response leaked author id: %s", list.Body.String())
	}
	create := doRequest(handler, http.MethodPost,
		"/api/v1/repositories/acme/lore/merge-requests/2/comments", `{"body":"Looks good"}`,
		"Authorization", "Bearer alice", "Content-Type", "application/json")
	if create.Code != http.StatusCreated || !strings.Contains(create.Body.String(), `"viewerCanUpdate":true`) {
		t.Fatalf("create response = %d %s", create.Code, create.Body.String())
	}
}

func TestMergeRequestCommentsFailClosedWithoutStoreSupport(t *testing.T) {
	store := &fakeStore{lookupRepo: func(_ *platform.User, owner string, slug string) (Repository, error) {
		return repoFor(owner, slug), nil
	}}
	recorder := doRequest(newTestAPI(store), http.MethodGet,
		"/api/v1/repositories/acme/lore/merge-requests/2/comments", "")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
