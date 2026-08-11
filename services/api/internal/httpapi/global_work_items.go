package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type GlobalWorkItemStore interface {
	ListGlobalIssues(
		context.Context,
		platform.User,
		platform.GlobalWorkItemFilter,
	) (platform.GlobalWorkItemPage, error)
	ListGlobalPullRequests(
		context.Context,
		platform.User,
		platform.GlobalWorkItemFilter,
	) (platform.GlobalWorkItemPage, error)
}

func WithGlobalWorkItems(store GlobalWorkItemStore) Option {
	return func(api *API) {
		api.globalWorkItems = store
	}
}

func (api *API) globalIssues(writer http.ResponseWriter, request *http.Request) {
	api.globalWorkItemPage(writer, request, platform.WorkItemKindIssue)
}

func (api *API) globalPullRequests(writer http.ResponseWriter, request *http.Request) {
	api.globalWorkItemPage(writer, request, platform.WorkItemKindPullRequest)
}

func (api *API) globalWorkItemPage(writer http.ResponseWriter, request *http.Request, kind string) {
	if api.globalWorkItems == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "work_items_unavailable",
			"The cross-repository work item service is not configured")
		return
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	filter, err := globalWorkItemFilter(request.URL.Query(), kind)
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_work_item_filter", err.Error())
		return
	}
	var page platform.GlobalWorkItemPage
	if kind == platform.WorkItemKindIssue {
		page, err = api.globalWorkItems.ListGlobalIssues(request.Context(), actor, filter)
	} else {
		page, err = api.globalWorkItems.ListGlobalPullRequests(request.Context(), actor, filter)
	}
	if errors.Is(err, platform.ErrInvalidInput) {
		writeProblem(writer, http.StatusBadRequest, "invalid_work_item_filter",
			"The work item filter is invalid")
		return
	}
	if err != nil {
		api.internalError(writer, request, "list cross-repository work items", err)
		return
	}
	writeJSON(writer, http.StatusOK, page)
}

func globalWorkItemFilter(values url.Values, kind string) (platform.GlobalWorkItemFilter, error) {
	allowed := map[string]bool{"state": true, "scope": true, "q": true, "cursor": true, "limit": true}
	for name, entries := range values {
		if !allowed[name] || len(entries) != 1 {
			return platform.GlobalWorkItemFilter{}, errors.New("the work item query is invalid")
		}
	}
	state := strings.TrimSpace(values.Get("state"))
	if state == "" {
		state = "open"
	}
	scope := strings.TrimSpace(values.Get("scope"))
	if scope == "" {
		scope = "involved"
	}
	limit, err := queryLimit(values.Get("limit"), 25, 100)
	if err != nil {
		return platform.GlobalWorkItemFilter{}, errors.New("the work item limit is invalid")
	}
	filter := platform.GlobalWorkItemFilter{
		State:  state,
		Scope:  scope,
		Query:  strings.TrimSpace(values.Get("q")),
		Cursor: strings.TrimSpace(values.Get("cursor")),
		Limit:  limit,
	}
	if !validGlobalWorkItemState(kind, state) || !validGlobalWorkItemScope(kind, scope) ||
		len([]rune(filter.Query)) > 160 || len(filter.Cursor) > 1024 {
		return platform.GlobalWorkItemFilter{}, errors.New("the work item filter is invalid")
	}
	return filter, nil
}

func validGlobalWorkItemState(kind string, state string) bool {
	if state == "all" || state == "open" || state == "closed" {
		return true
	}
	return kind == platform.WorkItemKindPullRequest && state == "merged"
}

func validGlobalWorkItemScope(kind string, scope string) bool {
	if scope == "all" || scope == "involved" || scope == "created" || scope == "assigned" {
		return true
	}
	return kind == platform.WorkItemKindPullRequest && scope == "review_requested"
}
