package collab

import (
	"net/http"
	"strings"
)

type labelRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
}

type labelListResponse struct {
	Items          []Label `json:"items"`
	NextCursor     string  `json:"nextCursor,omitempty"`
	HasMore        bool    `json:"hasMore"`
	ViewerCanWrite bool    `json:"viewerCanWrite"`
}

func (api *API) listLabels(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.optionalActor(writer, request)
	if !ok {
		return
	}
	repo, ok := api.lookup(writer, request, actor)
	if !ok {
		return
	}
	page, _, err := parsePage(request.URL.Query())
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	result, err := api.store.ListLabels(requestContext(request), repo.ID, page)
	if err != nil {
		storeError(writer, request, "list labels", err, api.logger)
		return
	}
	viewerCanWrite := false
	if actor != nil {
		access, allowed := api.permission(writer, request, *actor, repo)
		if !allowed {
			return
		}
		viewerCanWrite = access.AtLeast(PermWrite)
	}
	writeJSON(writer, http.StatusOK, labelListResponse{
		Items: result.Items, NextCursor: result.NextCursor, HasMore: result.HasMore,
		ViewerCanWrite: viewerCanWrite,
	})
}

func (api *API) createLabel(writer http.ResponseWriter, request *http.Request) {
	actor, repo, ok := api.requireMutationActor(writer, request)
	if !ok {
		return
	}
	access, ok := api.permission(writer, request, actor, repo)
	if !ok {
		return
	}
	if !requireLevel(writer, access, PermWrite) {
		return
	}
	input, ok := decodeLabelRequest(writer, request)
	if !ok {
		return
	}
	label, err := api.store.CreateLabel(requestContext(request), actor, repo.ID, input)
	if err != nil {
		storeError(writer, request, "create label", err, api.logger)
		return
	}
	writeLocation(writer, request, label.ID)
	writeJSON(writer, http.StatusCreated, label)
}

func (api *API) patchLabel(writer http.ResponseWriter, request *http.Request) {
	actor, repo, ok := api.requireMutationActor(writer, request)
	if !ok {
		return
	}
	labelID := strings.TrimSpace(request.PathValue("labelID"))
	if labelID == "" {
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return
	}
	input, ok := decodeLabelRequest(writer, request)
	if !ok {
		return
	}
	label, err := api.store.UpdateLabel(requestContext(request), actor, repo.ID, labelID, input)
	if err != nil {
		storeError(writer, request, "update label", err, api.logger)
		return
	}
	writeJSON(writer, http.StatusOK, label)
}

func (api *API) deleteLabel(writer http.ResponseWriter, request *http.Request) {
	actor, repo, ok := api.requireMutationActor(writer, request)
	if !ok {
		return
	}
	labelID := strings.TrimSpace(request.PathValue("labelID"))
	if labelID == "" {
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return
	}
	if err := api.store.DeleteLabel(requestContext(request), actor, repo.ID, labelID); err != nil {
		storeError(writer, request, "delete label", err, api.logger)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) putIssueLabel(writer http.ResponseWriter, request *http.Request) {
	actor, repo, ok := api.requireMutationActor(writer, request)
	if !ok {
		return
	}
	access, ok := api.permission(writer, request, actor, repo)
	if !ok {
		return
	}
	if !requireLevel(writer, access, PermTriage) {
		return
	}
	number, ok := parseNumber(writer, request.PathValue("number"))
	if !ok {
		return
	}
	labelID := strings.TrimSpace(request.PathValue("labelID"))
	if labelID == "" {
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return
	}
	label, applied, err := api.store.ApplyLabel(requestContext(request), actor, repo.ID, number, labelID)
	if err != nil {
		storeError(writer, request, "apply label", err, api.logger)
		return
	}
	if applied {
		writeJSON(writer, http.StatusCreated, label)
		return
	}
	writeJSON(writer, http.StatusOK, label)
}

func (api *API) deleteIssueLabel(writer http.ResponseWriter, request *http.Request) {
	actor, repo, ok := api.requireMutationActor(writer, request)
	if !ok {
		return
	}
	access, ok := api.permission(writer, request, actor, repo)
	if !ok {
		return
	}
	if !requireLevel(writer, access, PermTriage) {
		return
	}
	number, ok := parseNumber(writer, request.PathValue("number"))
	if !ok {
		return
	}
	labelID := strings.TrimSpace(request.PathValue("labelID"))
	if labelID == "" {
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return
	}
	if err := api.store.RemoveLabel(requestContext(request), actor, repo.ID, number, labelID); err != nil {
		storeError(writer, request, "remove label", err, api.logger)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func decodeLabelRequest(writer http.ResponseWriter, request *http.Request) (LabelInput, bool) {
	var body labelRequest
	if !decodeJSON(writer, request, &body) {
		return LabelInput{}, false
	}
	input, err := validateLabelInput(LabelInput{
		Name:        body.Name,
		Description: body.Description,
		Color:       body.Color,
	})
	if err != nil {
		validationError(writer, err)
		return LabelInput{}, false
	}
	return input, true
}
