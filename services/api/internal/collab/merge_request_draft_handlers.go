package collab

import (
	"errors"
	"net/http"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func (api *API) putMergeRequestDraft(writer http.ResponseWriter, request *http.Request) {
	api.setMergeRequestDraft(writer, request, true)
}

func (api *API) deleteMergeRequestDraft(writer http.ResponseWriter, request *http.Request) {
	api.setMergeRequestDraft(writer, request, false)
}

func (api *API) setMergeRequestDraft(writer http.ResponseWriter, request *http.Request, isDraft bool) {
	actor, repository, ok := api.requireMutationActor(writer, request)
	if !ok {
		return
	}
	number, ok := parseNumber(writer, request.PathValue("number"))
	if !ok || !api.requireMergeRequestDraftStore(writer) {
		return
	}
	mergeRequest, _, err := api.drafts.SetMergeRequestDraft(
		requestContext(request), actor, repository.ID, number, isDraft,
	)
	if errors.Is(err, ErrMergeBusy) {
		writeProblem(writer, http.StatusConflict, "merge_busy",
			"The pull request cannot become a draft while its merge is being pushed")
		return
	}
	if errors.Is(err, platform.ErrConflict) {
		writeProblem(writer, http.StatusConflict, "invalid_state",
			"Only an open pull request can change draft state")
		return
	}
	if err != nil {
		storeError(writer, request, "update pull request draft", err, api.logger)
		return
	}
	mergeRequest.ViewerCanUpdate = true
	mergeRequest.ViewerCanReview = mergeRequest.AuthorID != actor.ID && mergeRequest.State == "open"
	writeJSON(writer, http.StatusOK, mergeRequest)
}

func (api *API) requireMergeRequestDraftStore(writer http.ResponseWriter) bool {
	if api.drafts != nil {
		return true
	}
	writeProblem(writer, http.StatusServiceUnavailable, "pull_request_drafts_unavailable",
		"Pull request drafts are unavailable")
	return false
}
