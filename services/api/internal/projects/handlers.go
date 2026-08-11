package projects

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/collab"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type projectRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	State       string `json:"state"`
}

type projectPatchRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	State       *string `json:"state"`
}

type columnRequest struct {
	Name string `json:"name"`
}

type itemRequest struct {
	ColumnID           string `json:"columnId"`
	Kind               string `json:"kind"`
	IssueNumber        *int64 `json:"issueNumber"`
	MergeRequestNumber *int64 `json:"mergeRequestNumber"`
	Title              string `json:"title"`
	Body               string `json:"body"`
}

type itemPatchRequest struct {
	ColumnID *string `json:"columnId"`
	Title    *string `json:"title"`
	Body     *string `json:"body"`
}

func (api *API) listProjects(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actors.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	repository, ok := api.lookup(writer, request, actor)
	if !ok {
		return
	}
	projects, err := api.store.List(request.Context(), repository.ID)
	if err != nil {
		api.storeError(writer, request, "list projects", err)
		return
	}
	viewerCanWrite := false
	if actor != nil {
		viewerCanWrite, ok = api.canWrite(writer, request, *actor, repository)
		if !ok {
			return
		}
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"projects": projects, "viewerCanWrite": viewerCanWrite,
	})
}

func (api *API) getProject(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actors.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	repository, ok := api.lookup(writer, request, actor)
	if !ok {
		return
	}
	number, ok := parseNumber(writer, request.PathValue("number"))
	if !ok {
		return
	}
	project, err := api.store.Get(request.Context(), repository.ID, number)
	if err != nil {
		api.storeError(writer, request, "get project", err)
		return
	}
	if actor != nil {
		project.ViewerCanWrite, ok = api.canWrite(writer, request, *actor, repository)
		if !ok {
			return
		}
	}
	writeJSON(writer, http.StatusOK, project)
}

func (api *API) createProject(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.requireWrite(writer, request)
	if !ok {
		return
	}
	var body projectRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	project, err := api.store.Create(request.Context(), actor, repositoryRef(repository), ProjectInput{
		Title: body.Title, Description: body.Description, State: body.State,
	})
	if err != nil {
		api.storeError(writer, request, "create project", err)
		return
	}
	writer.Header().Set("Location", request.URL.Path+"/"+formatNumber(project.Number))
	writeJSON(writer, http.StatusCreated, project)
}

func (api *API) updateProject(writer http.ResponseWriter, request *http.Request) {
	actor, repository, number, ok := api.projectMutation(writer, request)
	if !ok {
		return
	}
	var body projectPatchRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	project, err := api.store.Update(request.Context(), actor, repositoryRef(repository), number, ProjectUpdate{
		Title: body.Title, Description: body.Description, State: body.State,
	})
	if err != nil {
		api.storeError(writer, request, "update project", err)
		return
	}
	writeJSON(writer, http.StatusOK, project)
}

