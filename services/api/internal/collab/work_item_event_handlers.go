package collab

import "net/http"

func (api *API) listIssueEvents(writer http.ResponseWriter, request *http.Request) {
	api.listWorkItemEvents(writer, request, WorkItemIssue)
}

func (api *API) listMergeRequestEvents(writer http.ResponseWriter, request *http.Request) {
	api.listWorkItemEvents(writer, request, WorkItemMergeRequest)
}

// listWorkItemEvents serves the issue and pull request timelines. Read access
// matches the comment endpoints: anonymous callers see events of repositories
// they can already read, and personal access tokens are accepted.
func (api *API) listWorkItemEvents(
	writer http.ResponseWriter,
	request *http.Request,
	itemKind string,
) {
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
	if api.events == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "events_unavailable",
			"Timeline events are unavailable")
		return
	}
	page, _, err := parsePage(request.URL.Query())
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	result, err := api.events.ListWorkItemEvents(
		requestContext(request), repo.ID, itemKind, number, page,
	)
	if err != nil {
		storeError(writer, request, "list work item events", err, api.logger)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}
