package merge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func mergeLeaseActive(operation collab.MergeOperation) bool {
	return operation.LeaseOwner != "" && operation.LeaseExpiresAt != nil &&
		operation.LeaseExpiresAt.After(time.Now().UTC())
}

func (api *API) buildReadiness(
	ctx context.Context,
	repository collab.Repository,
	number int64,
	actor *platform.User,
) (collab.MergeReadiness, error) {
	mergeRequest, err := api.store.GetMergeRequest(ctx, repository.ID, number)
	if err != nil {
		return collab.MergeReadiness{}, err
	}
	rules, err := api.store.ListBranchRules(ctx, repository.ID)
	if err != nil {
		return collab.MergeReadiness{}, err
	}
	reviews, err := api.store.ListReviews(ctx, repository.ID, number)
	if err != nil {
		return collab.MergeReadiness{}, err
	}
	readCredential, err := api.credentialFromContext(ctx, repository, actor, loreclient.ScopeRead)
	if err != nil {
		return collab.MergeReadiness{}, err
	}
	branches, err := api.lore.Branches(ctx, api.repositoryRef(repository), readCredential)
	if err != nil {
		return collab.MergeReadiness{}, err
	}
	sourceCurrent, sourceFound := branchRevision(branches, mergeRequest.SourceBranch)
	targetCurrent, targetFound := branchRevision(branches, mergeRequest.TargetBranch)
	matched := matchingRules(rules, mergeRequest.TargetBranch)
	statusStore, ok := api.store.(collab.RevisionStatusStore)
	if !ok {
		return collab.MergeReadiness{}, errors.New("revision status store is not configured")
	}
	statusChecks, err := statusStore.ListRevisionStatusChecks(
		ctx,
		repository.ID,
		mergeRequest.SourceRevision,
	)
	if err != nil {
		return collab.MergeReadiness{}, err
	}
	statusChecks, unsuccessfulChecks := evaluateRequiredStatusChecks(
		statusChecks,
		requiredStatusChecks(matched),
	)
	ciSuccess := true
	if requiresCI(matched) {
		ciSuccess = false
		if sourceFound {
			ciSuccess, err = api.workflow.ListSuccessfulCI(ctx, repository.ID, mergeRequest.SourceBranch,
				mergeRequest.SourceRevision)
			if err != nil {
				return collab.MergeReadiness{}, err
			}
		}
	}
	readiness := collab.MergeReadiness{
		MergeRequest:          mergeRequest,
		CurrentSourceRevision: sourceCurrent,
		CurrentTargetRevision: targetCurrent,
		SourceStale:           !sourceFound || sourceCurrent != mergeRequest.SourceRevision,
		TargetStale:           !targetFound || targetCurrent != mergeRequest.TargetRevision,
		Reviews:               reviews,
		CISuccess:             ciSuccess,
		StatusChecks:          statusChecks,
		DirectPushBlocked:     directPushBlocked(matched),
		Rules:                 matched,
		Blockers:              []collab.MergeBlocker{},
	}
	if actor != nil {
		access, err := api.store.RepositoryPermission(ctx, *actor, repository)
		if err != nil {
			return collab.MergeReadiness{}, err
		}
		readiness.CanMerge = access.AtLeast(collab.PermWrite)
	}
	if !readiness.CanMerge {
		readiness.Blockers = append(readiness.Blockers, collab.MergeBlocker{
			Code: "write_permission_required", Detail: "Write permission is required to merge",
		})
	}
	if mergeRequest.State != "open" {
		readiness.Blockers = append(readiness.Blockers, collab.MergeBlocker{Code: "state_not_open",
			Detail: "Only an open merge request can be merged"})
	}
	if mergeRequest.IsDraft {
		readiness.Blockers = append(readiness.Blockers, collab.MergeBlocker{Code: "draft",
			Detail: "Mark the pull request as ready for review before merging"})
	}
	if readiness.SourceStale {
		readiness.Blockers = append(readiness.Blockers, collab.MergeBlocker{Code: "stale_source_revision",
			Detail: "The source branch revision changed since this merge request was opened"})
	}
	if readiness.TargetStale {
		readiness.Blockers = append(readiness.Blockers, collab.MergeBlocker{Code: "stale_target_revision",
			Detail: "The target branch revision changed since this merge request was opened"})
	}
	if reviews.ChangeRequests > 0 {
		readiness.Blockers = append(readiness.Blockers, collab.MergeBlocker{Code: "changes_requested",
			Detail: "A current review requests changes"})
	}
	if approvals := requiredApprovals(matched); reviews.Approvals < int64(approvals) {
		readiness.Blockers = append(readiness.Blockers, collab.MergeBlocker{Code: "required_approvals",
			Detail: "The required current-revision approvals have not been reached"})
	}
	if requiresCI(matched) && !ciSuccess {
		readiness.Blockers = append(readiness.Blockers, collab.MergeBlocker{Code: "ci_required",
			Detail: "A successful CI run for the exact source revision is required"})
	}
	if blocker, blocked := requiredStatusChecksBlocker(unsuccessfulChecks); blocked {
		readiness.Blockers = append(readiness.Blockers, blocker)
	}
	if operation, operationErr := api.workflow.GetMergeOperation(ctx, repository.ID, number); operationErr == nil {
		readiness.Operation = &operation
		failedStart := operation.State == "created" && operation.ErrorCode != ""
		terminal := operation.State == "aborted" || operation.State == "pushed" || operation.State == "merged"
		if !failedStart && !terminal && (operation.SourceRevision != mergeRequest.SourceRevision ||
			operation.TargetRevision != mergeRequest.TargetRevision) {
			readiness.Blockers = append(readiness.Blockers, collab.MergeBlocker{
				Code: "stale_operation_revision", Detail: "Restart the merge for the current source and target revisions",
			})
		}
	} else if !errors.Is(operationErr, platform.ErrNotFound) {
		return collab.MergeReadiness{}, operationErr
	}
	readiness.Ready = len(readiness.Blockers) == 0
	return readiness, nil
}

