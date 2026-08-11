// Package filelocks exposes Lore-native file locks through repository-scoped HTTP APIs.
package filelocks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/authz"
	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const (
	maxRequestBody = 1 << 20
	maxLocks       = 1000
)

type Store interface {
	LookupRepository(context.Context, *platform.User, string, string) (collab.Repository, error)
	RepositoryPermission(context.Context, platform.User, collab.Repository) (collab.Access, error)
}

type UserDirectory interface {
	UserInfoForResource(context.Context, string, []string) ([]authz.UserInfo, error)
}

type ObservationStore interface {
	RecordLoreFileLockAcquisition(context.Context, string, string, string, string, string, time.Time) error
	RecordLoreFileLockRelease(context.Context, string, string, string, string, string, time.Time) error
}

type LoreClient interface {
	loreclient.Client
	loreclient.FileLockClient
}

type API struct {
	store               Store
	users               UserDirectory
	observations        ObservationStore
	actors              collab.ActorResolver
	lore                LoreClient
	credentials         loreclient.CredentialProvider
	publicReaderSubject string
	logger              *slog.Logger
}

func Register(
	mux *http.ServeMux,
	store Store,
	users UserDirectory,
	observations ObservationStore,
	actors collab.ActorResolver,
	lore LoreClient,
	credentials loreclient.CredentialProvider,
	publicReaderSubject string,
	logger *slog.Logger,
) {
	api := &API{
		store: store, users: users, observations: observations, actors: actors,
		lore: lore, credentials: credentials, publicReaderSubject: publicReaderSubject, logger: logger,
	}
	base := "/api/v1/repositories/{owner}/{repository}/locks"
	mux.HandleFunc("GET "+base, api.list)
	mux.HandleFunc("POST "+base, api.acquire)
	mux.HandleFunc("DELETE "+base, api.release)
}

type lockOwner struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

type fileLock struct {
	BranchID        string    `json:"branchId"`
	Branch          string    `json:"branch"`
	Path            string    `json:"path"`
	Owner           lockOwner `json:"owner"`
	LockedAt        time.Time `json:"lockedAt"`
	ViewerCanUnlock bool      `json:"viewerCanUnlock"`
}

type lockPage struct {
	Locks          []fileLock          `json:"locks"`
	Branches       []loreclient.Branch `json:"branches"`
	SelectedBranch string              `json:"selectedBranch"`
	ViewerCanLock  bool                `json:"viewerCanLock"`
	Truncated      bool                `json:"truncated"`
}

type mutationRequest struct {
	Branch string `json:"branch"`
	Path   string `json:"path"`
}

func (api *API) list(writer http.ResponseWriter, request *http.Request) {
	repository, actor, access, ok := api.visibleRepository(writer, request)
	if !ok {
		return
	}
	branchName := strings.TrimSpace(request.URL.Query().Get("branch"))
	if branchName == "" {
		branchName = repository.DefaultBranch
	}
	filePath := strings.TrimSpace(request.URL.Query().Get("path"))
	if !loreclient.ValidBranchName(branchName) || (filePath != "" && !loreclient.ValidFileLockPath(filePath)) {
		problem(writer, http.StatusBadRequest, "invalid_filter", "The file lock filter is invalid")
		return
	}
	credential, ok := api.credential(writer, request, repository, actor, loreclient.ScopeRead)
	if !ok {
		return
	}
	ref := repositoryRef(repository)
	branches, err := api.lore.Branches(request.Context(), ref, credential)
	if err != nil {
		api.loreError(writer, request, "list Lore branches", err)
		return
	}
	selected, found := activeBranch(branches, branchName)
	if !found {
		problem(writer, http.StatusNotFound, "branch_not_found", "The Lore branch was not found")
		return
	}
	locks, err := api.lore.QueryFileLocks(request.Context(), ref, branchName, "", filePath, credential)
	if err != nil {
		api.loreError(writer, request, "list Lore file locks", err)
		return
	}
	truncated := len(locks) > maxLocks
	if truncated {
		locks = locks[:maxLocks]
	}
	owners, err := api.lockOwners(request.Context(), repository.LoreRepositoryID, locks)
	if err != nil {
		api.storeError(writer, request, "resolve Lore file lock owners", err)
		return
	}
	canWrite := actor != nil && access.AtLeast(collab.PermWrite)
	result := make([]fileLock, 0, len(locks))
	for _, lock := range locks {
		owner := owners[lock.OwnerID]
		result = append(result, fileLock{
			BranchID: lock.BranchID, Branch: selected.Name, Path: lock.Path,
			Owner: owner, LockedAt: lock.LockedAt,
			ViewerCanUnlock: canWrite && (actor.ID == lock.OwnerID || access.CanManageBranchRules()),
		})
	}
	writeJSON(writer, http.StatusOK, lockPage{
		Locks: result, Branches: activeBranches(branches), SelectedBranch: selected.Name,
		ViewerCanLock: canWrite, Truncated: truncated,
	})
}

