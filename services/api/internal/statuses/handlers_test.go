package statuses

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type fakeStatusStore struct {
	page        Page
	result      CreateResult
	err         error
	createdWith CreateInput
}

func (store *fakeStatusStore) List(
	context.Context,
	string,
	string,
	int,
	int,
) (Page, error) {
	return store.page, store.err
}

func (store *fakeStatusStore) Create(
	_ context.Context,
	_ platform.User,
	_ RepositoryRef,
	input CreateInput,
) (CreateResult, error) {
	store.createdWith = input
	return store.result, store.err
}

type fakeRepositories struct {
	repository collab.Repository
	err        error
}

func (repositories fakeRepositories) LookupRepository(
	context.Context,
	*platform.User,
	string,
	string,
) (collab.Repository, error) {
	return repositories.repository, repositories.err
}

type fakeActors struct {
	actor *platform.User
}

func (actors fakeActors) ResolveActor(
	writer http.ResponseWriter,
	_ *http.Request,
) (platform.User, bool) {
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

type fakeCredentials struct {
	request loreclient.CredentialRequest
	err     error
}

func (credentials *fakeCredentials) ForRepository(
	_ context.Context,
	request loreclient.CredentialRequest,
) (loreclient.Credential, error) {
	credentials.request = request
	return loreclient.Credential{}, credentials.err
}

type fakeCode struct {
	revision loreclient.Revision
	err      error
}

func (code fakeCode) Tree(
	context.Context,
	loreclient.RepositoryRef,
	string,
	string,
	loreclient.Credential,
	int,
) (loreclient.Tree, error) {
	return loreclient.Tree{}, errors.New("unexpected Tree call")
}

func (code fakeCode) File(
	context.Context,
	loreclient.RepositoryRef,
	string,
	string,
	loreclient.Credential,
	int64,
) (loreclient.File, []byte, error) {
	return loreclient.File{}, nil, errors.New("unexpected File call")
}

func (code fakeCode) RevisionHistory(
	context.Context,
	loreclient.RepositoryRef,
	string,
	string,
	loreclient.Credential,
	int,
) ([]loreclient.RevisionHistoryEntry, error) {
	return nil, errors.New("unexpected RevisionHistory call")
}

func (code fakeCode) FileHistory(
	context.Context,
	loreclient.RepositoryRef,
	string,
	string,
	string,
	loreclient.Credential,
	int,
) ([]loreclient.FileHistoryEntry, error) {
	return nil, errors.New("unexpected FileHistory call")
}

func (code fakeCode) RevisionInfo(
	context.Context,
	loreclient.RepositoryRef,
	string,
	loreclient.Credential,
) (loreclient.Revision, error) {
	return code.revision, code.err
}

func (code fakeCode) RevisionDiff(
	context.Context,
	loreclient.RepositoryRef,
	string,
	string,
	[]string,
	loreclient.Credential,
	int,
	int,
) (loreclient.Diff, error) {
	return loreclient.Diff{}, errors.New("unexpected RevisionDiff call")
}

func testAPI(
	store Store,
	repositories RepositoryStore,
	actors collab.ActorResolver,
	code loreclient.CodeClient,
	credentials loreclient.CredentialProvider,
) *API {
	return NewAPI(
		store, repositories, actors, code, credentials,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
}

func testRepository() collab.Repository {
	return collab.Repository{
		ID:             "11111111-1111-4111-8111-111111111111",
		OrganizationID: "22222222-2222-4222-8222-222222222222",
		Owner:          "acme", Slug: "game", LoreRepositoryID: strings.Repeat("a", 32),
		LoreURL: "lore://localhost/" + strings.Repeat("a", 32), DefaultBranch: "main",
	}
}

func testActor() platform.User {
	return platform.User{
		ID:       "33333333-3333-4333-8333-333333333333",
		Username: "writer", DisplayName: "Writer",
	}
}

func statusRequest(method string, path string, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func TestCreateStatusVerifiesLoreRevision(t *testing.T) {
	store := &fakeStatusStore{result: CreateResult{
		Status: Status{ID: "status-id", Revision: testRevision}, Created: true,
	}}
	actor := testActor()
	credentials := &fakeCredentials{}
	api := testAPI(
		store,
		fakeRepositories{repository: testRepository()},
		fakeActors{actor: &actor}, fakeCode{revision: loreclient.Revision{Revision: testRevision}},
		credentials,
	)
	writer := httptest.NewRecorder()
	request := statusRequest(
		http.MethodPost,
		"/api/v1/repositories/acme/game/revisions/"+testRevision+"/statuses",
		`{"state":"success","context":"CI/Test"}`,
	)
	request.SetPathValue("owner", "acme")
	request.SetPathValue("repository", "game")
	request.SetPathValue("revision", testRevision)
	api.createStatus(writer, request)
	if writer.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", writer.Code, writer.Body.String())
	}
	if credentials.request.Scope != loreclient.ScopeRead ||
		credentials.request.Principal.UserID != actor.ID {
		t.Fatalf("credential request = %#v", credentials.request)
	}
	if store.createdWith.Context != "CI/Test" || store.createdWith.Revision != testRevision {
		t.Fatalf("create input = %#v", store.createdWith)
	}
}

func TestCreateStatusFailsClosedWithoutLore(t *testing.T) {
	actor := testActor()
	api := testAPI(
		&fakeStatusStore{},
		fakeRepositories{repository: testRepository()},
		fakeActors{actor: &actor}, nil, nil,
	)
	writer := httptest.NewRecorder()
	request := statusRequest(http.MethodPost, "/", `{"state":"pending"}`)
	request.SetPathValue("revision", testRevision)
	api.createStatus(writer, request)
	if writer.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", writer.Code, writer.Body.String())
	}
}

func TestCreateStatusReturnsNotFoundForMissingLoreRevision(t *testing.T) {
	actor := testActor()
	api := testAPI(
		&fakeStatusStore{},
		fakeRepositories{repository: testRepository()},
		fakeActors{actor: &actor}, fakeCode{err: loreclient.ErrNotFound}, &fakeCredentials{},
	)
	writer := httptest.NewRecorder()
	request := statusRequest(http.MethodPost, "/", `{"state":"pending"}`)
	request.SetPathValue("revision", testRevision)
	api.createStatus(writer, request)
	if writer.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", writer.Code, writer.Body.String())
	}
}