func branchRevision(branches []loreclient.Branch, name string) (string, bool) {
	for _, branch := range branches {
		if branch.Name == name && !branch.Archived && branch.LatestRevision != "" {
			return branch.LatestRevision, true
		}
	}
	return "", false
}

func (api *API) repositoryRef(repository collab.Repository) loreclient.RepositoryRef {
	return loreclient.RepositoryRef{CacheKey: repository.ID, URL: repository.LoreURL,
		LoreRepositoryID: repository.LoreRepositoryID, DefaultBranch: repository.DefaultBranch}
}

func (api *API) credentialFromContext(
	ctx context.Context,
	repository collab.Repository,
	actor *platform.User,
	scope loreclient.Scope,
) (loreclient.Credential, error) {
	if api.credentials == nil {
		return loreclient.Credential{}, loreclient.ErrCredentialUnavailable
	}
	if actor == nil {
		return loreclient.Credential{}, loreclient.ErrInvalidPrincipal
	}
	ref := api.repositoryRef(repository)
	return api.credentials.ForRepository(ctx, loreclient.CredentialRequest{
		Principal:  loreclient.UserPrincipal(actor.ID),
		Repository: ref,
		Partition:  ref.CanonicalPartition(),
		Scope:      scope,
	})
}

func (api *API) credential(
	request *http.Request,
	repository collab.Repository,
	actor *platform.User,
	scope loreclient.Scope,
) (loreclient.Credential, error) {
	return api.credentialFromContext(request.Context(), repository, actor, scope)
}

func (api *API) verifyPushedRemote(
	ctx context.Context,
	repository collab.Repository,
	operation collab.MergeOperation,
	targetBranch string,
	credential loreclient.Credential,
) error {
	if operation.PushedRevision == "" {
		return fmt.Errorf("%w: pushed revision is missing", loreclient.ErrMergeStale)
	}
	branches, err := api.lore.Branches(ctx, api.repositoryRef(repository), credential)
	if err != nil {
		return err
	}
	for _, branch := range branches {
		if branch.Name == targetBranch && branch.LatestRevision == operation.PushedRevision {
			return nil
		}
	}
	return fmt.Errorf("%w: remote target is not %s", loreclient.ErrMergeStale, operation.PushedRevision)
}

func (api *API) workspace(
	mergeRequest collab.MergeRequest,
	operation collab.MergeOperation,
) loreclient.MergeWorkspace {
	resolutions := make([]loreclient.MergeResolution, 0, len(operation.Resolutions))
	for _, resolution := range operation.Resolutions {
		resolutions = append(resolutions, loreclient.MergeResolution{
			Path: resolution.Path, Strategy: resolution.Strategy,
		})
	}
	return loreclient.MergeWorkspace{
		SourceBranch:   mergeRequest.SourceBranch,
		TargetBranch:   mergeRequest.TargetBranch,
		SourceRevision: operation.SourceRevision,
		TargetRevision: operation.TargetRevision,
		Message:        mergeRequest.Title,
		Resolutions:    resolutions,
	}
}
