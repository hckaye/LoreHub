package merge

import (
	"errors"
	"net/http"
	"time"

	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
)

func (api *API) push(writer http.ResponseWriter, request *http.Request) {
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
	operation, err := api.workflow.GetMergeOperation(request.Context(), repository.ID, number)
	if err != nil {
		api.workflowError(writer, request, err)
		return
	}
	if operation.State == "aborted" {
		api.problem(writer, http.StatusConflict, "merge_aborted", "The Lore merge operation was aborted")
		return
	}
	recoveringPush := operation.State == "pushing"
	if operation.State == "pushed" {
		readCredential, credentialErr := api.credential(request, repository, &actor, loreclient.ScopeRead)
		if credentialErr != nil {
			api.loreError(writer, request, credentialErr)
			return
		}
		if err := api.verifyPushedRemote(request.Context(), repository, operation, mergeRequest.TargetBranch,
			readCredential); err != nil {
			api.loreError(writer, request, err)
			return
		}
		merged, finalizeErr := api.workflow.FinalizeMerged(request.Context(), actor, repository.ID, number,
			operation.ID, operation.PushedRevision)
		if finalizeErr != nil {
			api.workflowError(writer, request, finalizeErr)
			return
		}
		if api.merge != nil {
			if cleanupErr := api.merge.CleanupMergeWorkspace(
				request.Context(), api.repositoryRef(repository), operation.ID,
			); cleanupErr != nil {
				api.logger.Warn("clean reconciled Lore merge workspace", "error", cleanupErr,
					"operation_id", operation.ID)
			}
		}
		writeJSON(writer, http.StatusOK, merged)
		return
	}
	if operation.State == "merged" {
		writeJSON(writer, http.StatusOK, mergeRequest)
		return
	}
	if !recoveringPush {
		readiness, readinessErr := api.buildReadiness(request.Context(), repository, number, &actor)
		if readinessErr != nil {
			api.workflowError(writer, request, readinessErr)
			return
		}
		if !readiness.Ready {
			api.writeReadinessBlocked(writer, readiness)
			return
		}
		if len(operation.ConflictPaths) > 0 || operation.State == "conflicts" {
			api.problem(writer, http.StatusConflict, "conflicts", "Resolve every Lore merge conflict before pushing")
			return
		}
		if operation.State != "ready_to_push" {
			api.problem(writer, http.StatusConflict, "merge_not_ready", "Start the Lore merge before pushing it")
			return
		}
	}
	if api.merge == nil {
		api.problem(writer, http.StatusServiceUnavailable, "lore_unavailable", "Lore merge operations are unavailable")
		return
	}
	leaseOwner := newLeaseOwner()
	if recoveringPush && operation.LeaseOwner != "" {
		leaseOwner = operation.LeaseOwner
	}
	operation, err = api.workflow.AcquireMergeOperation(request.Context(), actor.ID, repository.ID, number,
		operation.SourceRevision, operation.TargetRevision, leaseOwner, mergeLease)
	if err != nil {
		api.workflowError(writer, request, err)
		return
	}
	leaseOwner = operation.LeaseOwner
	if operation.State == "pushed" {
		readCredential, credentialErr := api.credential(request, repository, &actor, loreclient.ScopeRead)
		if credentialErr != nil {
			api.loreError(writer, request, credentialErr)
			return
		}
		if err := api.verifyPushedRemote(request.Context(), repository, operation, mergeRequest.TargetBranch,
			readCredential); err != nil {
			api.loreError(writer, request, err)
			return
		}
		merged, finalizeErr := api.workflow.FinalizeMerged(request.Context(), actor, repository.ID, number,
			operation.ID, operation.PushedRevision)
		if finalizeErr != nil {
			api.workflowError(writer, request, finalizeErr)
			return
		}
		if api.merge != nil {
			if cleanupErr := api.merge.CleanupMergeWorkspace(
				request.Context(), api.repositoryRef(repository), operation.ID,
			); cleanupErr != nil {
				api.logger.Warn("clean reconciled Lore merge workspace", "error", cleanupErr,
					"operation_id", operation.ID)
			}
		}
		writeJSON(writer, http.StatusOK, merged)
		return
	}
	if operation.State == "merged" {
		writeJSON(writer, http.StatusOK, mergeRequest)
		return
	}
	if !recoveringPush && operation.State != "ready_to_push" {
		api.problem(writer, http.StatusConflict, "merge_not_ready", "Start the Lore merge before pushing it")
		return
	}
	readCredential, credentialErr := api.credential(request, repository, &actor, loreclient.ScopeRead)
	if credentialErr != nil {
		api.loreError(writer, request, credentialErr)
		return
	}
	writeCredential, credentialErr := api.credential(request, repository, &actor, loreclient.ScopeWrite)
	if credentialErr != nil {
		api.loreError(writer, request, credentialErr)
		return
	}
	recoveryRevision := operation.StagedRevision
	if operation.PushedRevision != "" {
		recoveryRevision = operation.PushedRevision
	}
	if recoveringPush {
		recoveryRevision, err = api.recoveryRevision(
			request.Context(), repository, operation, mergeRequest, readCredential,
		)
		if err != nil {
			api.loreError(writer, request, err)
			return
		}
	}
	operation.LeaseExpiresAt = timePtr(time.Now().UTC().Add(mergeLease))
	operation.State = "pushing"
	operation, err = api.workflow.UpdateMergeOperationOwned(request.Context(), operation, leaseOwner)
	if err != nil {
		api.workflowError(writer, request, err)
		return
	}
	result, err := api.merge.PushMerge(request.Context(), api.repositoryRef(repository), operation.ID,
		api.workspace(mergeRequest, operation), recoveryRevision, readCredential, writeCredential,
		api.fixedPushAuthorizer(actor, repository, operation.ID))
	if err != nil {
		if errors.Is(err, loreclient.ErrMergeStale) || errors.Is(err, loreclient.ErrMergeParentCheck) {
			operation.State = "ready_to_push"
			operation.ErrorCode = "stale_revision"
			operation.ErrorDetail = err.Error()
			operation.LeaseOwner = ""
			operation.LeaseExpiresAt = nil
			if _, updateErr := api.workflow.UpdateMergeOperationOwned(
				request.Context(), operation, leaseOwner,
			); updateErr != nil {
				api.logger.Warn("record stale Lore merge operation", "error", updateErr,
					"operation_id", operation.ID)
			}
			api.problem(writer, http.StatusConflict, "stale_revision", "Lore branch revisions changed; restart the merge")
			return
		}
		if errors.Is(err, loreclient.ErrPushAuthorizationDenied) {
			operation.ErrorCode = "policy_blocked"
			operation.ErrorDetail = "protected branch authorization was denied"
			operation.LeaseOwner = ""
			operation.LeaseExpiresAt = nil
			_, _ = api.workflow.UpdateMergeOperationOwned(request.Context(), operation, leaseOwner)
			api.problem(writer, http.StatusConflict, "policy_blocked",
				"The merge no longer satisfies protected branch policy")
			return
		}
		if result.LocalRevision != "" {
			operation.StagedRevision = result.LocalRevision
		}
		operation.ErrorCode = "lore_unavailable"
		operation.ErrorDetail = err.Error()
		operation.LeaseExpiresAt = timePtr(time.Now().UTC().Add(mergeLease))
		_, _ = api.workflow.UpdateMergeOperationOwned(request.Context(), operation, leaseOwner)
		api.problem(writer, http.StatusBadGateway, "lore_unavailable",
			"Lore push did not report a durable result; retry to reconcile the operation")
		return
	}
	pushedRevision := result.RemoteRevision
	if pushedRevision == "" {
		pushedRevision = result.LocalRevision
	}
	if pushedRevision == "" {
		operation.ErrorCode = "lore_unavailable"
		operation.ErrorDetail = "Lore push completed without a revision"
		_, _ = api.workflow.UpdateMergeOperationOwned(request.Context(), operation, leaseOwner)
		api.problem(writer, http.StatusServiceUnavailable, "merge_recovery_required",
			"Lore push completed without a revision; retry to reconcile the operation")
		return
	}
	operation.PushedRevision = pushedRevision
	if result.LocalRevision != "" {
		operation.StagedRevision = result.LocalRevision
	}
	if len(result.Parents) > 0 {
		operation.ParentRevisions = result.Parents
	}
	operation.State = "pushed"
	operation.ErrorCode = ""
	operation.ErrorDetail = ""
	operation.LeaseExpiresAt = timePtr(time.Now().UTC().Add(mergeLease))
	operation, err = api.workflow.UpdateMergeOperationOwned(request.Context(), operation, leaseOwner)
	if err != nil {
		api.problem(writer, http.StatusServiceUnavailable, "merge_recovery_required",
			"Lore push succeeded; retry to finalize the merge request")
		return
	}
	if err := api.verifyPushedRemote(request.Context(), repository, operation, mergeRequest.TargetBranch,
		readCredential); err != nil {
		api.loreError(writer, request, err)
		return
	}
	merged, err := api.workflow.FinalizeMerged(
		request.Context(), actor, repository.ID, number, operation.ID, pushedRevision,
	)
	if err != nil {
		api.workflowError(writer, request, err)
		return
	}
	if err := api.merge.CleanupMergeWorkspace(
		request.Context(), api.repositoryRef(repository), operation.ID,
	); err != nil {
		api.logger.Warn("clean merged Lore workspace", "error", err, "operation_id", operation.ID)
	}
	writeJSON(writer, http.StatusOK, merged)
}
