package releases

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
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const testRevision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type fakeStore struct {
	created       *CreateInput
	page          ReleasePage
	listDrafts    bool
	createError   error
	publishError  error
	lastVersion   int64
	lastReleaseID string
}

func (store *fakeStore) List(
	_ context.Context,
	_ string,
	includeDrafts bool,
	_, _ int,
) (ReleasePage, error) {
	store.listDrafts = includeDrafts
	return store.page, nil
}

func (store *fakeStore) Get(context.Context, string, string, bool) (Release, error) {
	return Release{}, platform.ErrNotFound
}

func (store *fakeStore) Create(
	_ context.Context,
	_ platform.User,
	_ RepositoryRef,
	input CreateInput,
) (Release, error) {
	store.created = &input
	if store.createError != nil {
		return Release{}, store.createError
	}
	return Release{ID: uuid.NewString(), TagName: input.TagName, Revision: input.Revision, Version: 1}, nil
}

func (store *fakeStore) Update(
	context.Context,
	platform.User,
	RepositoryRef,
	string,
	UpdateInput,
) (Release, error) {
	return Release{}, nil
}

func (store *fakeStore) Publish(
	_ context.Context,
	_ platform.User,
	_ RepositoryRef,
	releaseID string,
	expectedVersion int64,
) (Release, error) {
	store.lastReleaseID = releaseID
	store.lastVersion = expectedVersion
	return Release{ID: releaseID, State: "published", Version: expectedVersion + 1}, store.publishError
}

func (store *fakeStore) Delete(context.Context, platform.User, RepositoryRef, string, int64) error {
	return nil
}

func (store *fakeStore) AddAsset(
	context.Context,
	platform.User,
	RepositoryRef,
	string,
	AssetInput,
) (Release, error) {
	return Release{}, nil
}

func (store *fakeStore) DeleteAsset(
	context.Context,
	platform.User,
	RepositoryRef,
	string,
	string,
	int64,
) (Release, error) {
	return Release{}, nil
}

type fakeRepositories struct {
	repository collab.Repository
	access     collab.Access
}

