package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/lorehub/lorehub/services/api/internal/collab"
)

var repositoryIssueQueryKeys = map[string]struct{}{
	"state": {}, "q": {}, "author": {}, "assignee": {}, "label": {},
	"milestone": {}, "sort": {}, "direction": {}, "page": {}, "per_page": {},
}

var repositoryMergeRequestQueryKeys = map[string]struct{}{
	"state": {}, "q": {}, "author": {}, "assignee": {}, "label": {},
	"milestone": {}, "source": {}, "target": {}, "draft": {},
	"sort": {}, "direction": {}, "page": {}, "per_page": {},
}

func (api *API) listIssues(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	reader, ok := api.collabStore.(collab.RepositoryReadStore)
	if !ok {
		writeProblem(writer, http.StatusServiceUnavailable, "issues_unavailable", "Issue search is unavailable")
		return
	}
	repository, err := api.collabStore.LookupRepository(
		request.Context(), actor, request.PathValue("owner"), request.PathValue("repository"),
	)
	if err != nil {
		api.platformError(writer, request, "list issues", err)
		return
	}
	query, ok := parseRepositoryIssueQuery(writer, request.URL.Query())
	if !ok {
		return
	}
	page, err := reader.ListIssuesForRepository(request.Context(), repository.ID, query)
	if err != nil {
		api.repositoryWorkItemError(writer, request, "list issues", err)
		return
	}
	if actor != nil {
		access, err := api.collabStore.RepositoryPermission(request.Context(), *actor, repository)
		if err != nil {
			api.internalError(writer, request, "resolve issue list permissions", err)
			return
		}
		for index := range page.Issues {
			page.Issues[index].ViewerCanUpdate = repository.ArchivedAt == nil &&
				(page.Issues[index].AuthorID == actor.ID || access.AtLeast(collab.PermTriage))
			canManage := repository.ArchivedAt == nil && access.AtLeast(collab.PermTriage)
			page.Issues[index].ViewerCanManageLabels = canManage
			page.Issues[index].ViewerCanManageMilestone = canManage
			page.Issues[index].ViewerCanManageAssignees = canManage
		}
	}
	writeJSON(writer, http.StatusOK, page)
}

func (api *API) listMergeRequests(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	reader, ok := api.collabStore.(collab.RepositoryReadStore)
	if !ok {
		writeProblem(
			writer, http.StatusServiceUnavailable, "pull_requests_unavailable",
			"Pull request search is unavailable",
		)
		return
	}
	repository, err := api.collabStore.LookupRepository(
		request.Context(), actor, request.PathValue("owner"), request.PathValue("repository"),
	)
	if err != nil {
		api.platformError(writer, request, "list pull requests", err)
		return
	}
	query, ok := parseRepositoryMergeRequestQuery(writer, request.URL.Query())
	if !ok {
		return
	}
	page, err := reader.ListMergeRequestsForRepository(request.Context(), repository.ID, query)
	if err != nil {
		api.repositoryWorkItemError(writer, request, "list pull requests", err)
		return
	}
	if actor != nil {
		access, err := api.collabStore.RepositoryPermission(request.Context(), *actor, repository)
		if err != nil {
			api.internalError(writer, request, "resolve pull request list permissions", err)
			return
		}
		for index := range page.MergeRequests {
			item := &page.MergeRequests[index]
			item.ViewerCanUpdate = repository.ArchivedAt == nil && item.State != "merged" &&
				(item.AuthorID == actor.ID || access.AtLeast(collab.PermTriage))
			item.ViewerCanReview = repository.ArchivedAt == nil && item.AuthorID != actor.ID &&
				item.State == "open" && access.AtLeast(collab.PermRead)
		}
	}
	writeJSON(writer, http.StatusOK, page)
}

