package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type repositoryLifecycleStore interface {
	SetRepositoryArchived(
		context.Context, platform.User, string, string, bool, string,
	) (platform.Repository, error)
}

type repositoryDeletionStore interface {
	ScheduleRepositoryDeletion(
		context.Context,
		platform.User,
		string,
		string,
		string,
		time.Duration,
	) (platform.DeletedRepository, error)
	ListDeletedRepositories(context.Context, platform.User, string) ([]platform.DeletedRepository, error)
	RestoreRepository(context.Context, platform.User, string, string) (platform.Repository, error)
}

func (api *API) scheduleRepositoryDeletion(writer http.ResponseWriter, request *http.Request) {
	store, ok := api.identityStore.(repositoryDeletionStore)
	if !ok || api.deletionRetention <= 0 {
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
	deleted, err := store.ScheduleRepositoryDeletion(
		request.Context(),
		actor,
		request.PathValue("owner"),
		request.PathValue("repository"),
		input.Confirmation,
		api.deletionRetention,
	)
	if err != nil {
		api.platformError(writer, request, "schedule repository deletion", err)
		return
	}
	writeJSON(writer, http.StatusAccepted, deleted)
}

func (api *API) listDeletedRepositories(writer http.ResponseWriter, request *http.Request) {
	store, ok := api.identityStore.(repositoryDeletionStore)
	if !ok {
		api.identityUnavailable(writer)
		return
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	deleted, err := store.ListDeletedRepositories(
		request.Context(),
		actor,
		request.PathValue("organization"),
	)
	if err != nil {
		api.platformError(writer, request, "list deleted repositories", err)
		return
	}
	writeJSON(writer, http.StatusOK, deleted)
}

func (api *API) restoreRepository(writer http.ResponseWriter, request *http.Request) {
	store, ok := api.identityStore.(repositoryDeletionStore)
	if !ok {
		api.identityUnavailable(writer)
		return
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	repository, err := store.RestoreRepository(
		request.Context(),
		actor,
		request.PathValue("organization"),
		request.PathValue("repository"),
	)
	if err != nil {
		api.platformError(writer, request, "restore repository", err)
		return
	}
	writeJSON(writer, http.StatusOK, repository)
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
