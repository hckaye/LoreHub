package collab

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type mergeRequestMetadataHandlerStore struct {
	*fakeStore
	metadata         MergeRequestMetadata
	label            Label
	assignee         Assignee
	milestone        *MilestoneSummary
	removedLabel     bool
	removedAssignee  bool
	removedMilestone bool
}

func (store *mergeRequestMetadataHandlerStore) GetMergeRequestMetadata(
	context.Context, string, int64,
) (MergeRequestMetadata, error) {
	return store.metadata, nil
}

func (store *mergeRequestMetadataHandlerStore) ApplyMergeRequestLabel(
	context.Context, platform.User, string, int64, string,
) (Label, bool, error) {
	return store.label, true, nil
}

func (store *mergeRequestMetadataHandlerStore) RemoveMergeRequestLabel(
	context.Context, platform.User, string, int64, string,
) error {
	store.removedLabel = true
	return nil
}

func (store *mergeRequestMetadataHandlerStore) AssignMergeRequestUser(
	context.Context, platform.User, string, int64, string,
) (Assignee, bool, error) {
	return store.assignee, true, nil
}

func (store *mergeRequestMetadataHandlerStore) RemoveMergeRequestUser(
	context.Context, platform.User, string, int64, string,
) error {
	store.removedAssignee = true
	return nil
}

func (store *mergeRequestMetadataHandlerStore) SetMergeRequestMilestone(
	_ context.Context,
	_ platform.User,
	_ string,
	_ int64,
	number *int64,
) (*MilestoneSummary, bool, error) {
	if number == nil {
		store.removedMilestone = true
		return nil, true, nil
	}
	return store.milestone, true, nil
}

func TestMergeRequestMetadataRoutes(t *testing.T) {
	user := platform.User{ID: uuidNew(), Username: "alice"}
	repository := Repository{ID: uuidNew(), OrganizationID: uuidNew(), Owner: "acme", Slug: "game"}
	labelID := uuidNew()
	milestone := &MilestoneSummary{ID: uuidNew(), Number: 3, Title: "Launch", State: "open"}
	store := &mergeRequestMetadataHandlerStore{
		fakeStore: &fakeStore{
			user: user,
			lookupRepo: func(_ *platform.User, _, _ string) (Repository, error) {
				return repository, nil
			},
			permission: func(platform.User, Repository) (Access, error) {
				return Access{Permission: PermTriage}, nil
			},
		},
		metadata: MergeRequestMetadata{
			Labels:    []Label{{ID: labelID, Name: "bug"}},
			Assignees: []Assignee{{ID: uuidNew(), Username: "bob", DisplayName: "Bob"}},
			Milestone: milestone,
		},
		label:     Label{ID: labelID, Name: "bug"},
		assignee:  Assignee{ID: uuidNew(), Username: "bob", DisplayName: "Bob"},
		milestone: milestone,
	}
	mux := http.NewServeMux()
	Register(mux, store, testActorResolver{store: store}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	base := "/api/v1/repositories/acme/game/merge-requests/7/metadata"

	assertMetadataResponse(t, metadataHTTP(t, mux, http.MethodGet, base, "alice"), http.StatusOK,
		"viewerCanManageLabels", "bug", "bob", "Launch")
	assertMetadataResponse(t, metadataHTTP(t, mux, http.MethodPut, base+"/labels/"+labelID, "alice"),
		http.StatusCreated, "bug")
	assertMetadataResponse(t, metadataHTTP(t, mux, http.MethodPut, base+"/assignees/bob", "alice"),
		http.StatusCreated, "bob")
	assertMetadataResponse(t, metadataHTTP(t, mux, http.MethodPut, base+"/milestone/3", "alice"),
		http.StatusCreated, "Launch")
	assertMetadataResponse(t, metadataHTTP(t, mux, http.MethodDelete, base+"/labels/"+labelID, "alice"),
		http.StatusNoContent)
	assertMetadataResponse(t, metadataHTTP(t, mux, http.MethodDelete, base+"/assignees/bob", "alice"),
		http.StatusNoContent)
	assertMetadataResponse(t, metadataHTTP(t, mux, http.MethodDelete, base+"/milestone", "alice"),
		http.StatusNoContent)
	if !store.removedLabel || !store.removedAssignee || !store.removedMilestone {
		t.Fatal("delete routes did not call all metadata store methods")
	}
}

func TestMergeRequestMetadataRejectsInvalidLabelID(t *testing.T) {
	user := platform.User{ID: uuidNew(), Username: "alice"}
	store := &mergeRequestMetadataHandlerStore{fakeStore: &fakeStore{
		user: user,
		lookupRepo: func(_ *platform.User, _, _ string) (Repository, error) {
			return Repository{ID: uuidNew(), Owner: "acme", Slug: "game"}, nil
		},
	}}
	mux := http.NewServeMux()
	Register(mux, store, testActorResolver{store: store}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	response := metadataHTTP(t, mux, http.MethodPut,
		"/api/v1/repositories/acme/game/merge-requests/7/metadata/labels/not-a-uuid", "alice")
	assertMetadataResponse(t, response, http.StatusNotFound, "not_found")
}

func metadataHTTP(t *testing.T, handler http.Handler, method, path, actor string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	if actor != "" {
		request.Header.Set("Authorization", "Bearer "+actor)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertMetadataResponse(t *testing.T, response *httptest.ResponseRecorder, status int, fragments ...string) {
	t.Helper()
	if response.Code != status || !containsAll(response.Body.String(), fragments...) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}
