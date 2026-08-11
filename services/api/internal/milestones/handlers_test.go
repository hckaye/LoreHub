package milestones

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/collab"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type fakeStore struct {
	page            MilestonePage
	listState       string
	created         *CreateInput
	updated         *UpdateInput
	assignedIssue   int64
	assignedNumber  int64
	createError     error
	updateError     error
	assignmentError error
}

func (store *fakeStore) List(
	_ context.Context,
	_ string,
	state string,
	_, _ int,
) (MilestonePage, error) {
	store.listState = state
	return store.page, nil
}

func (store *fakeStore) Get(context.Context, string, int64) (Milestone, error) {
	return Milestone{ID: uuid.NewString(), Number: 1, Title: "Version 1"}, nil
}

func (store *fakeStore) Create(
	_ context.Context,
	_ platform.User,
	_ RepositoryRef,
	input CreateInput,
) (Milestone, error) {
	store.created = &input
	return Milestone{ID: uuid.NewString(), Number: 3, Title: input.Title, Version: 1}, store.createError
}

func (store *fakeStore) Update(
	_ context.Context,
	_ platform.User,
	_ RepositoryRef,
	_ int64,
	input UpdateInput,
) (Milestone, error) {
	store.updated = &input
	return Milestone{ID: uuid.NewString(), Number: 1, Version: input.ExpectedVersion + 1}, store.updateError
}

func (store *fakeStore) Delete(context.Context, platform.User, RepositoryRef, int64, int64) error {
	return nil
}

func (store *fakeStore) AssignIssue(
	_ context.Context,
	_ platform.User,
	_ RepositoryRef,
	issueNumber int64,
	milestoneNumber int64,
) (collab.MilestoneSummary, error) {
	store.assignedIssue = issueNumber
	store.assignedNumber = milestoneNumber
	return collab.MilestoneSummary{ID: uuid.NewString(), Number: milestoneNumber}, store.assignmentError
}

func (store *fakeStore) RemoveIssue(context.Context, platform.User, RepositoryRef, int64) error {
	return nil
}

type fakeRepositories struct {
	access collab.Access
}

func (repositories fakeRepositories) LookupRepository(
	context.Context,
	*platform.User,
	string,
	string,
) (collab.Repository, error) {
	return collab.Repository{
		ID:             uuid.MustParse("00000000-0000-4000-8000-000000000201").String(),
		OrganizationID: uuid.MustParse("00000000-0000-4000-8000-000000000301").String(),
		Owner:          "acme", Slug: "game", Visibility: "private",
	}, nil
}

func (repositories fakeRepositories) RepositoryPermission(
	context.Context,
	platform.User,
	collab.Repository,
) (collab.Access, error) {
	return repositories.access, nil
}

type fakeActors struct {
	actor *platform.User
}

func (actors fakeActors) ResolveActor(writer http.ResponseWriter, _ *http.Request) (platform.User, bool) {
	if actors.actor == nil {
		writeProblem(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required")
		return platform.User{}, false
	}
	return *actors.actor, true
}

func (actors fakeActors) ResolveOptionalActor(
	_ http.ResponseWriter,
	_ *http.Request,
) (*platform.User, bool) {
	return actors.actor, true
}

var testActor = platform.User{
	ID: uuid.MustParse("00000000-0000-4000-8000-000000000101").String(), Username: "alice",
}

func TestListMilestonesUsesOpenDefaultAndViewerPermission(t *testing.T) {
	store := &fakeStore{page: MilestonePage{Milestones: []Milestone{{Number: 1}}}}
	handler := testHandler(store, &testActor, collab.PermWrite)
	response := performRequest(t, handler, http.MethodGet,
		"/api/v1/repositories/acme/game/milestones", nil)
	if response.Code != http.StatusOK || store.listState != "open" {
		t.Fatalf("status = %d, state = %q", response.Code, store.listState)
	}
	var page MilestonePage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if !page.ViewerCanWrite || len(page.Milestones) != 1 || !page.Milestones[0].ViewerCanWrite {
		t.Fatalf("page permissions = %#v", page)
	}

	response = performRequest(t, handler, http.MethodGet,
		"/api/v1/repositories/acme/game/milestones?state=invalid", nil)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid state status = %d", response.Code)
	}
}

