package collab

import (
	"net/http"
	"strings"
)

type commentRequest struct {
	Body string `json:"body"`
}

func (api *API) listIssueComments(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.optionalActor(writer, request)
	if !ok {
		return
	}
	repo, ok := api.lookup(writer, request, actor)
	if !ok {
		return
	}
	number, ok := parseNumber(writer, request.PathValue("number"))
	if !ok {
		return
	}
	page, _, err := parsePage(request.URL.Query())
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	var result Result[IssueComment]
	if reader, supported := api.store.(ReactionReadStore); supported {
		result, err = reader.ListIssueCommentsWithReactions(
			requestContext(request), repo.ID, number, page, reactionViewer(actor),
		)
	} else {
		result, err = api.store.ListIssueComments(requestContext(request), repo.ID, number, page)
	}
	if err != nil {
		storeError(writer, request, "list issue comments", err, api.logger)
		return
	}
	if actor != nil {
		access, ok := api.permission(writer, request, *actor, repo)
		if !ok {
			return
		}
		for index := range result.Items {
			result.Items[index].ViewerCanUpdate = !repositoryReadOnly(repo) &&
				(result.Items[index].AuthorID == actor.ID || access.AtLeast(PermTriage))
		}
	}
	for index := range result.Items {
		result.Items[index].Reactions = ensureReactions(result.Items[index].Reactions)
	}
	writeJSON(writer, http.StatusOK, result)
}

func (api *API) createIssueComment(writer http.ResponseWriter, request *http.Request) {
	actor, repo, ok := api.requireMutationActor(writer, request)
	if !ok {
		return
	}
	number, ok := parseNumber(writer, request.PathValue("number"))
	if !ok {
		return
	}
	var body commentRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	value, err := validateBody(body.Body, true)
	if err != nil {
		validationError(writer, err)
		return
	}
	comment, err := api.store.CreateIssueComment(requestContext(request), actor, repo.ID, number, value)
	if err != nil {
		storeError(writer, request, "create issue comment", err, api.logger)
		return
	}
	comment.ViewerCanUpdate = true
	comment.Reactions = ensureReactions(comment.Reactions)
	writeLocation(writer, request, comment.ID)
	writeJSON(writer, http.StatusCreated, comment)
}

func (api *API) patchIssueComment(writer http.ResponseWriter, request *http.Request) {
	actor, repo, ok := api.requireMutationActor(writer, request)
	if !ok {
		return
	}
	number, ok := parseNumber(writer, request.PathValue("number"))
	if !ok {
		return
	}
	var body commentRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	value, err := validateBody(body.Body, true)
	if err != nil {
		validationError(writer, err)
		return
	}
	commentID := strings.TrimSpace(request.PathValue("commentID"))
	if commentID == "" {
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return
	}
	updated, err := api.store.UpdateIssueComment(
		requestContext(request), actor, repo.ID, number, commentID, value,
	)
	if err != nil {
		storeError(writer, request, "update issue comment", err, api.logger)
		return
	}
	updated.ViewerCanUpdate = true
	updated.Reactions = ensureReactions(updated.Reactions)
	writeJSON(writer, http.StatusOK, updated)
}

func (api *API) deleteIssueComment(writer http.ResponseWriter, request *http.Request) {
	actor, repo, ok := api.requireMutationActor(writer, request)
	if !ok {
		return
	}
	number, ok := parseNumber(writer, request.PathValue("number"))
	if !ok {
		return
	}
	commentID := strings.TrimSpace(request.PathValue("commentID"))
	if commentID == "" {
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return
	}
	if err := api.store.DeleteIssueComment(
		requestContext(request), actor, repo.ID, number, commentID,
	); err != nil {
		storeError(writer, request, "delete issue comment", err, api.logger)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}
