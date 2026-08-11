package milestones

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type RepositoryStore interface {
	LookupRepository(context.Context, *platform.User, string, string) (collab.Repository, error)
	RepositoryPermission(context.Context, platform.User, collab.Repository) (collab.Access, error)
}

type API struct {
	store        Store
	repositories RepositoryStore
	actors       collab.ActorResolver
	logger       *slog.Logger
}

func NewAPI(
	store Store,
	repositories RepositoryStore,
	actors collab.ActorResolver,
	logger *slog.Logger,
) *API {
	return &API{store: store, repositories: repositories, actors: actors, logger: logger}
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
		api.storeError(writer, request, "lookup milestone repository", err)
		return collab.Repository{}, false
	}
	return repository, true
}

func (api *API) permission(
	writer http.ResponseWriter,
	request *http.Request,
	actor platform.User,
	repository collab.Repository,
) (collab.Access, bool) {
	access, err := api.repositories.RepositoryPermission(request.Context(), actor, repository)
	if err != nil {
		api.storeError(writer, request, "compute milestone permission", err)
		return collab.Access{}, false
	}
	return access, true
}

func (api *API) requirePermission(
	writer http.ResponseWriter,
	request *http.Request,
	minimum collab.Permission,
) (platform.User, collab.Repository, bool) {
	actor, ok := api.actors.ResolveActor(writer, request)
	if !ok {
		return platform.User{}, collab.Repository{}, false
	}
	repository, ok := api.lookup(writer, request, &actor)
	if !ok {
		return platform.User{}, collab.Repository{}, false
	}
	access, ok := api.permission(writer, request, actor, repository)
	if !ok {
		return platform.User{}, collab.Repository{}, false
	}
	if !access.AtLeast(minimum) {
		writeProblem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
		return platform.User{}, collab.Repository{}, false
	}
	return actor, repository, true
}

func repositoryRef(repository collab.Repository) RepositoryRef {
	return RepositoryRef{ID: repository.ID, OrganizationID: repository.OrganizationID}
}
