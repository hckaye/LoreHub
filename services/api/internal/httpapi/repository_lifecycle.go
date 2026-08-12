package httpapi

import (
	"context"
	"net/http"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type repositoryLifecycleStore interface {
	SetRepositoryArchived(
		context.Context, platform.User, string, string, bool, string,
	) (platform.Repository, error)
}

func (api *API) archiveRepository(writer http.ResponseWriter, request *http.Request) {
	api.setRepositoryArchived(writer, request, true)
}

func (api *API) unarchiveRepository(writer http.ResponseWriter, request *http.Request) {
	api.setRepositoryArchived(writer, request, false)
}

func (api *API) setRepositoryArchived(writer http.ResponseWriter, request *http.Request, archived bool) {
	store, ok := api.identityStore.(repositoryLifecycleStore)
	if !ok {
		api.identityUnavailable(writer)
		return
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input struct {
		Confirmation string `json:"confirmation"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	repository, err := store.SetRepositoryArchived(
		request.Context(), actor, request.PathValue("owner"), request.PathValue("repository"),
		archived, input.Confirmation,
	)
	if err != nil {
		api.platformError(writer, request, "update repository archive state", err)
		return
	}
	writeJSON(writer, http.StatusOK, repository)
}
