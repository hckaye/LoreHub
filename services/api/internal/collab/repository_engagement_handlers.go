package collab

import (
	"net/http"
)

func (api *API) engagementStore(
	writer http.ResponseWriter,
) (RepositoryEngagementStore, bool) {
	store, ok := api.store.(RepositoryEngagementStore)
	if !ok {
		writeProblem(writer, http.StatusServiceUnavailable, "service_unavailable",
			"Repository engagement is unavailable")
	}
	return store, ok
}

func (api *API) putRepositoryStar(writer http.ResponseWriter, request *http.Request) {
	api.setRepositoryEngagement(writer, request, "star", true)
}

func (api *API) deleteRepositoryStar(writer http.ResponseWriter, request *http.Request) {
	api.setRepositoryEngagement(writer, request, "star", false)
}

func (api *API) putRepositoryWatch(writer http.ResponseWriter, request *http.Request) {
	api.setRepositoryEngagement(writer, request, "watch", true)
}

func (api *API) deleteRepositoryWatch(writer http.ResponseWriter, request *http.Request) {
	api.setRepositoryEngagement(writer, request, "watch", false)
}

func (api *API) setRepositoryEngagement(
	writer http.ResponseWriter,
	request *http.Request,
	kind string,
	enabled bool,
) {
	store, ok := api.engagementStore(writer)
	if !ok {
		return
	}
	actor, repository, ok := api.requireMutationActor(writer, request)
	if !ok {
		return
	}
	var snapshot RepositoryEngagement
	var err error
	if kind == "star" {
		snapshot, err = store.SetRepositoryStar(
			requestContext(request), actor, repository.ID, enabled,
		)
	} else {
		snapshot, err = store.SetRepositoryWatch(
			requestContext(request), actor, repository.ID, enabled,
		)
	}
	if err != nil {
		storeError(writer, request, "update repository engagement", err, api.logger)
		return
	}
	writeJSON(writer, http.StatusOK, snapshot)
}
