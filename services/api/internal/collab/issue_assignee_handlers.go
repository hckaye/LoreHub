package collab

import (
	"errors"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func (api *API) listAssignableUsers(writer http.ResponseWriter, request *http.Request) {
	actor, repo, ok := api.requireMutationActor(writer, request)
	if !ok {
		return
	}
	access, ok := api.permission(writer, request, actor, repo)
	if !ok || !requireLevel(writer, access, PermTriage) {
		return
	}
	if api.assignees == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "assignees_unavailable",
			"Issue assignees are unavailable")
		return
	}
	page, _, err := parsePage(request.URL.Query())
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	query := strings.TrimSpace(request.URL.Query().Get("query"))
	if !validAssigneeValue(query, true) {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "The assignee query is invalid")
		return
	}
	result, err := api.assignees.ListAssignableUsers(requestContext(request), repo.ID, query, page)
	if err != nil {
		storeError(writer, request, "list assignable users", err, api.logger)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (api *API) putIssueAssignee(writer http.ResponseWriter, request *http.Request) {
	actor, repo, issueNumber, username, ok := api.issueAssigneeMutationContext(writer, request)
	if !ok {
		return
	}
	assignee, assigned, err := api.assignees.AssignIssueUser(
		requestContext(request), actor, repo.ID, issueNumber, username,
	)
	if err != nil {
		if errors.Is(err, ErrAssigneeLimit) {
			writeProblem(writer, http.StatusConflict, "assignee_limit", ErrAssigneeLimit.Error())
			return
		}
		storeError(writer, request, "assign issue user", err, api.logger)
		return
	}
	if assigned {
		writeJSON(writer, http.StatusCreated, assignee)
		return
	}
	writeJSON(writer, http.StatusOK, assignee)
}

func (api *API) deleteIssueAssignee(writer http.ResponseWriter, request *http.Request) {
	actor, repo, issueNumber, username, ok := api.issueAssigneeMutationContext(writer, request)
	if !ok {
		return
	}
	if err := api.assignees.RemoveIssueUser(
		requestContext(request), actor, repo.ID, issueNumber, username,
	); err != nil {
		storeError(writer, request, "remove issue user", err, api.logger)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) issueAssigneeMutationContext(
	writer http.ResponseWriter,
	request *http.Request,
) (platform.User, Repository, int64, string, bool) {
	actor, repo, ok := api.requireMutationActor(writer, request)
	if !ok {
		return platform.User{}, Repository{}, 0, "", false
	}
	access, ok := api.permission(writer, request, actor, repo)
	if !ok || !requireLevel(writer, access, PermTriage) {
		return platform.User{}, Repository{}, 0, "", false
	}
	if api.assignees == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "assignees_unavailable",
			"Issue assignees are unavailable")
		return platform.User{}, Repository{}, 0, "", false
	}
	issueNumber, ok := parseNumber(writer, request.PathValue("number"))
	if !ok {
		return platform.User{}, Repository{}, 0, "", false
	}
	username := strings.TrimSpace(request.PathValue("username"))
	if !validAssigneeValue(username, false) {
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return platform.User{}, Repository{}, 0, "", false
	}
	return actor, repo, issueNumber, username, true
}

func validAssigneeValue(value string, allowEmpty bool) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > 255 {
		return false
	}
	if !allowEmpty && value == "" {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
