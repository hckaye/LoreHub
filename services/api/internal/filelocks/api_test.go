package filelocks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/authz"
	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const testPartition = "0123456789abcdef0123456789abcdef"

type fakeStore struct {
	access collab.Access
	err    error
}

func (store fakeStore) LookupRepository(
	context.Context,
	*platform.User,
	string,
	string,
) (collab.Repository, error) {
	if store.err != nil {
		return collab.Repository{}, store.err
	}
	return collab.Repository{
		ID: "repository-id", Owner: "studio", Slug: "game", DefaultBranch: "main",
		LoreRepositoryID: testPartition, LoreURL: "lore://localhost/" + testPartition,
	}, nil
}

func (store fakeStore) RepositoryPermission(
	context.Context,
	platform.User,
	collab.Repository,
) (collab.Access, error) {
	return store.access, store.err
}

type fakeActors struct {
	actor *platform.User
}

func (actors fakeActors) ResolveActor(http.ResponseWriter, *http.Request) (platform.User, bool) {
	if actors.actor == nil {
		return platform.User{}, false
	}
	return *actors.actor, true
}

func (actors fakeActors) ResolveOptionalActor(
	http.ResponseWriter,
	*http.Request,
) (*platform.User, bool) {
	return actors.actor, true
}

type fakeUsers struct{}

func (fakeUsers) UserInfoForResource(
	context.Context,
	string,
	[]string,
) ([]authz.UserInfo, error) {
	return []authz.UserInfo{{ID: "user-id", Username: "alice", DisplayName: "Alice"}}, nil
}

type fakeCredentials struct {
	request *loreclient.CredentialRequest
}

func (provider fakeCredentials) ForRepository(
	_ context.Context,
	request loreclient.CredentialRequest,
) (loreclient.Credential, error) {
	if provider.request != nil {
		*provider.request = request
	}
	return loreclient.Credential{
		Partition: request.Partition, Scope: request.Scope,
		Identity: request.Principal.UserID, Principal: request.Principal, InsecureDevelopment: true,
	}, nil
}

type fakeObservations struct {
	acquired *int
	released *int
}

func (store fakeObservations) RecordLoreFileLockAcquisition(
	context.Context, string, string, string, string, string, time.Time,
) error {
	if store.acquired != nil {
		*store.acquired++
	}
	return nil
}

func (store fakeObservations) RecordLoreFileLockRelease(
	context.Context, string, string, string, string, string, time.Time,
) error {
	if store.released != nil {
		*store.released++
	}
	return nil
}

type fakeLore struct {
	locks         []loreclient.FileLock
	mutationErr   error
	ownerOverride *bool
}

func (lore fakeLore) RepositoryInfo(
	context.Context,
	string,
	loreclient.Credential,
) (loreclient.Repository, error) {
	return loreclient.Repository{}, nil
}

func (lore fakeLore) Branches(
	context.Context,
	loreclient.RepositoryRef,
	loreclient.Credential,
) ([]loreclient.Branch, error) {
	return []loreclient.Branch{{ID: "branch-id", Name: "main", LatestRevision: strings.Repeat("a", 64)}}, nil
}

func (lore fakeLore) QueryFileLocks(
	context.Context,
	loreclient.RepositoryRef,
	string,
	string,
	string,
	loreclient.Credential,
) ([]loreclient.FileLock, error) {
	return lore.locks, nil
}

func (lore fakeLore) AcquireFileLock(
	context.Context,
	loreclient.RepositoryRef,
	string,
	string,
	loreclient.Credential,
) (loreclient.FileLock, error) {
	if lore.mutationErr != nil {
		return loreclient.FileLock{}, lore.mutationErr
	}
	return lore.locks[0], nil
}

func (lore fakeLore) ReleaseFileLock(
	_ context.Context,
	_ loreclient.RepositoryRef,
	_ string,
	_ string,
	_ loreclient.Credential,
	allowOwnerOverride bool,
) (loreclient.FileLock, error) {
	if lore.ownerOverride != nil {
		*lore.ownerOverride = allowOwnerOverride
	}
	if lore.mutationErr != nil {
		return loreclient.FileLock{}, lore.mutationErr
	}
	return lore.locks[0], nil
}

