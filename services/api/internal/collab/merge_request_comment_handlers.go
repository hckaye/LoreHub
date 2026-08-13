package collab

import (
	"net/http"
	"strings"
)

func (api *API) mergeRequestConversations(
	writer http.ResponseWriter,
) (MergeRequestConversationStore, bool) {
	store, ok := api.store.(MergeRequestConversationStore)
	if !ok {
		writeProblem(writer, http.StatusServiceUnavailable, "service_unavailable",
			"Pull request comments are unavailable")
	}
	return store, ok
}

func (api *API) listMergeRequestComments(writer http.ResponseWriter, request *http.Request) {
	store, ok := api.mergeRequestConversations(writer)
	if !ok {
		return
	}
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
	var result Result[MergeRequestComment]
	if reader, supported := api.store.(ReactionReadStore); supported {
		result, err = reader.ListMergeRequestCommentsWithReactions(
			requestContext(request), repo.ID, number, page, reactionViewer(actor),
		)
	} else {
		result, err = store.ListMergeRequestComments(requestContext(request), repo.ID, number, page)
	}
	if err != nil {
		storeError(writer, request, "list pull request comments", err, api.logger)
		return
	}
	if actor != nil {
		access, ok := api.permission(writer, request, *actor, repo)
		if !ok {
			return
		}
		for index := range result.Items {
			result.Items[index].ViewerCanUpdate = repo.ArchivedAt == nil &&
				(result.Items[index].AuthorID == actor.ID || access.AtLeast(PermTriage))
		}
	}
	for index := range result.Items {
		result.Items[index].Reactions = ensureReactions(result.Items[index].Reactions)
	}
	writeJSON(writer, http.StatusOK, result)
}

func (api *API) createMergeRequestComment(writer http.ResponseWriter, request *http.Request) {
	store, ok := api.mergeRequestConversations(writer)
	if !ok {
		return
	}
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
	comment, err := store.CreateMergeRequestComment(
		requestContext(request), actor, repo.ID, number, value,
	)
	if err != nil {
		storeError(writer, request, "create pull request comment", err, api.logger)
		return
	}
	comment.ViewerCanUpdate = true
	comment.Reactions = ensureReactions(comment.Reactions)
	writeLocation(writer, request, comment.ID)
	writeJSON(writer, http.StatusCreated, comment)
}

func (api *API) patchMergeRequestComment(writer http.ResponseWriter, request *http.Request) {
	store, ok := api.mergeRequestConversations(writer)
	if !ok {
		return
	}
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
	var body commentRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	value, err := validateBody(body.Body, true)
	if err != nil {
		validationError(writer, err)
		return
	}
	comment, err := store.UpdateMergeRequestComment(
		requestContext(request), actor, repo.ID, number, commentID, value,
	)
	if err != nil {
		storeError(writer, request, "update pull request comment", err, api.logger)
		return
	}
	comment.ViewerCanUpdate = true
	comment.Reactions = ensureReactions(comment.Reactions)
	writeJSON(writer, http.StatusOK, comment)
}

func (api *API) deleteMergeRequestComment(writer http.ResponseWriter, request *http.Request) {
	store, ok := api.mergeRequestConversations(writer)
	if !ok {
		return
	}
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
	if err := store.DeleteMergeRequestComment(
		requestContext(request), actor, repo.ID, number, commentID,
	); err != nil {
		storeError(writer, request, "delete pull request comment", err, api.logger)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}