func parseRepositoryIssueQuery(
	writer http.ResponseWriter,
	values url.Values,
) (collab.RepositoryIssueQuery, bool) {
	if !validRepositoryQueryKeys(values, repositoryIssueQueryKeys) {
		invalidRepositoryQuery(writer)
		return collab.RepositoryIssueQuery{}, false
	}
	state, ok := singleRepositoryQueryValue(values, "state")
	if !ok {
		invalidRepositoryQuery(writer)
		return collab.RepositoryIssueQuery{}, false
	}
	query := collab.RepositoryIssueQuery{
		State: state, Search: values.Get("q"), Author: values.Get("author"),
		Assignee: values.Get("assignee"), Labels: values["label"],
		Sort: values.Get("sort"), Direction: values.Get("direction"),
	}
	if !parseRepositoryMilestone(values.Get("milestone"), &query.MilestoneNumber, &query.WithoutMilestone) ||
		!parseRepositoryPagination(values, &query.Page, &query.PerPage) ||
		!singleRepositoryQueryValues(values, "q", "author", "assignee", "milestone", "sort", "direction",
			"page", "per_page") {
		invalidRepositoryQuery(writer)
		return collab.RepositoryIssueQuery{}, false
	}
	query, err := collab.NormalizeRepositoryIssueQuery(query)
	if err != nil {
		invalidRepositoryQuery(writer)
		return collab.RepositoryIssueQuery{}, false
	}
	return query, true
}

func parseRepositoryMergeRequestQuery(
	writer http.ResponseWriter,
	values url.Values,
) (collab.RepositoryMergeRequestQuery, bool) {
	if !validRepositoryQueryKeys(values, repositoryMergeRequestQueryKeys) ||
		!singleRepositoryQueryValues(
			values, "state", "q", "author", "assignee", "milestone", "source", "target",
			"draft", "sort", "direction", "page", "per_page",
		) {
		invalidRepositoryQuery(writer)
		return collab.RepositoryMergeRequestQuery{}, false
	}
	query := collab.RepositoryMergeRequestQuery{
		State: values.Get("state"), Search: values.Get("q"), Author: values.Get("author"),
		Assignee: values.Get("assignee"), Labels: values["label"],
		SourceBranch: values.Get("source"), TargetBranch: values.Get("target"),
		Sort: values.Get("sort"), Direction: values.Get("direction"),
	}
	if !parseRepositoryMilestone(values.Get("milestone"), &query.MilestoneNumber, &query.WithoutMilestone) ||
		!parseRepositoryDraft(values.Get("draft"), &query.Draft) ||
		!parseRepositoryPagination(values, &query.Page, &query.PerPage) {
		invalidRepositoryQuery(writer)
		return collab.RepositoryMergeRequestQuery{}, false
	}
	query, err := collab.NormalizeRepositoryMergeRequestQuery(query)
	if err != nil {
		invalidRepositoryQuery(writer)
		return collab.RepositoryMergeRequestQuery{}, false
	}
	return query, true
}

func validRepositoryQueryKeys(values url.Values, allowed map[string]struct{}) bool {
	for key := range values {
		if _, ok := allowed[key]; !ok {
			return false
		}
	}
	return true
}

func singleRepositoryQueryValues(values url.Values, keys ...string) bool {
	for _, key := range keys {
		if len(values[key]) > 1 {
			return false
		}
	}
	return true
}

func singleRepositoryQueryValue(values url.Values, key string) (string, bool) {
	if len(values[key]) > 1 {
		return "", false
	}
	return values.Get(key), true
}

func parseRepositoryMilestone(value string, number **int64, without *bool) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	if value == "none" {
		*without = true
		return true
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 1 {
		return false
	}
	*number = &parsed
	return true
}

func parseRepositoryDraft(value string, draft **bool) bool {
	switch value {
	case "":
		return true
	case "true":
		parsed := true
		*draft = &parsed
		return true
	case "false":
		parsed := false
		*draft = &parsed
		return true
	default:
		return false
	}
}

func parseRepositoryPagination(values url.Values, page *int, perPage *int) bool {
	var ok bool
	*page, ok = parseRepositoryPositiveInteger(values.Get("page"), 1)
	if !ok {
		return false
	}
	*perPage, ok = parseRepositoryPositiveInteger(values.Get("per_page"), 25)
	return ok
}

func parseRepositoryPositiveInteger(value string, fallback int) (int, bool) {
	if value == "" {
		return fallback, true
	}
	parsed, err := strconv.Atoi(value)
	return parsed, err == nil && parsed > 0
}

func invalidRepositoryQuery(writer http.ResponseWriter) {
	writeProblem(writer, http.StatusBadRequest, "invalid_query", "The list filters are invalid")
}

func (api *API) repositoryWorkItemError(
	writer http.ResponseWriter,
	request *http.Request,
	operation string,
	err error,
) {
	if errors.Is(err, collab.ErrInvalidRepositoryWorkItemQuery) {
		invalidRepositoryQuery(writer)
		return
	}
	api.internalError(writer, request, operation, err)
}
