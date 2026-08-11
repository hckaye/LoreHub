package milestones

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type createRequest struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	DueOn       *string `json:"dueOn"`
}

type updateRequest struct {
	Title           *string         `json:"title"`
	Description     *string         `json:"description"`
	State           *string         `json:"state"`
	DueOn           json.RawMessage `json:"dueOn"`
	ExpectedVersion int64           `json:"expectedVersion"`
}

type versionRequest struct {
	ExpectedVersion int64 `json:"expectedVersion"`
}

func (api *API) listMilestones(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actors.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	repository, ok := api.lookup(writer, request, actor)
	if !ok {
		return
	}
	state, ok := parseState(writer, request.URL.Query().Get("state"))
	if !ok {
		return
	}
	page, perPage, ok := parsePagination(writer, request)
	if !ok {
		return
	}
	viewerCanWrite := false
	if actor != nil {
		access, permissionOK := api.permission(writer, request, *actor, repository)
		if !permissionOK {
			return
		}
		viewerCanWrite = access.AtLeast(collab.PermWrite)
	}
	result, err := api.store.List(request.Context(), repository.ID, state, page, perPage)
	if err != nil {
		api.storeError(writer, request, "list milestones", err)
		return
	}
	result.ViewerCanWrite = viewerCanWrite
	for index := range result.Milestones {
		result.Milestones[index].ViewerCanWrite = viewerCanWrite
	}
	writeJSON(writer, http.StatusOK, result)
}

func (api *API) getMilestone(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actors.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	repository, ok := api.lookup(writer, request, actor)
	if !ok {
		return
	}
	number, ok := parseNumber(writer, request.PathValue("number"), "milestone")
	if !ok {
		return
	}
	milestone, err := api.store.Get(request.Context(), repository.ID, number)
	if err != nil {
		api.storeError(writer, request, "get milestone", err)
		return
	}
	if actor != nil {
		access, permissionOK := api.permission(writer, request, *actor, repository)
		if !permissionOK {
			return
		}
		milestone.ViewerCanWrite = access.AtLeast(collab.PermWrite)
	}
	writeJSON(writer, http.StatusOK, milestone)
}

func (api *API) createMilestone(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.requirePermission(writer, request, collab.PermWrite)
	if !ok {
		return
	}
	var body createRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	milestone, err := api.store.Create(request.Context(), actor, repositoryRef(repository), CreateInput{
		Title: body.Title, Description: body.Description, DueOn: body.DueOn,
	})
	if err != nil {
		api.storeError(writer, request, "create milestone", err)
		return
	}
	writer.Header().Set("Location", request.URL.Path+"/"+strconv.FormatInt(milestone.Number, 10))
	writeJSON(writer, http.StatusCreated, milestone)
}

func (api *API) updateMilestone(writer http.ResponseWriter, request *http.Request) {
	actor, repository, number, ok := api.mutationContext(writer, request, collab.PermWrite)
	if !ok {
		return
	}
	var body updateRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	dueOn, dueOnSet, ok := parseDueOn(writer, body.DueOn)
	if !ok {
		return
	}
	milestone, err := api.store.Update(request.Context(), actor, repositoryRef(repository), number, UpdateInput{
		Title: body.Title, Description: body.Description, State: body.State,
		DueOn: dueOn, DueOnSet: dueOnSet, ExpectedVersion: body.ExpectedVersion,
	})
	if err != nil {
		api.storeError(writer, request, "update milestone", err)
		return
	}
	writeJSON(writer, http.StatusOK, milestone)
}

