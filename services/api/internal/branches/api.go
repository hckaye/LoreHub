package branches

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const maxRequestBody = 1 << 20

type Store interface {
	LookupRepository(context.Context, *platform.User, string, string) (collab.Repository, error)
	RepositoryPermission(context.Context, platform.User, collab.Repository) (collab.Access, error)
	ListBranchRules(context.Context, string) ([]collab.BranchRule, error)
}

type ObservationStore interface {
	RecordLoreBranchCreation(context.Context, string, string, string, string, string) error
	RecordLoreBranchDeletion(context.Context, string, string, string, string, string) error
}

type ActorResolver interface {
	ResolveActor(http.ResponseWriter, *http.Request) (platform.User, bool)
}

type LoreClient interface {
	loreclient.Client
	loreclient.BranchMutationClient
}

type API struct {
	store        Store
	observations ObservationStore
	actors       ActorResolver
	lore         LoreClient
	credentials  loreclient.CredentialProvider
	logger       *slog.Logger
}

func Register(
	mux *http.ServeMux,
	store Store,
	observations ObservationStore,
	actors ActorResolver,
	lore LoreClient,
	credentials loreclient.CredentialProvider,
	logger *slog.Logger,
) {
	api := &API{
		store: store, observations: observations, actors: actors,
		lore: lore, credentials: credentials, logger: logger,
	}
	base := "/api/v1/repositories/{owner}/{repository}/branches"
	mux.HandleFunc("POST "+base, api.createBranch)
	mux.HandleFunc("DELETE "+base+"/{branch...}", api.archiveBranch)
}

type createBranchRequest struct {
	Name           string `json:"name"`
	Category       string `json:"category"`
	SourceBranch   string `json:"sourceBranch"`
	SourceRevision string `json:"sourceRevision"`
}

func (api *API) createBranch(writer http.ResponseWriter, request *http.Request) {
	actor, repository, access, ok := api.mutationContext(writer, request)
	if !ok {
		return
	}
	if !access.AtLeast(collab.PermWrite) {
		problem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
		return
	}
	var input createBranchRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Category = strings.TrimSpace(input.Category)
	input.SourceBranch = strings.TrimSpace(input.SourceBranch)
	input.SourceRevision = strings.TrimSpace(input.SourceRevision)
	if !loreclient.ValidBranchName(input.Name) || !loreclient.ValidBranchName(input.SourceBranch) ||
		(input.Category != "" && !loreclient.ValidBranchCategory(input.Category)) ||
		len(input.SourceRevision) != 64 || strings.Trim(input.SourceRevision, "0123456789abcdef") != "" {
		problem(writer, http.StatusBadRequest, "invalid_branch", "The Lore branch request is invalid")
		return
	}
	protected, err := api.branchBlocksDirectPush(request.Context(), repository.ID, input.Name)
	if err != nil {
		api.storeError(writer, request, "check branch protection", err)
		return
	}
	if protected {
		problem(writer, http.StatusConflict, "branch_protected", "The branch protection rule blocks creation")
		return
	}
	ref := repositoryRef(repository)
	credential, ok := api.writeCredential(writer, request, actor, ref)
	if !ok {
		return
	}
	branches, err := api.lore.Branches(request.Context(), ref, credential)
	if err != nil {
		api.loreError(writer, request, "list branches before creation", err)
		return
	}
	if existing, found := findBranch(branches, input.Name); found {
		if existing.LatestRevision == input.SourceRevision {
			if !api.observeBranchCreation(writer, request, actor, repository, existing) {
				return
			}
			writeJSON(writer, http.StatusOK, existing)
			return
		}
		problem(writer, http.StatusConflict, "branch_exists", "A Lore branch with this name already exists")
		return
	}
	source, found := findBranch(branches, input.SourceBranch)
	if !found || source.LatestRevision != input.SourceRevision {
		problem(writer, http.StatusConflict, "source_changed", "The source branch has changed")
		return
	}
	created, err := api.lore.CreateBranch(
		request.Context(), ref, input.Name, input.Category, input.SourceRevision, credential,
	)
	if err != nil {
		api.branchMutationError(writer, request, "create branch", err)
		return
	}
	if !api.observeBranchCreation(writer, request, actor, repository, created) {
		return
	}
	writeJSON(writer, http.StatusCreated, created)
}