func (api *API) deleteProject(writer http.ResponseWriter, request *http.Request) {
	actor, repository, number, ok := api.projectMutation(writer, request)
	if !ok {
		return
	}
	if err := api.store.Delete(request.Context(), actor, repositoryRef(repository), number); err != nil {
		api.storeError(writer, request, "delete project", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) createColumn(writer http.ResponseWriter, request *http.Request) {
	actor, repository, number, ok := api.projectMutation(writer, request)
	if !ok {
		return
	}
	var body columnRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	project, err := api.store.CreateColumn(request.Context(), actor, repositoryRef(repository), number,
		ColumnInput{Name: body.Name})
	if err != nil {
		api.storeError(writer, request, "create project column", err)
		return
	}
	writeJSON(writer, http.StatusCreated, project)
}

func (api *API) updateColumn(writer http.ResponseWriter, request *http.Request) {
	actor, repository, number, ok := api.projectMutation(writer, request)
	if !ok {
		return
	}
	columnID, ok := parseUUID(writer, request.PathValue("columnID"))
	if !ok {
		return
	}
	var body columnRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	project, err := api.store.UpdateColumn(request.Context(), actor, repositoryRef(repository), number,
		columnID, ColumnInput{Name: body.Name})
	if err != nil {
		api.storeError(writer, request, "update project column", err)
		return
	}
	writeJSON(writer, http.StatusOK, project)
}

func (api *API) deleteColumn(writer http.ResponseWriter, request *http.Request) {
	actor, repository, number, ok := api.projectMutation(writer, request)
	if !ok {
		return
	}
	columnID, ok := parseUUID(writer, request.PathValue("columnID"))
	if !ok {
		return
	}
	if err := api.store.DeleteColumn(
		request.Context(), actor, repositoryRef(repository), number, columnID,
	); err != nil {
		api.storeError(writer, request, "delete project column", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) createItem(writer http.ResponseWriter, request *http.Request) {
	actor, repository, number, ok := api.projectMutation(writer, request)
	if !ok {
		return
	}
	var body itemRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	project, err := api.store.CreateItem(request.Context(), actor, repositoryRef(repository), number, ItemInput{
		ColumnID: body.ColumnID, Kind: body.Kind, IssueNumber: body.IssueNumber,
		MergeRequestNumber: body.MergeRequestNumber, Title: body.Title, Body: body.Body,
	})
	if err != nil {
		api.storeError(writer, request, "create project item", err)
		return
	}
	writeJSON(writer, http.StatusCreated, project)
}

func (api *API) updateItem(writer http.ResponseWriter, request *http.Request) {
	actor, repository, number, ok := api.projectMutation(writer, request)
	if !ok {
		return
	}
	itemID, ok := parseUUID(writer, request.PathValue("itemID"))
	if !ok {
		return
	}
	var body itemPatchRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	project, err := api.store.UpdateItem(request.Context(), actor, repositoryRef(repository), number, itemID,
		ItemUpdate{ColumnID: body.ColumnID, Title: body.Title, Body: body.Body})
	if err != nil {
		api.storeError(writer, request, "update project item", err)
		return
	}
	writeJSON(writer, http.StatusOK, project)
}

func (api *API) deleteItem(writer http.ResponseWriter, request *http.Request) {
	actor, repository, number, ok := api.projectMutation(writer, request)
	if !ok {
		return
	}
	itemID, ok := parseUUID(writer, request.PathValue("itemID"))
	if !ok {
		return
	}
	if err := api.store.DeleteItem(request.Context(), actor, repositoryRef(repository), number, itemID); err != nil {
		api.storeError(writer, request, "delete project item", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) projectMutation(
	writer http.ResponseWriter,
	request *http.Request,
) (platform.User, collab.Repository, int64, bool) {
	actor, repository, ok := api.requireWrite(writer, request)
	if !ok {
		return platform.User{}, collab.Repository{}, 0, false
	}
	number, ok := parseNumber(writer, request.PathValue("number"))
	return actor, repository, number, ok
}

func parseNumber(writer http.ResponseWriter, value string) (int64, bool) {
	var number int64
	if value == "" {
		writeNotFound(writer)
		return 0, false
	}
	for _, character := range value {
		if character < '0' || character > '9' || number > (1<<62) {
			writeNotFound(writer)
			return 0, false
		}
		number = number*10 + int64(character-'0')
	}
	if number < 1 {
		writeNotFound(writer)
		return 0, false
	}
	return number, true
}

func parseUUID(writer http.ResponseWriter, value string) (string, bool) {
	parsed, err := uuid.Parse(value)
	if err != nil {
		writeNotFound(writer)
		return "", false
	}
	return parsed.String(), true
}

func writeNotFound(writer http.ResponseWriter) {
	writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
}

func formatNumber(number int64) string {
	if number == 0 {
		return "0"
	}
	digits := make([]byte, 0, 20)
	for number > 0 {
		digits = append(digits, byte('0'+number%10))
		number /= 10
	}
	for left, right := 0, len(digits)-1; left < right; left, right = left+1, right-1 {
		digits[left], digits[right] = digits[right], digits[left]
	}
	return string(digits)
}