func (api *API) acquire(writer http.ResponseWriter, request *http.Request) {
	actor, repository, _, ok := api.mutationContext(writer, request)
	if !ok {
		return
	}
	input, ok := decodeMutation(writer, request)
	if !ok {
		return
	}
	credential, ok := api.credential(writer, request, repository, &actor, loreclient.ScopeWrite)
	if !ok {
		return
	}
	lock, err := api.lore.AcquireFileLock(
		request.Context(), repositoryRef(repository), input.Branch, input.Path, credential,
	)
	if err != nil {
		api.mutationError(writer, request, "acquire Lore file lock", err)
		return
	}
	if err := api.observations.RecordLoreFileLockAcquisition(
		request.Context(), actor.ID, repository.LoreRepositoryID,
		lock.BranchID, lock.Path, lock.OwnerID, lock.LockedAt,
	); err != nil {
		api.storeError(writer, request, "record Lore file lock acquisition", err)
		return
	}
	writeJSON(writer, http.StatusCreated, fileLock{
		BranchID: lock.BranchID, Branch: input.Branch, Path: lock.Path,
		Owner:    lockOwner{ID: actor.ID, Username: actor.Username, DisplayName: actor.DisplayName},
		LockedAt: lock.LockedAt, ViewerCanUnlock: true,
	})
}

