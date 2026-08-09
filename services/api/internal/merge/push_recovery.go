package merge

import (
	"context"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
)

// recoveryRevision compares the remote heads before a retry of a lost pushing
// response. An exact staged or pushed revision is already complete; the exact
// old target requests a fresh local reconstruction. Other heads remain the
// original revision hint so PushMerge returns its stale-revision error.
func (api *API) recoveryRevision(
	ctx context.Context,
	repository collab.Repository,
	operation collab.MergeOperation,
	mergeRequest collab.MergeRequest,
	credential loreclient.Credential,
) (string, error) {
	branches, err := api.lore.Branches(ctx, api.repositoryRef(repository), credential)
	if err != nil {
		return "", err
	}
	return selectRecoveryRevision(operation, mergeRequest.SourceBranch, mergeRequest.TargetBranch, branches), nil
}

func selectRecoveryRevision(
	operation collab.MergeOperation,
	sourceBranch string,
	targetBranch string,
	branches []loreclient.Branch,
) string {
	var sourceRevision, targetRevision string
	for _, branch := range branches {
		switch branch.Name {
		case sourceBranch:
			sourceRevision = branch.LatestRevision
		case targetBranch:
			targetRevision = branch.LatestRevision
		}
	}
	if operation.StagedRevision != "" && targetRevision == operation.StagedRevision {
		return operation.StagedRevision
	}
	if operation.PushedRevision != "" && targetRevision == operation.PushedRevision {
		return operation.PushedRevision
	}
	if sourceRevision == operation.SourceRevision && targetRevision == operation.TargetRevision {
		return ""
	}
	if operation.StagedRevision != "" {
		return operation.StagedRevision
	}
	return operation.PushedRevision
}
