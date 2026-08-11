package reviewthreads

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type RepositoryStore interface {
	LookupRepository(context.Context, *platform.User, string, string) (collab.Repository, error)
	RepositoryPermission(context.Context, platform.User, collab.Repository) (collab.Access, error)
	GetMergeRequest(context.Context, string, int64) (collab.MergeRequest, error)
}

type API struct {
	store        Store
	repositories RepositoryStore
	actors       collab.ActorResolver
	code         loreclient.CodeClient
	credentials  loreclient.CredentialProvider
	logger       *slog.Logger
}

func NewAPI(
	store Store,
	repositories RepositoryStore,
	actors collab.ActorResolver,
	code loreclient.CodeClient,
	credentials loreclient.CredentialProvider,
	logger *slog.Logger,
) *API {
	return &API{
		store: store, repositories: repositories, actors: actors, code: code,
		credentials: credentials, logger: logger,
	}
}

func (api *API) visibleRepository(
	writer http.ResponseWriter,
	request *http.Request,
) (collab.Repository, *platform.User, bool) {
	actor, ok := api.actors.ResolveOptionalActor(writer, request)
	if !ok {
		return collab.Repository{}, nil, false
	}
	repository, err := api.repositories.LookupRepository(
		request.Context(), actor, request.PathValue("owner"), request.PathValue("repository"),
	)
	if err != nil {
		api.storeError(writer, request, "lookup review repository", err)
		return collab.Repository{}, nil, false
	}
	return repository, actor, true
}

func (api *API) mutationRepository(
	writer http.ResponseWriter,
	request *http.Request,
) (collab.Repository, platform.User, bool) {
	actor, ok := api.actors.ResolveActor(writer, request)
	if !ok {
		return collab.Repository{}, platform.User{}, false
	}
	repository, err := api.repositories.LookupRepository(
		request.Context(), &actor, request.PathValue("owner"), request.PathValue("repository"),
	)
	if err != nil {
		api.storeError(writer, request, "lookup review repository", err)
		return collab.Repository{}, platform.User{}, false
	}
	return repository, actor, true
}

func (api *API) credential(
	request *http.Request,
	repository collab.Repository,
	actor platform.User,
) (loreclient.Credential, error) {
	if api.credentials == nil {
		return loreclient.Credential{}, loreclient.ErrCredentialUnavailable
	}
	ref := loreReference(repository)
	return api.credentials.ForRepository(request.Context(), loreclient.CredentialRequest{
		Principal: loreclient.UserPrincipal(actor.ID), Repository: ref,
		Partition: ref.CanonicalPartition(), Scope: loreclient.ScopeRead,
	})
}

func loreReference(repository collab.Repository) loreclient.RepositoryRef {
	return loreclient.RepositoryRef{
		CacheKey: repository.ID, URL: repository.LoreURL,
		LoreRepositoryID: repository.LoreRepositoryID, DefaultBranch: repository.DefaultBranch,
	}
}

func repositoryRef(repository collab.Repository) RepositoryRef {
	return RepositoryRef{ID: repository.ID, OrganizationID: repository.OrganizationID}
}
