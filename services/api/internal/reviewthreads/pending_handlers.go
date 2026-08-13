package reviewthreads

import (
	"net/http"
	"strings"
)

const maxPendingReviewBodyBytes = 1 << 20

type pendingReviewRequest struct {
	Body string `json:"body"`
}

type submitPendingReviewRequest struct {
	Verdict string  `json:"verdict"`
	Body    *string `json:"body"`
}

func (api *API) startPendingReview(writer http.ResponseWriter, request *http.Request) {
	repository, actor, ok := api.mutationRepository(writer, request)
	if !ok {
		return
	}
	number, ok := requestNumber(writer, request)
	if !ok {
		return
	}
	pending, created, err := api.store.StartPendingReview(
		request.Context(), actor, repositoryRef(repository), number,
	)
	if err != nil {
		api.storeError(writer, request, "start pending review", err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(writer, status, pending)
}

func (api *API) updatePendingReview(writer http.ResponseWriter, request *http.Request) {
	repository, actor, ok := api.mutationRepository(writer, request)
	if !ok {
		return
	}
	number, ok := requestNumber(writer, request)
	if !ok {
		return
	}
	var body pendingReviewRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	value, err := normalizeReviewBody(body.Body)
	if err != nil {
		api.storeError(writer, request, "validate pending review body", err)
		return
	}
	pending, err := api.store.UpdatePendingReview(
		request.Context(), actor, repositoryRef(repository), number, value,
	)
	if err != nil {
		api.storeError(writer, request, "update pending review", err)
		return
	}
	writeJSON(writer, http.StatusOK, pending)
}

func (api *API) submitPendingReview(writer http.ResponseWriter, request *http.Request) {
	repository, actor, ok := api.mutationRepository(writer, request)
	if !ok {
		return
	}
	number, ok := requestNumber(writer, request)
	if !ok {
		return
	}
	var body submitPendingReviewRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	input, err := normalizeSubmit(body)
	if err != nil {
		api.storeError(writer, request, "validate review verdict", err)
		return
	}
	result, err := api.store.SubmitPendingReview(
		request.Context(), actor, repositoryRef(repository), number, input,
	)
	if err != nil {
		api.storeError(writer, request, "submit pending review", err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (api *API) discardPendingReview(writer http.ResponseWriter, request *http.Request) {
	repository, actor, ok := api.mutationRepository(writer, request)
	if !ok {
		return
	}
	number, ok := requestNumber(writer, request)
	if !ok {
		return
	}
	err := api.store.DiscardPendingReview(request.Context(), actor, repositoryRef(repository), number)
	if err != nil {
		api.storeError(writer, request, "discard pending review", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

// normalizeSubmit maps the verdict of the review form onto the decisions the
// merge request review table stores.
func normalizeSubmit(body submitPendingReviewRequest) (SubmitInput, error) {
	input := SubmitInput{}
	switch strings.TrimSpace(body.Verdict) {
	case "approve":
		input.Decision = "approved"
	case "request_changes":
		input.Decision = "changes_requested"
	case "comment":
		input.Decision = "commented"
	default:
		return SubmitInput{}, invalid("verdict must be approve, request_changes or comment")
	}
	if body.Body != nil {
		value, err := normalizeReviewBody(*body.Body)
		if err != nil {
			return SubmitInput{}, err
		}
		input.Body = &value
	}
	return input, nil
}

func normalizeReviewBody(body string) (string, error) {
	if len(body) > maxPendingReviewBodyBytes {
		return "", invalid("review body must not exceed 1048576 bytes")
	}
	return body, nil
}
