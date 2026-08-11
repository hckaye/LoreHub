package collab

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type fakeDraftStore struct {
	Store
	setDraft func(platform.User, string, int64, bool) (MergeRequest, bool, error)
}

func (store *fakeDraftStore) SetMergeRequestDraft(
	_ context.Context,
	actor platform.User,
	repositoryID string,
	number int64,
	isDraft bool,
) (MergeRequest, bool, error) {
	return store.setDraft(actor, repositoryID, number, isDraft)
}

func TestMergeRequestDraftHandlersPersistBothStates(t *testing.T) {
	base := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner string, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
	}
	var states []bool
	store := &fakeDraftStore{
		Store: base,
		setDraft: func(
			actor platform.User, repositoryID string, number int64, isDraft bool,
		) (MergeRequest, bool, error) {
			if actor.ID != alice().ID || repositoryID != "repo-1" || number != 2 {
				t.Fatalf("unexpected draft update: actor=%+v repository=%q number=%d", actor, repositoryID, number)
			}
			states = append(states, isDraft)
			return MergeRequest{ID: "request-1", Number: number, State: "open", IsDraft: isDraft}, true, nil
		},
	}
	handler := newTestAPI(store)
	for _, request := range []struct {
		method  string
		isDraft bool
	}{
		{method: http.MethodPut, isDraft: true},
		{method: http.MethodDelete, isDraft: false},
	} {
		recorder := doRequest(handler, request.method,
			"/api/v1/repositories/acme/lore/merge-requests/2/draft", "",
			"Authorization", "Bearer alice")
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %s", request.method, recorder.Code, recorder.Body.String())
		}
		want := `"isDraft":false`
		if request.isDraft {
			want = `"isDraft":true`
		}
		if !strings.Contains(recorder.Body.String(), want) {
			t.Fatalf("%s response = %s, want %s", request.method, recorder.Body.String(), want)
		}
	}
	if len(states) != 2 || !states[0] || states[1] {
		t.Fatalf("persisted draft states = %#v", states)
	}
}

func TestMergeRequestDraftHandlersMapConflictsAndFailClosed(t *testing.T) {
	base := &fakeStore{
		user: alice(),
		lookupRepo: func(_ *platform.User, owner string, slug string) (Repository, error) {
			return repoFor(owner, slug), nil
		},
	}
	withoutSupport := doRequest(newTestAPI(base), http.MethodPut,
		"/api/v1/repositories/acme/lore/merge-requests/2/draft", "",
		"Authorization", "Bearer alice")
	if withoutSupport.Code != http.StatusServiceUnavailable {
		t.Fatalf("unsupported status = %d, body = %s", withoutSupport.Code, withoutSupport.Body.String())
	}
	store := &fakeDraftStore{
		Store: base,
		setDraft: func(
			platform.User, string, int64, bool,
		) (MergeRequest, bool, error) {
			return MergeRequest{}, false, ErrMergeBusy
		},
	}
	busy := doRequest(newTestAPI(store), http.MethodPut,
		"/api/v1/repositories/acme/lore/merge-requests/2/draft", "",
		"Authorization", "Bearer alice")
	if busy.Code != http.StatusConflict || errorCode(t, busy) != "merge_busy" {
		t.Fatalf("busy response = %d %s", busy.Code, busy.Body.String())
	}
}
