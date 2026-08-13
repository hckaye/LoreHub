package collab

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type eventHandlerStore struct {
	Store
	itemKind string
	number   int64
	page     Page
	events   []WorkItemEvent
	listErr  error
	calls    int
}

func (store *eventHandlerStore) ListWorkItemEvents(
	_ context.Context,
	_ string,
	itemKind string,
	number int64,
	page Page,
) (Result[WorkItemEvent], error) {
	store.calls++
	store.itemKind = itemKind
	store.number = number
	store.page = page
	if store.listErr != nil {
		return Result[WorkItemEvent]{}, store.listErr
	}
	return Result[WorkItemEvent]{Items: store.events}, nil
}

func TestWorkItemEventEndpointsSelectTheItemKind(t *testing.T) {
	store := &eventHandlerStore{
		Store: assigneeBaseStore(PermRead),
		events: []WorkItemEvent{{
			ID: "event-1", ItemKind: WorkItemIssue, ItemID: "issue-1", Actor: "alice",
			Kind: EventLabeled, CreatedAt: time.Now().UTC(),
			Payload: WorkItemEventPayload{Label: &Label{Name: "bug", Color: "d73a4a"}},
		}},
	}
	handler := newTestAPI(store)
	response := doRequest(
		handler, http.MethodGet,
		"/api/v1/repositories/acme/game/issues/7/events?limit=5", "",
		"Authorization", "Bearer alice",
	)
	if response.Code != http.StatusOK || store.itemKind != WorkItemIssue ||
		store.number != 7 || store.page.Limit != 5 {
		t.Fatalf("issue events status = %d, kind = %q, number = %d, page = %#v",
			response.Code, store.itemKind, store.number, store.page)
	}
	var decoded Result[WorkItemEvent]
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode issue events: %v", err)
	}
	if len(decoded.Items) != 1 || decoded.Items[0].Kind != EventLabeled ||
		decoded.Items[0].Payload.Label == nil || decoded.Items[0].Payload.Label.Name != "bug" {
		t.Fatalf("decoded issue events = %#v", decoded.Items)
	}

	response = doRequest(
		handler, http.MethodGet,
		"/api/v1/repositories/acme/game/merge-requests/3/events", "",
		"Authorization", "Bearer alice",
	)
	if response.Code != http.StatusOK || store.itemKind != WorkItemMergeRequest || store.number != 3 {
		t.Fatalf("pull request events status = %d, kind = %q, number = %d",
			response.Code, store.itemKind, store.number)
	}
}

func TestWorkItemEventsAllowAnonymousReadersAndRejectBadInput(t *testing.T) {
	store := &eventHandlerStore{Store: assigneeBaseStore(PermRead)}
	handler := newTestAPI(store)
	response := doRequest(handler, http.MethodGet, "/api/v1/repositories/acme/game/issues/7/events", "")
	if response.Code != http.StatusOK || store.calls != 1 {
		t.Fatalf("anonymous status = %d, calls = %d", response.Code, store.calls)
	}
	response = doRequest(
		handler, http.MethodGet, "/api/v1/repositories/acme/game/issues/7/events?limit=0", "",
	)
	if response.Code != http.StatusBadRequest || errorCode(t, response) != "invalid_input" {
		t.Fatalf("invalid limit status = %d, body = %s", response.Code, response.Body.String())
	}
	response = doRequest(handler, http.MethodGet, "/api/v1/repositories/acme/game/issues/x/events", "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("invalid number status = %d", response.Code)
	}
	store.listErr = platform.ErrNotFound
	response = doRequest(handler, http.MethodGet, "/api/v1/repositories/acme/game/issues/9/events", "")
	if response.Code != http.StatusNotFound || errorCode(t, response) != "not_found" {
		t.Fatalf("missing issue status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestWorkItemEventRoutesFailClosedWithoutStore(t *testing.T) {
	response := doRequest(
		newTestAPI(assigneeBaseStore(PermRead)), http.MethodGet,
		"/api/v1/repositories/acme/game/merge-requests/1/events", "",
	)
	if response.Code != http.StatusServiceUnavailable || errorCode(t, response) != "events_unavailable" {
		t.Fatalf("missing store status = %d, body = %s", response.Code, response.Body.String())
	}
}
