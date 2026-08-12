package collab

import (
	"errors"
	"net/http"
	"strings"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func (api *API) listReviewRequests(writer http.ResponseWriter, request *http.Request) {
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
	if !api.requireReviewRequestStore(writer) {
		return
	}
	items, err := api.reviewRequests.ListReviewRequests(requestContext(request), repo.ID, number)
	if err != nil {
		storeError(writer, request, "list review requests", err, api.logger)
		return
	}
	summary := ReviewRequestSummary{Items: items}
	if actor != nil {
		mergeRequest, err := api.store.GetMergeRequest(requestContext(request), repo.ID, number)
		if err != nil {
			storeError(writer, request, "find pull request for review requests", err, api.logger)
			return
		}
		access, ok := api.permission(writer, request, *actor, repo)
		if !ok {
			return
		}
		summary.ViewerCanManage = repo.ArchivedAt == nil && mergeRequest.State == "open" &&
			(access.AtLeast(PermTriage) || mergeRequest.AuthorID == actor.ID && access.AtLeast(PermRead))
	}
	writeJSON(writer, http.StatusOK, summary)
}

func (api *API) listReviewCandidates(writer http.ResponseWriter, request *http.Request) {
	actor, repo, ok := api.requireMutationActor(writer, request)
	if !ok {
		return
	}
	number, ok := parseNumber(writer, request.PathValue("number"))
	if !ok || !api.requireReviewRequestStore(writer) {
		return
	}
	query := strings.TrimSpace(request.URL.Query().Get("query"))
	if !validAssigneeValue(query, true) {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "The reviewer query is invalid")
		return
	}
	candidates, err := api.reviewRequests.ListReviewCandidates(
		requestContext(request), actor, repo.ID, number, query,
	)
	if err != nil {
		storeError(writer, request, "list review candidates", err, api.logger)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"items": candidates})
}

func (api *API) putUserReviewRequest(writer http.ResponseWriter, request *http.Request) {
	api.putReviewRequest(writer, request, "user")
}

func (api *API) putTeamReviewRequest(writer http.ResponseWriter, request *http.Request) {
	api.putReviewRequest(writer, request, "team")
}

func (api *API) putReviewRequest(writer http.ResponseWriter, request *http.Request, kind string) {
	actor, repo, number, slug, ok := api.reviewRequestMutationContext(writer, request, kind)
	if !ok {
		return
	}
	var reviewRequest ReviewRequest
	var created bool
	var err error
	if kind == "user" {
		reviewRequest, created, err = api.reviewRequests.RequestUserReview(
			requestContext(request), actor, repo.ID, number, slug,
		)
	} else {
		reviewRequest, created, err = api.reviewRequests.RequestTeamReview(
			requestContext(request), actor, repo.ID, number, slug,
		)
	}
	if errors.Is(err, ErrReviewRequestLimit) {
		writeProblem(writer, http.StatusConflict, "review_request_limit", err.Error())
		return
	}
	if err != nil {
		storeError(writer, request, "create review request", err, api.logger)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
		writeLocation(writer, request, reviewRequest.ID)
	}
	writeJSON(writer, status, reviewRequest)
}

func (api *API) deleteUserReviewRequest(writer http.ResponseWriter, request *http.Request) {
	api.deleteReviewRequest(writer, request, "user")
}

func (api *API) deleteTeamReviewRequest(writer http.ResponseWriter, request *http.Request) {
	api.deleteReviewRequest(writer, request, "team")
}

func (api *API) deleteReviewRequest(writer http.ResponseWriter, request *http.Request, kind string) {
	actor, repo, number, slug, ok := api.reviewRequestMutationContext(writer, request, kind)
	if !ok {
		return
	}
	var err error
	if kind == "user" {
		err = api.reviewRequests.RemoveUserReviewRequest(
			requestContext(request), actor, repo.ID, number, slug,
		)
	} else {
		err = api.reviewRequests.RemoveTeamReviewRequest(
			requestContext(request), actor, repo.ID, number, slug,
		)
	}
	if err != nil {
		storeError(writer, request, "remove review request", err, api.logger)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) reviewRequestMutationContext(
	writer http.ResponseWriter,
	request *http.Request,
	kind string,
) (platform.User, Repository, int64, string, bool) {
	actor, repo, ok := api.requireMutationActor(writer, request)
	if !ok {
		return platform.User{}, Repository{}, 0, "", false
	}
	number, ok := parseNumber(writer, request.PathValue("number"))
	if !ok || !api.requireReviewRequestStore(writer) {
		return platform.User{}, Repository{}, 0, "", false
	}
	pathName := "username"
	if kind == "team" {
		pathName = "team"
	}
	slug := strings.TrimSpace(request.PathValue(pathName))
	if !validAssigneeValue(slug, false) {
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return platform.User{}, Repository{}, 0, "", false
	}
	return actor, repo, number, slug, true
}

func (api *API) requireReviewRequestStore(writer http.ResponseWriter) bool {
	if api.reviewRequests != nil {
		return true
	}
	writeProblem(writer, http.StatusServiceUnavailable, "review_requests_unavailable",
		"Review requests are unavailable")
	return false
}