func (api *API) release(writer http.ResponseWriter, request *http.Request) {
	actor, repository, access, ok := api.mutationContext(writer, request)
	if !ok {
		return
	}
	input, ok := decodeMutation(writer, request)
	if !ok {
		return
	}
	scope := loreclient.ScopeWrite
	if access.CanManageBranchRules() {
		scope = loreclient.ScopeAdmin
	}
	credential, ok := api.credential(writer, request, repository, &actor, scope)
	if !ok {
		return
	}
	lock, err := api.lore.ReleaseFileLock(
		request.Context(), repositoryRef(repository), input.Branch, input.Path,
		credential, access.CanManageBranchRules(),
	)
	if err != nil {
		api.mutationError(writer, request, "release Lore file lock", err)
		return
	}
	if err := api.observations.RecordLoreFileLockRelease(
		request.Context(), actor.ID, repository.LoreRepositoryID,
		lock.BranchID, lock.Path, lock.OwnerID, lock.LockedAt,
	); err != nil {
		api.storeError(writer, request, "record Lore file lock release", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) visibleRepository(
	writer http.ResponseWriter,
	request *http.Request,
) (collab.Repository, *platform.User, collab.Access, bool) {
	actor, ok := api.actors.ResolveOptionalActor(writer, request)
	if !ok {
		return collab.Repository{}, nil, collab.Access{}, false
	}
	repository, err := api.store.LookupRepository(
		request.Context(), actor, request.PathValue("owner"), request.PathValue("repository"),
	)
	if err != nil {
		api.storeError(writer, request, "lookup repository", err)
		return collab.Repository{}, nil, collab.Access{}, false
	}
	if repository.LoreURL == "" || repository.LoreRepositoryID == "" {
		problem(writer, http.StatusBadGateway, "lore_unavailable", "The Lore repository is not configured")
		return collab.Repository{}, nil, collab.Access{}, false
	}
	if actor == nil {
		return repository, nil, collab.Access{Permission: collab.PermRead}, true
	}
	access, err := api.store.RepositoryPermission(request.Context(), *actor, repository)
	if err != nil {
		api.storeError(writer, request, "check repository permission", err)
		return collab.Repository{}, nil, collab.Access{}, false
	}
	return repository, actor, access, true
}

func (api *API) mutationContext(
	writer http.ResponseWriter,
	request *http.Request,
) (platform.User, collab.Repository, collab.Access, bool) {
	actor, ok := api.actors.ResolveActor(writer, request)
	if !ok {
		return platform.User{}, collab.Repository{}, collab.Access{}, false
	}
	repository, err := api.store.LookupRepository(
		request.Context(), &actor, request.PathValue("owner"), request.PathValue("repository"),
	)
	if err != nil {
		api.storeError(writer, request, "lookup repository", err)
		return platform.User{}, collab.Repository{}, collab.Access{}, false
	}
	access, err := api.store.RepositoryPermission(request.Context(), actor, repository)
	if err != nil {
		api.storeError(writer, request, "check repository permission", err)
		return platform.User{}, collab.Repository{}, collab.Access{}, false
	}
	if !access.AtLeast(collab.PermWrite) {
		problem(writer, http.StatusForbidden, "forbidden", "Repository write access is required")
		return platform.User{}, collab.Repository{}, collab.Access{}, false
	}
	return actor, repository, access, true
}

func (api *API) credential(
	writer http.ResponseWriter,
	request *http.Request,
	repository collab.Repository,
	actor *platform.User,
	scope loreclient.Scope,
) (loreclient.Credential, bool) {
	if api.credentials == nil {
		problem(writer, http.StatusServiceUnavailable, "lore_unavailable", "Lore credentials are unavailable")
		return loreclient.Credential{}, false
	}
	principal := loreclient.ServicePrincipal(loreclient.ServicePurposePublicReader, api.publicReaderSubject)
	if actor != nil {
		principal = loreclient.UserPrincipal(actor.ID)
	}
	ref := repositoryRef(repository)
	credential, err := api.credentials.ForRepository(request.Context(), loreclient.CredentialRequest{
		Principal: principal, Repository: ref, Partition: ref.CanonicalPartition(), Scope: scope,
	})
	if err != nil {
		api.loreError(writer, request, "issue Lore file lock credential", err)
		return loreclient.Credential{}, false
	}
	return credential, true
}

func (api *API) lockOwners(
	ctx context.Context,
	partition string,
	locks []loreclient.FileLock,
) (map[string]lockOwner, error) {
	owners := make(map[string]lockOwner)
	ownerIDs := make([]string, 0, len(locks))
	for _, lock := range locks {
		if _, exists := owners[lock.OwnerID]; !exists {
			owners[lock.OwnerID] = lockOwner{
				ID: lock.OwnerID, Username: lock.OwnerID, DisplayName: lock.OwnerID,
			}
			ownerIDs = append(ownerIDs, lock.OwnerID)
		}
	}
	if len(ownerIDs) == 0 || api.users == nil {
		return owners, nil
	}
	users, err := api.users.UserInfoForResource(ctx, "urc-"+partition, ownerIDs)
	if err != nil {
		return nil, err
	}
	for _, user := range users {
		owners[user.ID] = lockOwner{ID: user.ID, Username: user.Username, DisplayName: user.DisplayName}
	}
	return owners, nil
}

func (api *API) mutationError(
	writer http.ResponseWriter,
	request *http.Request,
	operation string,
	err error,
) {
	switch {
	case errors.Is(err, loreclient.ErrFileLockConflict):
		problem(writer, http.StatusConflict, "lock_conflict", "The file is locked by another user")
	case errors.Is(err, loreclient.ErrFileLockNotOwned):
		problem(writer, http.StatusConflict, "lock_not_owned", "The file lock belongs to another user")
	case errors.Is(err, loreclient.ErrFileLockNotFound):
		problem(writer, http.StatusNotFound, "lock_not_found", "The file lock was not found")
	case errors.Is(err, loreclient.ErrBranchNotFound):
		problem(writer, http.StatusNotFound, "branch_not_found", "The Lore branch was not found")
	default:
		api.loreError(writer, request, operation, err)
	}
}

func (api *API) loreError(writer http.ResponseWriter, request *http.Request, operation string, err error) {
	if errors.Is(err, request.Context().Err()) {
		return
	}
	if api.logger != nil {
		api.logger.Error(operation, "error", err, "method", request.Method, "path", request.URL.Path)
	}
	problem(writer, http.StatusBadGateway, "lore_unavailable", "Lore could not complete the file lock operation")
}

func (api *API) storeError(writer http.ResponseWriter, request *http.Request, operation string, err error) {
	switch {
	case errors.Is(err, platform.ErrNotFound):
		problem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
	case errors.Is(err, platform.ErrForbidden):
		problem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
	default:
		if api.logger != nil {
			api.logger.Error(operation, "error", err, "method", request.Method, "path", request.URL.Path)
		}
		problem(writer, http.StatusInternalServerError, "internal_error", "The request could not be completed")
	}
}

func repositoryRef(repository collab.Repository) loreclient.RepositoryRef {
	return loreclient.RepositoryRef{
		CacheKey: repository.ID, URL: repository.LoreURL,
		LoreRepositoryID: repository.LoreRepositoryID, DefaultBranch: repository.DefaultBranch,
	}
}

func activeBranch(branches []loreclient.Branch, name string) (loreclient.Branch, bool) {
	for _, branch := range branches {
		if branch.Name == name && !branch.Archived {
			return branch, true
		}
	}
	return loreclient.Branch{}, false
}

func activeBranches(branches []loreclient.Branch) []loreclient.Branch {
	result := make([]loreclient.Branch, 0, len(branches))
	for _, branch := range branches {
		if !branch.Archived {
			result = append(result, branch)
		}
	}
	return result
}

func decodeMutation(writer http.ResponseWriter, request *http.Request) (mutationRequest, bool) {
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBody)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input mutationRequest
	if err := decoder.Decode(&input); err != nil {
		problem(writer, http.StatusBadRequest, "invalid_json", "The request body is invalid")
		return mutationRequest{}, false
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		problem(writer, http.StatusBadRequest, "invalid_json", "The request body must contain one JSON value")
		return mutationRequest{}, false
	}
	input.Branch = strings.TrimSpace(input.Branch)
	input.Path = strings.TrimSpace(input.Path)
	if !loreclient.ValidBranchName(input.Branch) || !loreclient.ValidFileLockPath(input.Path) {
		problem(writer, http.StatusBadRequest, "invalid_lock", "The file lock request is invalid")
		return mutationRequest{}, false
	}
	return input, true
}

func problem(writer http.ResponseWriter, status int, code, detail string) {
	writer.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"type":  "https://lorehub.dev/problems/" + code,
		"title": http.StatusText(status), "status": status, "detail": detail,
	})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
