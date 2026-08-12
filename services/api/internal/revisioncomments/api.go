package revisioncomments

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
	store               collab.RevisionCommentStore
	repositories        RepositoryStore
	actors              collab.ActorResolver
	code                loreclient.CodeClient
	credentials         loreclient.CredentialProvider
	publicReaderSubject string
	logger              *slog.Logger
}

func NewAPI(
	store collab.RevisionCommentStore,
	repositories RepositoryStore,
	actors collab.ActorResolver,
	code loreclient.CodeClient,
	credentials loreclient.CredentialProvider,
	publicReaderSubject string,
	logger *slog.Logger,
) *API {
	return &API{
		store: store, repositories: repositories, actors: actors,
		code: code, credentials: credentials,
		publicReaderSubject: publicReaderSubject, logger: logger,
	}
}

func (api *API) lookup(
	writer http.ResponseWriter,
	request *http.Request,
	actor *platform.User,
) (collab.Repository, bool) {
	repository, err := api.repositories.LookupRepository(
		request.Context(), actor,
		request.PathValue("owner"), request.PathValue("repository"),
	)
	if err != nil {
		api.storeError(writer, request, "lookup revision comment repository", err)
		return collab.Repository{}, false
	}
	return repository, true
}

func loreRepositoryRef(repository collab.Repository) loreclient.RepositoryRef {
	return loreclient.RepositoryRef{
		CacheKey: repository.ID, URL: repository.LoreURL,
		LoreRepositoryID: repository.LoreRepositoryID,
		DefaultBranch:    repository.DefaultBranch,
	}
}