func (repositories fakeRepositories) LookupRepository(
	_ context.Context,
	_ *platform.User,
	_, _ string,
) (collab.Repository, error) {
	return repositories.repository, nil
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

type fakeLore struct {
	branches []loreclient.Branch
	err      error
}

func (client fakeLore) RepositoryInfo(
	context.Context,
	string,
	loreclient.Credential,
) (loreclient.Repository, error) {
	return loreclient.Repository{}, nil
}

func (client fakeLore) Branches(
	context.Context,
	loreclient.RepositoryRef,
	loreclient.Credential,
) ([]loreclient.Branch, error) {
	return client.branches, client.err
}

type fakeCredentials struct {
	request *loreclient.CredentialRequest
}

func (credentials *fakeCredentials) ForRepository(
	_ context.Context,
	request loreclient.CredentialRequest,
) (loreclient.Credential, error) {
	credentials.request = &request
	return loreclient.Credential{}, nil
}

func TestCreateReleasePinsCurrentLoreRevision(t *testing.T) {
	store := &fakeStore{}
	credentials := &fakeCredentials{}
	repository := testRepository()
	handler := testHandler(store, &testActor, collab.PermWrite, fakeLore{branches: []loreclient.Branch{{
		Name: "main", LatestRevision: testRevision,
	}}}, credentials)
	response := performJSON(t, handler, http.MethodPost,
		"/api/v1/repositories/acme/game/releases", map[string]any{
			"tagName": "v1.0.0", "title": "Version 1", "notes": "Stable",
			"sourceBranch": "main", "revision": testRevision, "state": "draft",
		})
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if store.created == nil || store.created.Revision != testRevision {
		t.Fatalf("created input = %#v", store.created)
	}
	if credentials.request == nil || credentials.request.Scope != loreclient.ScopeRead ||
		credentials.request.Repository.LoreRepositoryID != repository.LoreRepositoryID {
		t.Fatalf("credential request = %#v", credentials.request)
	}
}

func TestCreateReleaseRejectsChangedOrArchivedLoreBranch(t *testing.T) {
	for name, branch := range map[string]loreclient.Branch{
		"changed":  {Name: "main", LatestRevision: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		"archived": {Name: "main", LatestRevision: testRevision, Archived: true},
	} {
		t.Run(name, func(t *testing.T) {
			store := &fakeStore{}
			handler := testHandler(store, &testActor, collab.PermWrite,
				fakeLore{branches: []loreclient.Branch{branch}}, &fakeCredentials{})
			response := performJSON(t, handler, http.MethodPost,
				"/api/v1/repositories/acme/game/releases", map[string]any{
					"tagName": "v1", "title": "Version 1", "notes": "",
					"sourceBranch": "main", "revision": testRevision, "state": "draft",
				})
			if response.Code != http.StatusConflict || store.created != nil {
				t.Fatalf("status = %d, created = %#v", response.Code, store.created)
			}
		})
	}
}

func TestListReleasesHidesDraftsWithoutWriteAccess(t *testing.T) {
	for name, fixture := range map[string]struct {
		actor      *platform.User
		permission collab.Permission
		wantDrafts bool
	}{
		"anonymous": {actor: nil, permission: collab.PermNone, wantDrafts: false},
		"reader":    {actor: &testActor, permission: collab.PermRead, wantDrafts: false},
		"writer":    {actor: &testActor, permission: collab.PermWrite, wantDrafts: true},
	} {
		t.Run(name, func(t *testing.T) {
			store := &fakeStore{page: ReleasePage{Releases: []Release{{ID: uuid.NewString()}}}}
			handler := testHandler(store, fixture.actor, fixture.permission, fakeLore{}, &fakeCredentials{})
			request := httptest.NewRequest(http.MethodGet,
				"/api/v1/repositories/acme/game/releases?page=1&perPage=25", nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusOK || store.listDrafts != fixture.wantDrafts {
				t.Fatalf("status = %d, include drafts = %v", response.Code, store.listDrafts)
			}
			var page ReleasePage
			if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if page.ViewerCanWrite != fixture.wantDrafts ||
				page.Releases[0].ViewerCanWrite != fixture.wantDrafts {
				t.Fatalf("page permissions = %#v", page)
			}
		})
	}
}

func TestPublishReleaseReportsOptimisticConflict(t *testing.T) {
	store := &fakeStore{publishError: ErrVersionConflict}
	handler := testHandler(store, &testActor, collab.PermWrite, fakeLore{}, &fakeCredentials{})
	releaseID := uuid.NewString()
	response := performJSON(t, handler, http.MethodPost,
		"/api/v1/repositories/acme/game/releases/"+releaseID+"/publish",
		map[string]any{"expectedVersion": 4})
	if response.Code != http.StatusConflict || store.lastVersion != 4 || store.lastReleaseID != releaseID {
		t.Fatalf("status = %d, version = %d, release = %s", response.Code, store.lastVersion,
			store.lastReleaseID)
	}
}

func TestCreateReleaseRequiresWritePermissionAndStrictJSON(t *testing.T) {
	store := &fakeStore{}
	handler := testHandler(store, &testActor, collab.PermRead, fakeLore{}, &fakeCredentials{})
	response := performJSON(t, handler, http.MethodPost,
		"/api/v1/repositories/acme/game/releases", map[string]string{"unknown": "value"})
	if response.Code != http.StatusForbidden || store.created != nil {
		t.Fatalf("status = %d, created = %#v", response.Code, store.created)
	}

	handler = testHandler(store, &testActor, collab.PermWrite, fakeLore{}, &fakeCredentials{})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/repositories/acme/game/releases",
		bytes.NewBufferString(`{"unknown":"value"}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

var testActor = platform.User{ID: uuid.MustParse("00000000-0000-4000-8000-000000000101").String(), Username: "alice"}

func testRepository() collab.Repository {
	return collab.Repository{
		ID:             uuid.MustParse("00000000-0000-4000-8000-000000000201").String(),
		OrganizationID: uuid.MustParse("00000000-0000-4000-8000-000000000301").String(),
		Owner:          "acme", Slug: "game", Visibility: "private",
		LoreRepositoryID: "11111111111111111111111111111111",
		LoreURL:          "https://lore.invalid/11111111111111111111111111111111",
		DefaultBranch:    "main",
	}
}

func testHandler(
	store Store,
	actor *platform.User,
	permission collab.Permission,
	lore loreclient.Client,
	credentials loreclient.CredentialProvider,
) http.Handler {
	mux := http.NewServeMux()
	Register(mux, store, fakeRepositories{repository: testRepository(), access: collab.Access{
		Permission: permission,
	}}, fakeActors{actor: actor}, lore, credentials, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return mux
}

func performJSON(
	t *testing.T,
	handler http.Handler,
	method string,
	path string,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
