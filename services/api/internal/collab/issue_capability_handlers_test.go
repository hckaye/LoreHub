package collab

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func TestGetIssueIncludesViewerCapabilitiesWithoutAuthorID(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		permission: func(platform.User, Repository) (Access, error) {
			return Access{Permission: PermTriage}, nil
		},
		getIssue: func(_ string, _ int64) (Issue, error) {
			return Issue{
				ID: "issue-1", Number: 3, Title: "Bug", State: "open",
				Author: "bob", AuthorID: "bob-id", Labels: []Label{},
			}, nil
		},
	}
	handler := newTestAPI(store)
	recorder := doRequest(handler, http.MethodGet,
		"/api/v1/repositories/acme/lore/issues/3", "", "Authorization", "Bearer alice")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", recorder.Code, recorder.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	if payload["viewerCanUpdate"] != true || payload["viewerCanManageLabels"] != true {
		t.Fatalf("viewer capabilities = %+v", payload)
	}
	if _, exists := payload["authorId"]; exists {
		t.Fatalf("response leaked authorId: %+v", payload)
	}
}

func TestListCommentsIncludesViewerCapabilityWithoutAuthorID(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		permission: func(platform.User, Repository) (Access, error) {
			return Access{Permission: PermTriage}, nil
		},
		listComments: func(string, int64, Page) (Result[IssueComment], error) {
			return Result[IssueComment]{Items: []IssueComment{{
				ID: "comment-1", Author: "bob", AuthorID: "bob-id", Body: "body",
			}}}, nil
		},
	}
	handler := newTestAPI(store)
	recorder := doRequest(handler, http.MethodGet,
		"/api/v1/repositories/acme/lore/issues/3/comments", "", "Authorization", "Bearer alice")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode comments: %v", err)
	}
	if len(payload.Items) != 1 || payload.Items[0]["viewerCanUpdate"] != true {
		t.Fatalf("viewer capabilities = %+v", payload.Items)
	}
	if _, exists := payload.Items[0]["authorId"]; exists {
		t.Fatalf("response leaked authorId: %+v", payload.Items[0])
	}
}

func TestPatchIssueReturnsViewerCapability(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		updateIssue: func(_ platform.User, _ string, _ int64, input UpdateIssueInput) (Issue, error) {
			title := "New title"
			state := "closed"
			if input.Title != nil {
				title = *input.Title
			}
			if input.State != nil {
				state = *input.State
			}
			return Issue{ID: "issue-1", Number: 3, Title: title, State: state, Author: "alice"}, nil
		},
	}
	handler := newTestAPI(store)
	body := `{"title":"New title","state":"closed"}`
	recorder := doRequest(handler, http.MethodPatch,
		"/api/v1/repositories/acme/lore/issues/3", body, "Authorization", "Bearer alice",
		"Content-Type", "application/json")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", recorder.Code, recorder.Body.String())
	}
	var issue Issue
	if err := json.Unmarshal(recorder.Body.Bytes(), &issue); err != nil {
		t.Fatalf("decode patched issue: %v", err)
	}
	if !issue.ViewerCanUpdate {
		t.Fatal("successful issue mutation did not return viewerCanUpdate")
	}
}