func (api *API) deleteMilestone(writer http.ResponseWriter, request *http.Request) {
	actor, repository, number, ok := api.mutationContext(writer, request, collab.PermWrite)
	if !ok {
		return
	}
	var body versionRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	if err := api.store.Delete(
		request.Context(), actor, repositoryRef(repository), number, body.ExpectedVersion,
	); err != nil {
		api.storeError(writer, request, "delete milestone", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) assignIssue(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.requirePermission(writer, request, collab.PermTriage)
	if !ok {
		return
	}
	issueNumber, ok := parseNumber(writer, request.PathValue("issueNumber"), "issue")
	if !ok {
		return
	}
	milestoneNumber, ok := parseNumber(writer, request.PathValue("number"), "milestone")
	if !ok {
		return
	}
	milestone, err := api.store.AssignIssue(
		request.Context(), actor, repositoryRef(repository), issueNumber, milestoneNumber,
	)
	if err != nil {
		api.storeError(writer, request, "assign issue milestone", err)
		return
	}
	writeJSON(writer, http.StatusOK, milestone)
}

func (api *API) removeIssue(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.requirePermission(writer, request, collab.PermTriage)
	if !ok {
		return
	}
	issueNumber, ok := parseNumber(writer, request.PathValue("issueNumber"), "issue")
	if !ok {
		return
	}
	if err := api.store.RemoveIssue(
		request.Context(), actor, repositoryRef(repository), issueNumber,
	); err != nil {
		api.storeError(writer, request, "remove issue milestone", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) mutationContext(
	writer http.ResponseWriter,
	request *http.Request,
	minimum collab.Permission,
) (platform.User, collab.Repository, int64, bool) {
	actor, repository, ok := api.requirePermission(writer, request, minimum)
	if !ok {
		return platform.User{}, collab.Repository{}, 0, false
	}
	number, ok := parseNumber(writer, request.PathValue("number"), "milestone")
	return actor, repository, number, ok
}

func parseDueOn(writer http.ResponseWriter, value json.RawMessage) (*string, bool, bool) {
	if value == nil {
		return nil, false, true
	}
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return nil, true, true
	}
	var dueOn string
	if err := json.Unmarshal(value, &dueOn); err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "dueOn must be a date or null")
		return nil, false, false
	}
	return &dueOn, true, true
}

func parseState(writer http.ResponseWriter, value string) (string, bool) {
	if value == "" {
		return "open", true
	}
	if value != "open" && value != "closed" && value != "all" {
		writeProblem(writer, http.StatusBadRequest, "invalid_state", "state must be open, closed, or all")
		return "", false
	}
	return value, true
}

func parsePagination(writer http.ResponseWriter, request *http.Request) (int, int, bool) {
	page, err := positiveQuery(request.URL.Query().Get("page"), 1, 1_000_000)
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_page", "The page is invalid")
		return 0, 0, false
	}
	perPage, err := positiveQuery(request.URL.Query().Get("perPage"), 20, 100)
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_per_page", "The page size is invalid")
		return 0, 0, false
	}
	return page, perPage, true
}

func positiveQuery(value string, fallback, maximum int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < 1 || number > maximum {
		return 0, ErrInvalidInput
	}
	return number, nil
}

func parseNumber(writer http.ResponseWriter, value string, resource string) (int64, bool) {
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil || number < 1 {
		writeProblem(writer, http.StatusNotFound, "not_found", "The "+resource+" was not found")
		return 0, false
	}
	return number, true
}

func (api *API) storeError(writer http.ResponseWriter, request *http.Request, operation string, err error) {
	switch {
	case errors.Is(err, platform.ErrNotFound):
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
	case errors.Is(err, platform.ErrForbidden):
		writeProblem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
	case errors.Is(err, platform.ErrConflict):
		writeProblem(writer, http.StatusConflict, "conflict", "A milestone with this title already exists")
	case errors.Is(err, ErrVersionConflict):
		writeProblem(writer, http.StatusConflict, "version_conflict", "The milestone changed; reload and try again")
	case errors.Is(err, ErrInvalidInput):
		writeProblem(writer, http.StatusBadRequest, "invalid_input", err.Error())
	default:
		if api.logger != nil {
			api.logger.Error(operation, "error", err, "method", request.Method, "path", request.URL.Path)
		}
		writeProblem(writer, http.StatusInternalServerError, "internal_error",
			"The request could not be completed")
	}
}
