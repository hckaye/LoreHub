package collab

import (
	"net/http"
	"strings"
)

// issuePatchRequest is the JSON body for editing an issue. Pointer fields are
// nil when omitted, supporting partial updates.
type issuePatchRequest struct {
	Title *string `json:"title"`
	Body  *string `json:"body"`
	State *string `json:"state"`
}

func (api *API) getIssue(writer http.ResponseWriter, request *http.Request) {
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
	var issue Issue
	var err error
	if reader, supported := api.store.(ReactionReadStore); supported {
		issue, err = reader.GetIssueWithReactions(requestContext(request), repo.ID, number, reactionViewer(actor))
	} else {
		issue, err = api.store.GetIssue(requestContext(request), repo.ID, number)
	}
	if err != nil {
		storeError(writer, request, "get issue", err, api.logger)
		return
	}
	if actor != nil {
		access, ok := api.permission(writer, request, *actor, repo)
		if !ok {
			return
		}
		issue.ViewerCanUpdate = !repositoryReadOnly(repo) &&
			(issue.AuthorID == actor.ID || access.AtLeast(PermTriage))
		canManage := !repositoryReadOnly(repo) && access.AtLeast(PermTriage)
		issue.ViewerCanManageLabels = canManage
		issue.ViewerCanManageMilestone = canManage
		issue.ViewerCanManageAssignees = canManage
	}
	issue.Reactions = ensureReactions(issue.Reactions)
	writeJSON(writer, http.StatusOK, issue)
}

func (api *API) patchIssue(writer http.ResponseWriter, request *http.Request) {
	actor, repo, ok := api.requireMutationActor(writer, request)
	if !ok {
		return
	}
	access, ok := api.permission(writer, request, actor, repo)
	if !ok {
		return
	}
	number, ok := parseNumber(writer, request.PathValue("number"))
	if !ok {
		return
	}
	var body issuePatchRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	input, err := buildIssueUpdateInput(body, request.Header.Get("If-Match"))
	if err != nil {
		validationError(writer, err)
		return
	}
	if input.empty() {
		writeProblem(writer, http.StatusBadRequest, "invalid_input",
			"At least one of title, body or state must be supplied")
		return
	}
	issue, err := api.store.UpdateIssue(requestContext(request), actor, repo.ID, number, input)
	if err != nil {
		storeError(writer, request, "update issue", err, api.logger)
		return
	}
	issue.ViewerCanUpdate = true
	issue.ViewerCanManageLabels = access.AtLeast(PermTriage)
	issue.ViewerCanManageMilestone = access.AtLeast(PermTriage)
	issue.ViewerCanManageAssignees = access.AtLeast(PermTriage)
	issue.Reactions = ensureReactions(issue.Reactions)
	writeJSON(writer, http.StatusOK, issue)
}

func buildIssueUpdateInput(body issuePatchRequest, ifMatchHeader string) (UpdateIssueInput, error) {
	input := UpdateIssueInput{}
	if body.Title != nil {
		title, err := validateTitle(*body.Title)
		if err != nil {
			return UpdateIssueInput{}, err
		}
		input.Title = &title
	}
	if body.Body != nil {
		value, err := validateBody(*body.Body, false)
		if err != nil {
			return UpdateIssueInput{}, err
		}
		input.Body = &value
	}
	if body.State != nil {
		state, err := validateIssueState(strings.TrimSpace(*body.State))
		if err != nil {
			return UpdateIssueInput{}, err
		}
		input.State = &state
	}
	if ifMatchHeader != "" {
		if updated, ok := parseIfMatch(ifMatchHeader); ok {
			input.IfMatch = &updated
		} else {
			return UpdateIssueInput{}, ErrInvalidPrecondition
		}
	}
	return input, nil
}

func (input UpdateIssueInput) empty() bool {
	return input.Title == nil && input.Body == nil && input.State == nil
}
