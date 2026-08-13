package statuses

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
}

type API struct {
	store        Store
	repositories RepositoryStore
	actors       collab.ActorResolver
	lore         loreclient.CodeClient
	credentials  loreclient.CredentialProvider
	logger       *slog.Logger
}

func NewAPI(
	store Store,
	repositories RepositoryStore,
	actors collab.ActorResolver,
	lore loreclient.CodeClient,
	credentials loreclient.CredentialProvider,
	logger *slog.Logger,
) *API {
	return &API{
		store: store, repositories: repositories, actors: actors,
		lore: lore, credentials: credentials, logger: logger,
	}
}

func (api *API) lookup(
	writer http.ResponseWriter,
	request *http.Request,
	actor *platform.User,
) (collab.Repository, bool) {
	repository, err := api.repositories.LookupRepository(
		request.Context(), actor, request.PathValue("owner"), request.PathValue("repository"),
	)
	if err != nil {
		api.storeError(writer, request, "lookup revision status repository", err)
		return collab.Repository{}, false
	}
	return repository, true
}

func (api *API) requireWrite(
	writer http.ResponseWriter,
	request *http.Request,
) (platform.User, collab.Repository, bool) {
	actor, ok := api.actors.ResolveActor(writer, request)
	if !ok {
		return platform.User{}, collab.Repository{}, false
	}
	repository, ok := api.lookup(writer, request, &actor)
	if !ok {
		return platform.User{}, collab.Repository{}, false
	}
	if repository.ArchivedAt != nil || repository.MigratingAt != nil {
		writeProblem(writer, http.StatusForbidden, "repository_read_only", "The repository is read-only")
		return platform.User{}, collab.Repository{}, false
	}
	return actor, repository, true
}

func repositoryRef(repository collab.Repository) RepositoryRef {
	return RepositoryRef{ID: repository.ID, OrganizationID: repository.OrganizationID}
}

func loreRepositoryRef(repository collab.Repository) loreclient.RepositoryRef {
	return loreclient.RepositoryRef{
		CacheKey: repository.ID, URL: repository.LoreURL,
		LoreRepositoryID: repository.LoreRepositoryID,
		DefaultBranch:    repository.DefaultBranch,
	}
}
