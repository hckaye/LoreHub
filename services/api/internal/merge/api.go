// Package merge implements the durable Lore merge-request lifecycle.
package merge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const mergeLease = 5 * time.Minute

var (
	errInvalidMergePaths = errors.New("invalid merge paths")
	errInvalidStrategy   = errors.New("invalid merge strategy")
)

type API struct {
	store       collab.Store
	workflow    collab.MergeWorkflowStore
	lore        loreclient.Client
	merge       loreclient.MergeClient
	actors      collab.ActorResolver
	credentials loreclient.CredentialProvider
	pushAuth    loreclient.PushAuthorizer
	logger      *slog.Logger
}

// Register mounts readiness, continuation, conflict resolution and final push
// endpoints for merge requests.
func Register(
	mux *http.ServeMux,
	store collab.Store,
	workflow collab.MergeWorkflowStore,
	lore loreclient.Client,
	mergeClient loreclient.MergeClient,
	actors collab.ActorResolver,
	credentials loreclient.CredentialProvider,
	pushAuthorizer loreclient.PushAuthorizer,
	logger *slog.Logger,
) {
	api := &API{store: store, workflow: workflow, lore: lore, merge: mergeClient,
		actors: actors, credentials: credentials, pushAuth: pushAuthorizer, logger: logger}
	base := "/api/v1/repositories/{owner}/{repository}/merge-requests/{number}"
	mux.HandleFunc("GET "+base+"/merge-readiness", api.readiness)
	mux.HandleFunc("GET "+base+"/merge-operation", api.operation)
	mux.HandleFunc("POST "+base+"/merge/start", api.start)
	mux.HandleFunc("POST "+base+"/merge/continue", api.start)
	mux.HandleFunc("GET "+base+"/merge/conflicts", api.conflicts)
	mux.HandleFunc("POST "+base+"/merge/conflicts", api.resolve)
	mux.HandleFunc("POST "+base+"/merge/abort", api.abort)
	mux.HandleFunc("POST "+base+"/merge/restart", api.restart)
	mux.HandleFunc("POST "+base+"/merge", api.push)
	mux.HandleFunc("POST "+base+"/merge/push", api.push)
}

func (api *API) visible(writer http.ResponseWriter, request *http.Request) (collab.Repository, *platform.User, bool) {
	actor, ok := api.actors.ResolveActor(writer, request)
	if !ok {
		return collab.Repository{}, nil, false
	}
	repository, err := api.store.LookupRepository(request.Context(), &actor,
		request.PathValue("owner"), request.PathValue("repository"))
	if err != nil {
		api.storeError(writer, request, err)
		return collab.Repository{}, nil, false
	}
	return repository, &actor, true
}

func (api *API) mutation(writer http.ResponseWriter, request *http.Request) (platform.User, collab.Repository, bool) {
	actor, ok := api.actors.ResolveActor(writer, request)
	if !ok {
		return platform.User{}, collab.Repository{}, false
	}
	repository, err := api.store.LookupRepository(request.Context(), &actor,
		request.PathValue("owner"), request.PathValue("repository"))
	if err != nil {
		api.storeError(writer, request, err)
		return platform.User{}, collab.Repository{}, false
	}
	access, err := api.store.RepositoryPermission(request.Context(), actor, repository)
	if err != nil {
		api.storeError(writer, request, err)
		return platform.User{}, collab.Repository{}, false
	}
	if !access.AtLeast(collab.PermWrite) {
		api.problem(writer, http.StatusForbidden, "forbidden", "Write permission is required to merge")
		return platform.User{}, collab.Repository{}, false
	}
	return actor, repository, true
}