func TestCreateMilestoneRequiresWriteAndStrictJSON(t *testing.T) {
	store := &fakeStore{}
	reader := testHandler(store, &testActor, collab.PermRead)
	response := performRequest(t, reader, http.MethodPost,
		"/api/v1/repositories/acme/game/milestones", map[string]any{"title": "Version 1"})
	if response.Code != http.StatusForbidden || store.created != nil {
		t.Fatalf("reader status = %d, created = %#v", response.Code, store.created)
	}

	writer := testHandler(store, &testActor, collab.PermWrite)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/repositories/acme/game/milestones",
		bytes.NewBufferString(`{"title":"Version 1","unknown":true}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	writer.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("strict JSON status = %d, body = %s", response.Code, response.Body.String())
	}

	response = performRequest(t, writer, http.MethodPost,
		"/api/v1/repositories/acme/game/milestones", map[string]any{
			"title": "Version 1", "description": "Scope", "dueOn": "2026-12-31",
		})
	if response.Code != http.StatusCreated || store.created == nil || store.created.Title != "Version 1" {
		t.Fatalf("writer status = %d, created = %#v", response.Code, store.created)
	}
	if location := response.Header().Get("Location"); location != "/api/v1/repositories/acme/game/milestones/3" {
		t.Fatalf("location = %q", location)
	}
}

func TestUpdateMilestoneDistinguishesOmittedAndNullDueDate(t *testing.T) {
	store := &fakeStore{}
	handler := testHandler(store, &testActor, collab.PermWrite)
	path := "/api/v1/repositories/acme/game/milestones/1"
	response := performRequest(t, handler, http.MethodPatch, path, map[string]any{
		"title": "Updated", "expectedVersion": 2,
	})
	if response.Code != http.StatusOK || store.updated == nil || store.updated.DueOnSet {
		t.Fatalf("omitted due date update = %#v, status = %d", store.updated, response.Code)
	}
	response = performRequest(t, handler, http.MethodPatch, path, map[string]any{
		"dueOn": nil, "expectedVersion": 3,
	})
	if response.Code != http.StatusOK || store.updated == nil || !store.updated.DueOnSet || store.updated.DueOn != nil {
		t.Fatalf("cleared due date update = %#v, status = %d", store.updated, response.Code)
	}
}

func TestIssueMilestoneAssignmentRequiresTriage(t *testing.T) {
	store := &fakeStore{}
	path := "/api/v1/repositories/acme/game/issues/9/milestone/4"
	response := performRequest(t, testHandler(store, &testActor, collab.PermRead), http.MethodPut, path, nil)
	if response.Code != http.StatusForbidden {
		t.Fatalf("reader status = %d", response.Code)
	}
	response = performRequest(t, testHandler(store, &testActor, collab.PermTriage), http.MethodPut, path, nil)
	if response.Code != http.StatusOK || store.assignedIssue != 9 || store.assignedNumber != 4 {
		t.Fatalf("triage status = %d, issue = %d, milestone = %d",
			response.Code, store.assignedIssue, store.assignedNumber)
	}
}

func TestMilestoneConflictsHaveStableStatus(t *testing.T) {
	store := &fakeStore{createError: platform.ErrConflict, updateError: ErrVersionConflict}
	handler := testHandler(store, &testActor, collab.PermWrite)
	response := performRequest(t, handler, http.MethodPost,
		"/api/v1/repositories/acme/game/milestones", map[string]any{"title": "Version 1"})
	if response.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d", response.Code)
	}
	response = performRequest(t, handler, http.MethodPatch,
		"/api/v1/repositories/acme/game/milestones/1", map[string]any{
			"title": "Changed", "expectedVersion": 1,
		})
	if response.Code != http.StatusConflict {
		t.Fatalf("version status = %d", response.Code)
	}
}

func testHandler(store Store, actor *platform.User, permission collab.Permission) http.Handler {
	mux := http.NewServeMux()
	Register(mux, store, fakeRepositories{access: collab.Access{Permission: permission}},
		fakeActors{actor: actor}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return mux
}

func performRequest(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()
	var request *http.Request
	if body == nil {
		request = httptest.NewRequest(method, path, nil)
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		request = httptest.NewRequest(method, path, bytes.NewReader(encoded))
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
