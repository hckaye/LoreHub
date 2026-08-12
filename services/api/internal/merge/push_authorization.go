package merge

import (
	"context"
	"errors"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func (api *API) fixedPushAuthorizer(
	actor platform.User,
	repository collab.Repository,
	operationID string,
) loreclient.PushAuthorizer {
	if api.pushAuth == nil || api.mergeAuthorization == nil {
		return nil
	}
	return loreclient.PushAuthorizerFunc(func(ctx context.Context, input loreclient.PushAuthorization) error {
		input.ActorUserID = actor.ID
		input.RepositoryID = repository.ID
		input.OperationID = operationID
		if err := api.pushAuth.AuthorizeLoreMergePush(ctx, input); err != nil {
			return err
		}
		if err := api.mergeAuthorization.PrepareMergeAuthorization(ctx, actor.ID,
			platform.MergeAuthorizationInput{
				OperationID:    input.OperationID,
				RepositoryID:   repository.LoreRepositoryID,
				BranchID:       input.TargetBranchID,
				BranchName:     input.TargetBranchName,
				ExpectedBase:   input.ExpectedTargetRevision,
				ExpectedHead:   input.ProposedRevision,
				SourceRevision: input.SourceRevision,
				Lifetime:       mergeAuthorizationLifetime,
			}); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return loreclient.ErrPushAuthorizationDenied
		}
		return nil
	})
}
