package collab

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type fakeReactionStore struct {
	Store
	put    func(platform.User, string, ReactionInput) (ReactionMutation, error)
	remove func(platform.User, string, ReactionInput) (ReactionMutation, error)
}

func (store *fakeReactionStore) PutReaction(
	_ context.Context,
	actor platform.User,
	repositoryID string,
	input ReactionInput,
) (ReactionMutation, error) {
	return store.put(actor, repositoryID, input)
}

func (store *fakeReactionStore) DeleteReaction(
	_ context.Context,
	actor platform.User,
	repositoryID string,
	input ReactionInput,
) (ReactionMutation, error) {
	return store.remove(actor, repositoryID, input)
}

func TestReactionMutationRoutesUseRepositoryScopeAndToggleBody(t *testing.T) {
	t.Parallel()
	var received ReactionInput
	base := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		permission: func(platform.User, Repository) (Access, error) {
			return Access{Permission: PermRead}, nil
		},
	}
	store := &fakeReactionStore{
		Store: base,
		put: func(actor platform.User, repositoryID string, input ReactionInput) (ReactionMutation, error) {
			if actor.Username != "alice" || repositoryID != "repo-1" {
				t.Fatalf("unexpected reaction scope: actor=%+v repository=%q", actor, repositoryID)
			}
			received = input
			return ReactionMutation{Reactions: []Reaction{{Reaction: input.Reaction, Count: 1, ViewerReacted: true}}}, nil
		},
		remove: func(_ platform.User, _ string, input ReactionInput) (ReactionMutation, error) {
			received = input
			return ReactionMutation{}, nil
		},
	}
	handler := newTestAPI(store)
	body := `{"subjectKind":"issue_comment","subjectId":"00000000-0000-4000-8000-000000000001","reaction":"heart"}`
	put := doRequest(handler, http.MethodPut, "/api/v1/repositories/acme/lore/reactions", body,
		"Authorization", "Bearer alice", "Content-Type", "application/json")
	if put.Code != http.StatusOK || received.SubjectKind != reactionIssueComment || received.Reaction != "heart" {
		t.Fatalf("PUT response=%d input=%+v body=%s", put.Code, received, put.Body.String())
	}
	remove := doRequest(handler, http.MethodDelete, "/api/v1/repositories/acme/lore/reactions", body,
		"Authorization", "Bearer alice", "Content-Type", "application/json")
	if remove.Code != http.StatusOK || received.SubjectID == "" {
		t.Fatalf("DELETE response=%d input=%+v body=%s", remove.Code, received, remove.Body.String())
	}
}

func TestReactionMutationRejectsUnsupportedValues(t *testing.T) {
	t.Parallel()
	base := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
		permission: func(platform.User, Repository) (Access, error) {
			return Access{Permission: PermRead}, nil
		},
	}
	called := false
	store := &fakeReactionStore{
		Store: base,
		put: func(platform.User, string, ReactionInput) (ReactionMutation, error) {
			called = true
			return ReactionMutation{}, nil
		},
		remove: func(platform.User, string, ReactionInput) (ReactionMutation, error) {
			called = true
			return ReactionMutation{}, nil
		},
	}
	recorder := doRequest(newTestAPI(store), http.MethodPut,
		"/api/v1/repositories/acme/lore/reactions",
		`{"subjectKind":"issue","subjectId":"not-a-uuid","reaction":"party"}`,
		"Authorization", "Bearer alice", "Content-Type", "application/json")
	if recorder.Code != http.StatusBadRequest || errorCode(t, recorder) != "invalid_input" || called {
		t.Fatalf("invalid reaction response=%d called=%t body=%s", recorder.Code, called, recorder.Body.String())
	}
}

func TestNormalizeReactionInputAllowsTheGitHubReactionSet(t *testing.T) {
	for _, reaction := range []string{"+1", "-1", "laugh", "confused", "heart", "hooray", "rocket", "eyes"} {
		input, err := normalizeReactionInput(ReactionInput{
			SubjectKind: " issue_comment ",
			SubjectID:   "00000000-0000-4000-8000-000000000001",
			Reaction:    " " + reaction + " ",
		})
		if err != nil || input.Reaction != reaction || input.SubjectKind != reactionIssueComment {
			t.Fatalf("reaction %q normalized to %+v, err=%v", reaction, input, err)
		}
	}
}

func TestReactionMutationResponseUsesLowerCamelCase(t *testing.T) {
	value, err := json.Marshal(ReactionMutation{Reactions: []Reaction{{
		Reaction: "rocket", Count: 2, ViewerReacted: true,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(value), `"viewerReacted":true`) {
		t.Fatalf("reaction response = %s", value)
	}
}
