package merge

import (
	"context"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func (api *API) fixedPushAuthorizer(
	actor platform.User,
	repository collab.Repository,
	operationID string,
) loreclient.PushAuthorizer {
	if api.pushAuth == nil {
		return nil
	}
	return loreclient.PushAuthorizerFunc(func(ctx context.Context, input loreclient.PushAuthorization) error {
		input.ActorUserID = actor.ID
		input.RepositoryID = repository.ID
		input.OperationID = operationID
		return api.pushAuth.AuthorizeLoreMergePush(ctx, input)
	})
}
