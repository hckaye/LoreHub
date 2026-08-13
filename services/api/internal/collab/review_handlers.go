package collab

import (
	"net/http"
	"strings"
)

type mergeRequestPatchRequest struct {
	Title *string `json:"title"`
	Body  *string `json:"body"`
	State *string `json:"state"`
}

type reviewRequest struct {
	Decision string `json:"decision"`
	Body     string `json:"body"`
}

func (api *API) getMergeRequest(writer http.ResponseWriter, request *http.Request) {
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
	var mr MergeRequest
	var err error
	if reader, supported := api.store.(ReactionReadStore); supported {
		mr, err = reader.GetMergeRequestWithReactions(
			requestContext(request), repo.ID, number, reactionViewer(actor),
		)
	} else {
		mr, err = api.store.GetMergeRequest(requestContext(request), repo.ID, number)
	}
	if err != nil {
		storeError(writer, request, "get merge request", err, api.logger)
		return
	}
	if actor != nil {
		access, ok := api.permission(writer, request, *actor, repo)
		if !ok {
			return
		}
		mr.ViewerCanUpdate = repo.ArchivedAt == nil && mr.State != "merged" &&
			(mr.AuthorID == actor.ID || access.AtLeast(PermTriage))
		mr.ViewerCanReview = repo.ArchivedAt == nil && mr.AuthorID != actor.ID &&
			access.AtLeast(PermRead) && mr.State == "open"
	}
	mr.Reactions = ensureReactions(mr.Reactions)
	writeJSON(writer, http.StatusOK, mr)
}

func (api *API) patchMergeRequest(writer http.ResponseWriter, request *http.Request) {
	actor, repo, ok := api.requireMutationActor(writer, request)
	if !ok {
		return
	}
	number, ok := parseNumber(writer, request.PathValue("number"))
	if !ok {
		return
	}
	var body mergeRequestPatchRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	input, err := buildMergeRequestUpdateInput(body, request.Header.Get("If-Match"))
	if err != nil {
		validationError(writer, err)
		return
	}
	if input.empty() {
		writeProblem(writer, http.StatusBadRequest, "invalid_input",
			"At least one of title, body or state must be supplied")
		return
	}
	mr, err := api.store.UpdateMergeRequest(requestContext(request), actor, repo.ID, number, input)
	if err != nil {
		storeError(writer, request, "update merge request", err, api.logger)
		return
	}
	mr.ViewerCanUpdate = true
	mr.ViewerCanReview = mr.AuthorID != actor.ID && mr.State == "open"
	mr.Reactions = ensureReactions(mr.Reactions)
	writeJSON(writer, http.StatusOK, mr)
}

func buildMergeRequestUpdateInput(
	body mergeRequestPatchRequest, ifMatchHeader string,
) (UpdateMergeRequestInput, error) {
	input := UpdateMergeRequestInput{}
	if body.Title != nil {
		title, err := validateTitle(*body.Title)
		if err != nil {
			return UpdateMergeRequestInput{}, err
		}
		input.Title = &title
	}
	if body.Body != nil {
		value, err := validateBody(*body.Body, false)
		if err != nil {
			return UpdateMergeRequestInput{}, err
		}
		input.Body = &value
	}
	if body.State != nil {
		state, err := validateMergeRequestState(strings.TrimSpace(*body.State))
		if err != nil {
			return UpdateMergeRequestInput{}, err
		}
		input.State = &state
	}
	if ifMatchHeader != "" {
		if updated, ok := parseIfMatch(ifMatchHeader); ok {
			input.IfMatch = &updated
		} else {
			return UpdateMergeRequestInput{}, ErrInvalidPrecondition
		}
	}
	return input, nil
}

func (input UpdateMergeRequestInput) empty() bool {
	return input.Title == nil && input.Body == nil && input.State == nil
}

func (api *API) listReviews(writer http.ResponseWriter, request *http.Request) {
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
	summary, err := api.store.ListReviews(requestContext(request), repo.ID, number)
	if err != nil {
		storeError(writer, request, "list reviews", err, api.logger)
		return
	}
	writeJSON(writer, http.StatusOK, summary)
}

func (api *API) createReview(writer http.ResponseWriter, request *http.Request) {
	actor, repo, ok := api.requireMutationActor(writer, request)
	if !ok {
		return
	}
	number, ok := parseNumber(writer, request.PathValue("number"))
	if !ok {
		return
	}
	var body reviewRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	input, err := validateReviewInput(ReviewInput{Decision: body.Decision, Body: body.Body})
	if err != nil {
		validationError(writer, err)
		return
	}
	review, created, err := api.store.CreateReview(requestContext(request), actor, repo.ID, number, input)
	if err != nil {
		storeError(writer, request, "create review", err, api.logger)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
		writeLocation(writer, request, review.ID)
	}
	writeJSON(writer, status, review)
}
