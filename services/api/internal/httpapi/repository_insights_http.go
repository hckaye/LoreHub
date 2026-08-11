package httpapi

import (
	"net/http"
	"strconv"
)

func (api *API) repositoryInsights(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "private, no-store")
	if api.identityStore == nil {
		api.identityUnavailable(writer)
		return
	}
	actor, ok := api.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	values := request.URL.Query()
	for key := range values {
		if key != "days" {
			writeProblem(writer, http.StatusBadRequest, "invalid_input", "The insights parameters are invalid")
			return
		}
	}
	days := 30
	if selected := values["days"]; len(selected) > 1 {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "The insights period must be unique")
		return
	} else if len(selected) == 1 {
		raw := selected[0]
		parsed, err := strconv.Atoi(raw)
		if err != nil || (parsed != 7 && parsed != 30 && parsed != 90) {
			writeProblem(writer, http.StatusBadRequest, "invalid_period", "The insights period is invalid")
			return
		}
		days = parsed
	}
	result, err := api.identityStore.RepositoryInsights(
		request.Context(), actor, request.PathValue("owner"), request.PathValue("repository"), days,
	)
	if err != nil {
		api.platformError(writer, request, "get repository insights", err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}