func TestCreateStatusRejectsArchivedRepositoryBeforeStore(t *testing.T) {
	actor := testActor()
	archivedAt := time.Now()
	repository := testRepository()
	repository.ArchivedAt = &archivedAt
	api := testAPI(
		&fakeStatusStore{}, fakeRepositories{repository: repository},
		fakeActors{actor: &actor}, fakeCode{}, &fakeCredentials{},
	)
	writer := httptest.NewRecorder()
	request := statusRequest(http.MethodPost, "/", `{"state":"success"}`)
	request.SetPathValue("revision", testRevision)
	api.createStatus(writer, request)
	if writer.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", writer.Code, writer.Body.String())
	}
}

func TestCreateStatusRejectsUnknownJSONField(t *testing.T) {
	actor := testActor()
	api := testAPI(
		&fakeStatusStore{},
		fakeRepositories{repository: testRepository()},
		fakeActors{actor: &actor}, fakeCode{}, &fakeCredentials{},
	)
	writer := httptest.NewRecorder()
	request := statusRequest(http.MethodPost, "/", `{"state":"success","unknown":true}`)
	request.SetPathValue("revision", testRevision)
	api.createStatus(writer, request)
	if writer.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", writer.Code, writer.Body.String())
	}
}

func TestGitHubCreateShapeAndIdempotencyHeader(t *testing.T) {
	store := &fakeStatusStore{result: CreateResult{
		Status: Status{
			ID: "status-id", State: "failure", Context: "build",
			Description: "failed", TargetURL: "https://ci.example/runs/1",
			Creator: Creator{ID: "user-id", Username: "writer"}, CreatedAt: time.Now(),
		},
		Created: true,
	}}
	actor := testActor()
	api := testAPI(
		store,
		fakeRepositories{repository: testRepository()},
		fakeActors{actor: &actor}, fakeCode{revision: loreclient.Revision{Revision: testRevision}},
		&fakeCredentials{},
	)
	writer := httptest.NewRecorder()
	request := statusRequest(
		http.MethodPost, "/api/v3/repos/acme/game/statuses/"+testRevision,
		`{"state":"failure","target_url":"https://ci.example/runs/1",`+
			`"description":"failed","context":"build"}`,
	)
	request.Header.Set("Idempotency-Key", "delivery-1")
	request.SetPathValue("revision", testRevision)
	api.createGitHubStatus(writer, request)
	if writer.Code != http.StatusCreated || !strings.Contains(writer.Body.String(), `"target_url"`) {
		t.Fatalf("status = %d, body = %s", writer.Code, writer.Body.String())
	}
	if store.createdWith.IdempotencyKey == nil || *store.createdWith.IdempotencyKey != "delivery-1" {
		t.Fatalf("idempotency key = %#v", store.createdWith.IdempotencyKey)
	}
}

func TestListStatusesAllowsAnonymousRepositoryLookup(t *testing.T) {
	store := &fakeStatusStore{page: Page{Revision: testRevision, State: "pending"}}
	api := testAPI(store, fakeRepositories{repository: testRepository()}, fakeActors{}, nil, nil)
	writer := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet, "/api/v1/repositories/acme/game/revisions/"+testRevision+"/statuses", nil,
	)
	request.SetPathValue("revision", testRevision)
	api.listStatuses(writer, request)
	if writer.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", writer.Code, writer.Body.String())
	}
}

func TestListStatusesDoesNotExposeInvisibleRepository(t *testing.T) {
	api := testAPI(
		&fakeStatusStore{}, fakeRepositories{err: platform.ErrNotFound}, fakeActors{}, nil, nil,
	)
	writer := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.SetPathValue("revision", testRevision)
	api.listStatuses(writer, request)
	if writer.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", writer.Code, writer.Body.String())
	}
}