func TestRepositoryAdminCanReleaseAnotherUsersLock(t *testing.T) {
	actor := platform.User{ID: "admin-id", Username: "admin"}
	lock := loreclient.FileLock{
		BranchID: "branch-id", Path: "Content/Hero.uasset",
		OwnerID: "owner-id", LockedAt: time.Now().UTC(),
	}
	override := false
	released := 0
	var issued loreclient.CredentialRequest
	handler := testHandler(
		t, fakeStore{access: collab.Access{Permission: collab.PermAdmin}}, fakeActors{actor: &actor},
		fakeLore{locks: []loreclient.FileLock{lock}, ownerOverride: &override},
		fakeCredentials{request: &issued}, fakeObservations{released: &released},
	)
	request := httptest.NewRequest(
		http.MethodDelete, "/api/v1/repositories/studio/game/locks",
		strings.NewReader(`{"branch":"main","path":"Content/Hero.uasset"}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || !override || released != 1 {
		t.Fatalf(
			"status = %d, override = %t, observations = %d, body = %s",
			response.Code, override, released, response.Body.String(),
		)
	}
	if issued.Scope != loreclient.ScopeAdmin || issued.Principal.UserID != actor.ID {
		t.Fatalf("credential request = %#v", issued)
	}
}

func TestListPublicFileLocksUsesReadOnlyServiceCredential(t *testing.T) {
	lockedAt := time.Date(2026, 8, 12, 3, 0, 0, 0, time.UTC)
	lock := loreclient.FileLock{
		BranchID: "branch-id", Path: "Content/Hero.uasset", OwnerID: "user-id", LockedAt: lockedAt,
	}
	var issued loreclient.CredentialRequest
	handler := testHandler(
		t, fakeStore{}, fakeActors{}, fakeLore{locks: []loreclient.FileLock{lock}},
		fakeCredentials{request: &issued}, fakeObservations{},
	)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/repositories/studio/game/locks", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if issued.Scope != loreclient.ScopeRead ||
		issued.Principal.ServicePurpose != loreclient.ServicePurposePublicReader {
		t.Fatalf("credential request = %#v", issued)
	}
	var page lockPage
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Locks) != 1 || page.Locks[0].Owner.Username != "alice" ||
		page.ViewerCanLock || page.Locks[0].ViewerCanUnlock {
		t.Fatalf("page = %#v", page)
	}
}

func TestAcquireFileLockRequiresWriteAndRecordsObservation(t *testing.T) {
	actor := platform.User{ID: "user-id", Username: "alice"}
	lock := loreclient.FileLock{
		BranchID: "branch-id", Path: "Content/Hero.uasset",
		OwnerID: actor.ID, LockedAt: time.Now().UTC(),
	}
	acquired := 0
	var issued loreclient.CredentialRequest
	handler := testHandler(
		t, fakeStore{access: collab.Access{Permission: collab.PermWrite}}, fakeActors{actor: &actor},
		fakeLore{locks: []loreclient.FileLock{lock}}, fakeCredentials{request: &issued},
		fakeObservations{acquired: &acquired},
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/repositories/studio/game/locks",
		strings.NewReader(`{"branch":"main","path":"Content/Hero.uasset"}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || acquired != 1 {
		t.Fatalf("status = %d, observations = %d, body = %s", response.Code, acquired, response.Body.String())
	}
	if issued.Scope != loreclient.ScopeWrite || issued.Principal.UserID != actor.ID {
		t.Fatalf("credential request = %#v", issued)
	}
}

func TestFileLockMutationErrorsAreSpecific(t *testing.T) {
	actor := platform.User{ID: "user-id", Username: "alice"}
	tests := []struct {
		name   string
		err    error
		status int
	}{
		{name: "conflict", err: loreclient.ErrFileLockConflict, status: http.StatusConflict},
		{name: "not owned", err: loreclient.ErrFileLockNotOwned, status: http.StatusConflict},
		{name: "not found", err: loreclient.ErrFileLockNotFound, status: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := testHandler(
				t, fakeStore{access: collab.Access{Permission: collab.PermWrite}}, fakeActors{actor: &actor},
				fakeLore{mutationErr: test.err}, fakeCredentials{}, fakeObservations{},
			)
			request := httptest.NewRequest(
				http.MethodPost, "/api/v1/repositories/studio/game/locks",
				strings.NewReader(`{"branch":"main","path":"Content/Hero.uasset"}`),
			)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d, body = %s", response.Code, test.status, response.Body.String())
			}
		})
	}
}

func TestAcquireFileLockRejectsInvalidInputAndReadOnlyActor(t *testing.T) {
	actor := platform.User{ID: "user-id", Username: "alice"}
	handler := testHandler(
		t, fakeStore{access: collab.Access{Permission: collab.PermRead}}, fakeActors{actor: &actor},
		fakeLore{}, fakeCredentials{}, fakeObservations{},
	)
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/repositories/studio/game/locks",
		strings.NewReader(`{"branch":"main","path":"../secret"}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}

	handler = testHandler(
		t, fakeStore{access: collab.Access{Permission: collab.PermWrite}}, fakeActors{actor: &actor},
		fakeLore{}, fakeCredentials{}, fakeObservations{},
	)
	request = httptest.NewRequest(
		http.MethodPost, "/api/v1/repositories/studio/game/locks",
		strings.NewReader(`{"branch":"main","path":"../secret"}`),
	)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func testHandler(
	t *testing.T,
	store Store,
	actors collab.ActorResolver,
	lore LoreClient,
	credentials loreclient.CredentialProvider,
	observations ObservationStore,
) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, store, fakeUsers{}, observations, actors, lore, credentials, "public-reader", nil)
	return mux
}