func (api *API) readiness(writer http.ResponseWriter, request *http.Request) {
	repository, actor, ok := api.visible(writer, request)
	if !ok {
		return
	}
	number, ok := parseNumber(request.PathValue("number"))
	if !ok {
		api.problem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return
	}
	result, err := api.buildReadiness(request.Context(), repository, number, actor)
	if err != nil {
		api.workflowError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (api *API) operation(writer http.ResponseWriter, request *http.Request) {
	repository, _, ok := api.visible(writer, request)
	if !ok {
		return
	}
	number, ok := parseNumber(request.PathValue("number"))
	if !ok {
		api.problem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return
	}
	operation, err := api.workflow.GetMergeOperation(request.Context(), repository.ID, number)
	if err != nil {
		api.workflowError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, operation)
}

func (api *API) start(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.mutation(writer, request)
	if !ok {
		return
	}
	number, ok := parseNumber(request.PathValue("number"))
	if !ok {
		api.problem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return
	}
	mergeRequest, err := api.store.GetMergeRequest(request.Context(), repository.ID, number)
	if err != nil {
		api.storeError(writer, request, err)
		return
	}
	if mergeRequest.State == "merged" {
		api.problem(writer, http.StatusConflict, "already_merged", "The merge request is already merged")
		return
	}
	existing, operationErr := api.workflow.GetMergeOperation(request.Context(), repository.ID, number)
	if operationErr == nil {
		if existing.State == "pushing" || existing.State == "pushed" || existing.State == "merged" {
			writeJSON(writer, http.StatusOK, existing)
			return
		}
	} else if !errors.Is(operationErr, platform.ErrNotFound) {
		api.workflowError(writer, request, operationErr)
		return
	}
	readiness, err := api.buildReadiness(request.Context(), repository, number, &actor)
	if err != nil {
		api.workflowError(writer, request, err)
		return
	}
	if !readiness.Ready {
		api.writeReadinessBlocked(writer, readiness)
		return
	}
	if operationErr == nil && existing.SourceRevision == mergeRequest.SourceRevision &&
		existing.TargetRevision == mergeRequest.TargetRevision &&
		(existing.State == "conflicts" || existing.State == "ready_to_push" ||
			(existing.State == "started" && existing.ErrorCode == "" && mergeLeaseActive(existing))) {
		writeJSON(writer, http.StatusOK, existing)
		return
	}
	if api.merge == nil {
		api.problem(writer, http.StatusServiceUnavailable, "lore_unavailable", "Lore merge operations are unavailable")
		return
	}
	operation, err := api.workflow.AcquireMergeOperation(request.Context(), actor.ID, repository.ID, number,
		mergeRequest.SourceRevision, mergeRequest.TargetRevision, newLeaseOwner(), mergeLease)
	if err != nil {
		api.workflowError(writer, request, err)
		return
	}
	leaseOwner := operation.LeaseOwner
	if operation.State == "merged" || operation.State == "pushed" || operation.State == "conflicts" ||
		operation.State == "ready_to_push" || operation.State == "pushing" {
		writeJSON(writer, http.StatusOK, operation)
		return
	}
	operation.State = "started"
	if operation.StartedAt == nil {
		now := time.Now().UTC()
		operation.StartedAt = &now
	}
	operation.LeaseExpiresAt = timePtr(time.Now().UTC().Add(mergeLease))
	operation, err = api.workflow.UpdateMergeOperationOwned(request.Context(), operation, leaseOwner)
	if err != nil {
		api.workflowError(writer, request, err)
		return
	}
	writeCredential, credentialErr := api.credential(request, repository, &actor, loreclient.ScopeWrite)
	if credentialErr != nil {
		api.releaseAfterLoreError(request.Context(), operation, credentialErr)
		api.loreError(writer, request, credentialErr)
		return
	}
	result, err := api.merge.StartMerge(request.Context(), api.repositoryRef(repository), operation.ID,
		mergeRequest.SourceBranch, mergeRequest.TargetBranch, mergeRequest.SourceRevision,
		mergeRequest.TargetRevision, mergeRequest.Title, writeCredential)
	if err != nil {
		stale := errors.Is(err, loreclient.ErrMergeStale) || errors.Is(err, loreclient.ErrMergeParentCheck)
		operation.ErrorCode = "lore_unavailable"
		if stale {
			operation.ErrorCode = "stale_revision"
			operation.State = "created"
			operation.StartedAt = nil
		}
		operation.ErrorDetail = err.Error()
		operation.LeaseOwner = ""
		operation.LeaseExpiresAt = nil
		if updated, updateErr := api.workflow.UpdateMergeOperationOwned(
			request.Context(), operation, leaseOwner,
		); updateErr == nil {
			operation = updated
		} else {
			api.logger.Warn("record failed Lore merge start", "error", updateErr, "operation_id", operation.ID)
		}
		if stale {
			api.problem(writer, http.StatusConflict, "stale_revision", "Lore branch revisions changed; restart the merge")
		} else {
			api.problem(writer, http.StatusBadGateway, "lore_unavailable", "Lore did not complete the merge start")
		}
		return
	}
	operation.SourceRevision = result.SourceRevision
	operation.TargetRevision = result.TargetRevision
	operation.StagedRevision = result.StagedRevision
	operation.ParentRevisions = result.Parents
	operation.ConflictPaths = result.Conflicts
	operation.ErrorCode = ""
	operation.ErrorDetail = ""
	if len(result.Conflicts) > 0 {
		operation.State = "conflicts"
	} else {
		operation.State = "ready_to_push"
	}
	operation.LeaseOwner = ""
	operation.LeaseExpiresAt = nil
	operation, err = api.workflow.UpdateMergeOperationOwned(request.Context(), operation, leaseOwner)
	if err != nil {
		api.problem(writer, http.StatusServiceUnavailable, "merge_recovery_required",
			"Lore completed the merge preparation but its durable state needs recovery")
		return
	}
	writeJSON(writer, http.StatusOK, operation)
}

func (api *API) conflicts(writer http.ResponseWriter, request *http.Request) {
	repository, actor, ok := api.visible(writer, request)
	if !ok {
		return
	}
	number, ok := parseNumber(request.PathValue("number"))
	if !ok {
		api.problem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return
	}
	operation, err := api.workflow.GetMergeOperation(request.Context(), repository.ID, number)
	if err != nil {
		api.workflowError(writer, request, err)
		return
	}
	mergeRequest, err := api.store.GetMergeRequest(request.Context(), repository.ID, number)
	if err != nil {
		api.storeError(writer, request, err)
		return
	}
	if operation.State == "aborted" {
		api.problem(writer, http.StatusConflict, "merge_aborted", "The Lore merge operation was aborted")
		return
	}
	if operation.State == "merged" || operation.State == "pushed" {
		api.problem(writer, http.StatusConflict, "already_merged", "The Lore push has already completed")
		return
	}
	if api.merge == nil {
		api.problem(writer, http.StatusServiceUnavailable, "lore_unavailable", "Lore merge operations are unavailable")
		return
	}
	readCredential, credentialErr := api.credential(request, repository, actor, loreclient.ScopeRead)
	if credentialErr != nil {
		api.loreError(writer, request, credentialErr)
		return
	}
	paths, err := api.merge.ListConflicts(
		request.Context(), api.repositoryRef(repository), operation.ID, api.workspace(mergeRequest, operation), nil,
		readCredential,
	)
	if err != nil {
		api.loreError(writer, request, err)
		return
	}
	operation.ConflictPaths = paths
	if len(paths) > 0 {
		operation.State = "conflicts"
	} else if operation.State == "conflicts" || operation.State == "started" {
		operation.State = "ready_to_push"
	}
	writeJSON(writer, http.StatusOK, operation)
}

type resolveRequest struct {
	Paths    []string `json:"paths"`
	Strategy string   `json:"strategy"`
}

func (api *API) resolve(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.mutation(writer, request)
	if !ok {
		return
	}
	number, ok := parseNumber(request.PathValue("number"))
	if !ok {
		api.problem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return
	}
	var input resolveRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	if err := validateResolveRequest(input); err != nil {
		api.problem(writer, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	operation, err := api.workflow.GetMergeOperation(request.Context(), repository.ID, number)
	if err != nil {
		api.workflowError(writer, request, err)
		return
	}
	mergeRequest, err := api.store.GetMergeRequest(request.Context(), repository.ID, number)
	if err != nil {
		api.storeError(writer, request, err)
		return
	}
	if operation.State == "aborted" {
		api.problem(writer, http.StatusConflict, "merge_aborted", "The Lore merge operation was aborted")
		return
	}
	if operation.State == "merged" || operation.State == "pushed" {
		api.problem(writer, http.StatusConflict, "already_merged", "The Lore push has already completed")
		return
	}
	if api.merge == nil {
		api.problem(writer, http.StatusServiceUnavailable, "lore_unavailable", "Lore merge operations are unavailable")
		return
	}
	writeCredential, credentialErr := api.credential(request, repository, &actor, loreclient.ScopeWrite)
	if credentialErr != nil {
		api.loreError(writer, request, credentialErr)
		return
	}
	operation, err = api.workflow.AcquireMergeOperation(request.Context(), actor.ID, repository.ID, number,
		operation.SourceRevision, operation.TargetRevision, newLeaseOwner(), mergeLease)
	if err != nil {
		api.workflowError(writer, request, err)
		return
	}
	leaseOwner := operation.LeaseOwner
	if operation.State != "conflicts" && operation.State != "started" {
		api.problem(writer, http.StatusConflict, "merge_not_in_conflict", "The merge operation has no unresolved conflicts")
		return
	}
	operation, err = api.workflow.RecordMergeResolutions(request.Context(), actor, operation,
		input.Paths, input.Strategy)
	if err != nil {
		api.workflowError(writer, request, err)
		return
	}
	resolution, err := api.merge.ResolveMerge(request.Context(), api.repositoryRef(repository), operation.ID,
		api.workspace(mergeRequest, operation), input.Paths, input.Strategy, writeCredential)
	if err != nil {
		api.releaseAfterLoreError(request.Context(), operation, err)
		api.loreError(writer, request, err)
		return
	}
	paths, err := api.merge.ListConflicts(
		request.Context(), api.repositoryRef(repository), operation.ID, api.workspace(mergeRequest, operation), nil,
		writeCredential,
	)
	if err != nil {
		api.releaseAfterLoreError(request.Context(), operation, err)
		api.loreError(writer, request, err)
		return
	}
	operation.ConflictPaths = paths
	operation.StagedRevision = resolution.StagedRevision
	operation.ParentRevisions = resolution.Parents
	operation.State = "ready_to_push"
	if len(paths) > 0 {
		operation.State = "conflicts"
	}
	operation.LeaseOwner = ""
	operation.LeaseExpiresAt = nil
	operation, err = api.workflow.UpdateMergeOperationOwned(request.Context(), operation, leaseOwner)
	if err != nil {
		api.workflowError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, operation)
}

func (api *API) abort(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.mutation(writer, request)
	if !ok {
		return
	}
	number, ok := parseNumber(request.PathValue("number"))
	if !ok {
		api.problem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return
	}
	operation, err := api.workflow.GetMergeOperation(request.Context(), repository.ID, number)
	if err != nil {
		api.workflowError(writer, request, err)
		return
	}
	if operation.State == "aborted" {
		writeJSON(writer, http.StatusOK, operation)
		return
	}
	if operation.State == "merged" || operation.State == "pushed" {
		api.problem(writer, http.StatusConflict, "already_merged", "The Lore push has already completed")
		return
	}
	if operation.State == "pushing" {
		api.problem(writer, http.StatusConflict, "merge_busy", "The Lore push is still in progress")
		return
	}
	if api.merge == nil {
		api.problem(writer, http.StatusServiceUnavailable, "lore_unavailable", "Lore merge operations are unavailable")
		return
	}
	writeCredential, credentialErr := api.credential(request, repository, &actor, loreclient.ScopeWrite)
	if credentialErr != nil {
		api.loreError(writer, request, credentialErr)
		return
	}
	operation, err = api.workflow.AcquireMergeOperation(request.Context(), actor.ID, repository.ID, number,
		operation.SourceRevision, operation.TargetRevision, newLeaseOwner(), mergeLease)
	if err != nil {
		api.workflowError(writer, request, err)
		return
	}
	leaseOwner := operation.LeaseOwner
	if err := api.merge.AbortMerge(
		request.Context(), api.repositoryRef(repository), operation.ID, writeCredential,
	); err != nil {
		api.releaseAfterLoreError(request.Context(), operation, err)
		api.loreError(writer, request, err)
		return
	}
	operation.State = "aborted"
	operation.LeaseOwner = ""
	operation.LeaseExpiresAt = nil
	operation.ErrorCode = ""
	operation.ErrorDetail = ""
	operation.ConflictPaths = []string{}
	operation.CompletedAt = timePtr(time.Now().UTC())
	operation, err = api.workflow.UpdateMergeOperationOwned(request.Context(), operation, leaseOwner)
	if err != nil {
		api.problem(writer, http.StatusServiceUnavailable, "merge_recovery_required",
			"Lore aborted the merge but its durable state needs recovery")
		return
	}
	if err := api.merge.CleanupMergeWorkspace(request.Context(), api.repositoryRef(repository), operation.ID); err != nil {
		api.logger.Warn("clean aborted Lore merge workspace", "error", err, "operation_id", operation.ID)
	}
	_ = actor
	writeJSON(writer, http.StatusOK, operation)
}

type restartRequest struct {
	Paths []string `json:"paths"`
}

func (api *API) restart(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.mutation(writer, request)
	if !ok {
		return
	}
	number, ok := parseNumber(request.PathValue("number"))
	if !ok {
		api.problem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return
	}
	var input restartRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	if err := validatePaths(input.Paths, true); err != nil {
		api.problem(writer, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	operation, err := api.workflow.GetMergeOperation(request.Context(), repository.ID, number)
	if err != nil {
		api.workflowError(writer, request, err)
		return
	}
	if operation.State == "aborted" {
		api.problem(writer, http.StatusConflict, "merge_aborted", "The Lore merge operation was aborted")
		return
	}
	if operation.State == "merged" || operation.State == "pushed" {
		api.problem(writer, http.StatusConflict, "already_merged", "The Lore push has already completed")
		return
	}
	if operation.State == "pushing" {
		api.problem(writer, http.StatusConflict, "merge_busy", "The Lore push is still in progress")
		return
	}
	mergeRequest, err := api.store.GetMergeRequest(request.Context(), repository.ID, number)
	if err != nil {
		api.storeError(writer, request, err)
		return
	}
	readiness, err := api.buildReadiness(request.Context(), repository, number, &actor)
	if err != nil {
		api.workflowError(writer, request, err)
		return
	}
	if !readiness.SourceStale && !readiness.TargetStale {
		api.problem(writer, http.StatusConflict, "merge_not_stale", "The source and target revisions are already current")
		return
	}
	if hasRestartBlocker(readiness.Blockers) {
		api.writeReadinessBlocked(writer, readiness)
		return
	}
	if api.merge == nil {
		api.problem(writer, http.StatusServiceUnavailable, "lore_unavailable", "Lore merge operations are unavailable")
		return
	}
	operation, err = api.workflow.RestartMergeOperation(request.Context(), actor, repository.ID, number,
		readiness.CurrentSourceRevision, readiness.CurrentTargetRevision, newLeaseOwner(), mergeLease)
	if err != nil {
		api.workflowError(writer, request, err)
		return
	}
	leaseOwner := operation.LeaseOwner
	mergeRequest, err = api.workflow.RefreshMergeRequestRevisions(request.Context(), actor, repository.ID, number,
		readiness.CurrentSourceRevision, readiness.CurrentTargetRevision)
	if err != nil {
		api.releaseAfterLoreError(request.Context(), operation, err)
		api.workflowError(writer, request, err)
		return
	}
	writeCredential, credentialErr := api.credential(request, repository, &actor, loreclient.ScopeWrite)
	if credentialErr != nil {
		api.releaseAfterLoreError(request.Context(), operation, credentialErr)
		api.loreError(writer, request, credentialErr)
		return
	}
	result, err := api.merge.RestartMerge(request.Context(), api.repositoryRef(repository), operation.ID,
		api.workspace(mergeRequest, operation), input.Paths, writeCredential)
	if err != nil {
		api.releaseAfterLoreError(request.Context(), operation, err)
		api.loreError(writer, request, err)
		return
	}
	operation.SourceRevision = result.SourceRevision
	operation.TargetRevision = result.TargetRevision
	operation.StagedRevision = result.StagedRevision
	operation.ParentRevisions = result.Parents
	operation.ConflictPaths = result.Conflicts
	operation.State = "ready_to_push"
	if len(result.Conflicts) > 0 {
		operation.State = "conflicts"
	}
	operation.LeaseOwner = ""
	operation.LeaseExpiresAt = nil
	operation, err = api.workflow.UpdateMergeOperationOwned(request.Context(), operation, leaseOwner)
	if err != nil {
		api.workflowError(writer, request, err)
		return
	}
	writeJSON(writer, http.StatusOK, operation)
}

func hasRestartBlocker(blockers []collab.MergeBlocker) bool {
	for _, blocker := range blockers {
		switch blocker.Code {
		case "write_permission_required", "state_not_open":
			return true
		}
	}
	return false
}

func parseNumber(value string) (int64, bool) {
	if value == "" {
		return 0, false
	}
	var result int64
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, false
		}
		result = result*10 + int64(character-'0')
		if result > 1<<62 {
			return 0, false
		}
	}
	return result, result > 0
}

func validateResolveRequest(input resolveRequest) error {
	if input.Strategy != "mine" && input.Strategy != "theirs" {
		return errInvalidStrategy
	}
	return validatePaths(input.Paths, false)
}

func validatePaths(paths []string, allowEmpty bool) error {
	if len(paths) > 2_000 || (!allowEmpty && len(paths) == 0) {
		return errInvalidMergePaths
	}
	for _, value := range paths {
		if value == "" || len(value) > 2_048 || strings.HasPrefix(value, "/") || strings.ContainsRune(value, '\x00') ||
			strings.ContainsRune(value, '\\') {
			return errInvalidMergePaths
		}
		for _, part := range strings.Split(value, "/") {
			if part == "" || part == "." || part == ".." {
				return errInvalidMergePaths
			}
		}
	}
	return nil
}

func (api *API) writeReadinessBlocked(writer http.ResponseWriter, readiness collab.MergeReadiness) {
	status := http.StatusConflict
	code := "policy_blocked"
	for _, blocker := range readiness.Blockers {
		if strings.Contains(blocker.Code, "stale") {
			code = "stale_revision"
			break
		}
	}
	api.writeProblemValue(writer, status, code, "The merge request is not ready to merge", readiness)
}

func (api *API) workflowError(writer http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, platform.ErrNotFound):
		api.problem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
	case errors.Is(err, platform.ErrForbidden):
		api.problem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
	case errors.Is(err, collab.ErrMergeBusy):
		api.problem(writer, http.StatusConflict, "merge_busy", "Another merge operation is currently running")
	case errors.Is(err, collab.ErrMergeOperationConflict):
		api.problem(writer, http.StatusConflict, "merge_operation_changed", "The merge operation changed; reload and retry")
	case errors.Is(err, platform.ErrConflict):
		api.problem(writer, http.StatusConflict, "merge_operation_changed", "The merge operation changed; reload and retry")
	default:
		api.logger.Error("merge workflow", "error", err, "method", request.Method, "path", request.URL.Path)
		api.problem(writer, http.StatusInternalServerError, "internal_error", "The request could not be completed")
	}
}

func (api *API) storeError(writer http.ResponseWriter, request *http.Request, err error) {
	api.workflowError(writer, request, err)
}

func (api *API) releaseAfterLoreError(ctx context.Context, operation collab.MergeOperation, err error) {
	leaseOwner := operation.LeaseOwner
	operation.ErrorCode = "lore_unavailable"
	operation.ErrorDetail = err.Error()
	operation.LeaseOwner = ""
	operation.LeaseExpiresAt = nil
	var updateErr error
	if leaseOwner != "" {
		_, updateErr = api.workflow.UpdateMergeOperationOwned(ctx, operation, leaseOwner)
	} else {
		_, updateErr = api.workflow.UpdateMergeOperation(ctx, operation)
	}
	if updateErr != nil {
		api.logger.Warn("record failed Lore merge operation", "error", updateErr, "operation_id", operation.ID)
	}
}

func (api *API) loreError(writer http.ResponseWriter, request *http.Request, err error) {
	if errors.Is(err, request.Context().Err()) {
		return
	}
	if errors.Is(err, loreclient.ErrMergeStale) || errors.Is(err, loreclient.ErrMergeParentCheck) {
		api.problem(writer, http.StatusConflict, "stale_revision", "Lore branch revisions changed; restart the merge")
		return
	}
	if errors.Is(err, loreclient.ErrPushAuthorizationRequired) {
		api.problem(writer, http.StatusServiceUnavailable, "merge_authorization_unavailable",
			"Protected merge authorization is not configured")
		return
	}
	if errors.Is(err, loreclient.ErrPushAuthorizationDenied) {
		api.problem(writer, http.StatusConflict, "policy_blocked",
			"The merge no longer satisfies protected branch policy")
		return
	}
	api.logger.Error("Lore merge operation", "error", err, "method", request.Method, "path", request.URL.Path)
	api.problem(writer, http.StatusBadGateway, "lore_unavailable", "Lore did not complete the request")
}

func (api *API) problem(writer http.ResponseWriter, status int, code string, detail string) {
	api.writeProblemValue(writer, status, code, detail, nil)
}

func (api *API) writeProblemValue(writer http.ResponseWriter, status int, code string, detail string, value any) {
	writer.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	writer.WriteHeader(status)
	body := map[string]any{"error": map[string]string{"code": code, "detail": detail}}
	if value != nil {
		body["data"] = value
	}
	_ = json.NewEncoder(writer).Encode(body)
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, target any) bool {
	mediaType, _, err := mimeParse(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeInputProblem(writer, http.StatusUnsupportedMediaType, "unsupported_media_type",
			"Content-Type must be application/json")
		return false
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 1<<20)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeInputProblem(writer, http.StatusBadRequest, "invalid_json", "Request body is invalid")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeInputProblem(writer, http.StatusBadRequest, "invalid_json",
			"Request body must contain one JSON value")
		return false
	}
	return true
}

func writeInputProblem(writer http.ResponseWriter, status int, code string, detail string) {
	writer.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"error": map[string]string{"code": code, "detail": detail},
	})
}

func mimeParse(value string) (string, string, error) {
	parts := strings.Split(value, ";")
	mediaType := strings.TrimSpace(strings.ToLower(parts[0]))
	if mediaType == "" {
		return "", "", errors.New("missing content type")
	}
	return mediaType, "", nil
}

func timePtr(value time.Time) *time.Time { return &value }

func newLeaseOwner() string {
	return uuid.NewString()
}