func (api *API) archiveBranch(writer http.ResponseWriter, request *http.Request) {
	actor, repository, access, ok := api.mutationContext(writer, request)
	if !ok {
		return
	}
	if !access.AtLeast(collab.PermWrite) {
		problem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
		return
	}
	name := strings.Trim(strings.TrimSpace(request.PathValue("branch")), "/")
	if !loreclient.ValidBranchName(name) {
		problem(writer, http.StatusNotFound, "branch_not_found", "The Lore branch was not found")
		return
	}
	if name == repository.DefaultBranch {
		problem(writer, http.StatusConflict, "default_branch", "The default branch cannot be archived")
		return
	}
	protected, err := api.branchBlocksDirectPush(request.Context(), repository.ID, name)
	if err != nil {
		api.storeError(writer, request, "check branch protection", err)
		return
	}
	if protected {
		problem(writer, http.StatusConflict, "branch_protected", "The branch protection rule blocks archival")
		return
	}
	ref := repositoryRef(repository)
	credential, ok := api.writeCredential(writer, request, actor, ref)
	if !ok {
		return
	}
	branches, err := api.lore.Branches(request.Context(), ref, credential)
	if err != nil {
		api.loreError(writer, request, "list branches before archival", err)
		return
	}
	branch, found := findBranch(branches, name)
	if !found {
		problem(writer, http.StatusNotFound, "branch_not_found", "The Lore branch was not found")
		return
	}
	if err := api.lore.ArchiveBranch(request.Context(), ref, branch, credential); err != nil {
		api.branchMutationError(writer, request, "archive branch", err)
		return
	}
	if err := api.observations.RecordLoreBranchDeletion(
		request.Context(), actor.ID, repository.LoreRepositoryID,
		branch.ID, branch.Name, branch.LatestRevision,
	); err != nil {
		api.storeError(writer, request, "record branch archival", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) observeBranchCreation(
	writer http.ResponseWriter,
	request *http.Request,
	actor platform.User,
	repository collab.Repository,
	branch loreclient.Branch,
) bool {
	err := api.observations.RecordLoreBranchCreation(
		request.Context(), actor.ID, repository.LoreRepositoryID,
		branch.ID, branch.Name, branch.LatestRevision,
	)
	if err == nil {
		return true
	}
	api.storeError(writer, request, "record branch creation", err)
	return false
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
		api.storeError(writer, request, "check branch permission", err)
		return platform.User{}, collab.Repository{}, collab.Access{}, false
	}
	return actor, repository, access, true
}

func (api *API) writeCredential(
	writer http.ResponseWriter,
	request *http.Request,
	actor platform.User,
	repository loreclient.RepositoryRef,
) (loreclient.Credential, bool) {
	if api.credentials == nil {
		problem(writer, http.StatusServiceUnavailable, "lore_unavailable", "Lore credentials are unavailable")
		return loreclient.Credential{}, false
	}
	credential, err := api.credentials.ForRepository(request.Context(), loreclient.CredentialRequest{
		Principal: loreclient.UserPrincipal(actor.ID), Repository: repository,
		Partition: repository.CanonicalPartition(), Scope: loreclient.ScopeWrite,
	})
	if err != nil {
		api.loreError(writer, request, "issue branch credential", err)
		return loreclient.Credential{}, false
	}
	return credential, true
}

func (api *API) branchBlocksDirectPush(ctx context.Context, repositoryID, branch string) (bool, error) {
	rules, err := api.store.ListBranchRules(ctx, repositoryID)
	if err != nil {
		return false, err
	}
	return collab.BranchBlocksDirectPush(collab.MatchingBranchRules(rules, branch)), nil
}

func (api *API) branchMutationError(
	writer http.ResponseWriter,
	request *http.Request,
	operation string,
	err error,
) {
	switch {
	case errors.Is(err, loreclient.ErrBranchExists):
		problem(writer, http.StatusConflict, "branch_changed", "The Lore branch changed during this operation")
	case errors.Is(err, loreclient.ErrBranchNotFound):
		problem(writer, http.StatusNotFound, "branch_not_found", "The Lore branch was not found")
	default:
		api.loreError(writer, request, operation, err)
	}
}

func (api *API) loreError(writer http.ResponseWriter, request *http.Request, operation string, err error) {
	if api.logger != nil {
		api.logger.Error(operation, "error", err, "method", request.Method, "path", request.URL.Path)
	}
	problem(writer, http.StatusBadGateway, "lore_unavailable", "Lore could not complete the branch operation")
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
		CacheKey: repository.ID, URL: repository.LoreURL, LoreRepositoryID: repository.LoreRepositoryID,
	}
}

func findBranch(branches []loreclient.Branch, name string) (loreclient.Branch, bool) {
	for _, branch := range branches {
		if branch.Name == name && !branch.Archived {
			return branch, true
		}
	}
	return loreclient.Branch{}, false
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, destination any) bool {
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBody)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		problem(writer, http.StatusBadRequest, "invalid_json", "The request body is invalid")
		return false
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		problem(writer, http.StatusBadRequest, "invalid_json", "The request body must contain one JSON value")
		return false
	}
	return true
}

func problem(writer http.ResponseWriter, status int, code, detail string) {
	writer.Header().Set("Content-Type", "application/problem+json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"type": "https://lorehub.dev/problems/" + code, "title": http.StatusText(status),
		"status": status, "detail": detail,
	})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
