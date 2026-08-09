package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/runner"
)

type ActionsStore interface {
	RepositoryForActions(
		ctx context.Context,
		owner string,
		repository string,
		actorID string,
	) (runner.RepositoryAccess, error)
	ListWorkflows(
		ctx context.Context,
		owner string,
		repository string,
		actorID string,
		page runner.PageRequest,
	) (runner.WorkflowPage, error)
	ListActionRuns(
		ctx context.Context,
		owner string,
		repository string,
		actorID string,
		filter runner.RunFilter,
	) (runner.RunPage, error)
	ActionRunDetail(
		ctx context.Context,
		owner string,
		repository string,
		runNumber int64,
		actorID string,
	) (runner.RunDetail, error)
	DispatchWorkflow(
		ctx context.Context,
		access runner.RepositoryAccess,
		workflowRef string,
		branch string,
		revision string,
		payload []byte,
		actorID string,
	) (runner.RunRecord, error)
	CancelActionRun(
		ctx context.Context,
		access runner.RepositoryAccess,
		runNumber int64,
		actorID string,
	) (runner.RunRecord, error)
	RerunActionRun(
		ctx context.Context,
		access runner.RepositoryAccess,
		runNumber int64,
		actorID string,
	) (runner.RunRecord, error)
	OpenActionJobLog(
		ctx context.Context,
		owner string,
		repository string,
		jobID string,
		actorID string,
	) (runner.FileDownload, error)
	OpenActionArtifact(
		ctx context.Context,
		owner string,
		repository string,
		artifactID string,
		actorID string,
	) (runner.FileDownload, error)
}

func WithActions(store ActionsStore) Option {
	return func(api *API) { api.actions = store }
}

func (api *API) listActionWorkflows(writer http.ResponseWriter, request *http.Request) {
	if api.actions == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "actions_unavailable", "Actions is not configured")
		return
	}
	actorID, ok := api.optionalActor(writer, request)
	if !ok {
		return
	}
	owner := request.PathValue("owner")
	repository := request.PathValue("repository")
	access, err := api.actions.RepositoryForActions(request.Context(), owner, repository, actorID)
	if err != nil {
		api.actionsError(writer, request, "check Actions read permission", err)
		return
	}
	page, err := actionPageRequest(request)
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "actions_invalid_pagination", err.Error())
		return
	}
	workflowPage, err := api.actions.ListWorkflows(request.Context(), owner,
		repository, actorID, page)
	if err != nil {
		api.actionsError(writer, request, "list Actions workflows", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"workflows":  workflowPage.Workflows,
		"totalCount": workflowPage.Total,
		"page":       page.Page,
		"perPage":    page.PerPage,
		"hasMore":    workflowPage.HasMore,
		"canWrite":   access.CanWrite,
	})
}

func (api *API) actionRuns(writer http.ResponseWriter, request *http.Request) {
	if api.actions == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "actions_unavailable", "Actions is not configured")
		return
	}
	actorID, ok := api.optionalActor(writer, request)
	if !ok {
		return
	}
	filter, err := actionRunFilter(request)
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "actions_invalid_filter", err.Error())
		return
	}
	runPage, err := api.actions.ListActionRuns(request.Context(), request.PathValue("owner"),
		request.PathValue("repository"), actorID, filter)
	if err != nil {
		api.actionsError(writer, request, "list Actions runs", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"totalCount": runPage.Total,
		"runs":       runPage.Runs,
		"page":       filter.Page,
		"perPage":    filter.PerPage,
		"hasMore":    runPage.HasMore,
	})
}

func (api *API) actionRunDetail(writer http.ResponseWriter, request *http.Request) {
	if api.actions == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "actions_unavailable", "Actions is not configured")
		return
	}
	actorID, ok := api.optionalActor(writer, request)
	if !ok {
		return
	}
	runNumber, err := parseActionRunNumber(request)
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_run_number", err.Error())
		return
	}
	detail, err := api.actions.ActionRunDetail(request.Context(), request.PathValue("owner"),
		request.PathValue("repository"), runNumber, actorID)
	if err != nil {
		api.actionsError(writer, request, "get Actions run", err)
		return
	}
	writeJSON(writer, http.StatusOK, detail)
}

func (api *API) dispatchActionWorkflow(writer http.ResponseWriter, request *http.Request) {
	if api.actions == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "actions_unavailable", "Actions is not configured")
		return
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input struct {
		Ref    string            `json:"ref"`
		Branch string            `json:"branch"`
		Inputs map[string]string `json:"inputs"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	ref := input.Ref
	if strings.TrimSpace(ref) == "" {
		ref = input.Branch
	}
	branch, err := actionBranchRef(ref)
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_ref", err.Error())
		return
	}
	owner := request.PathValue("owner")
	repositoryName := request.PathValue("repository")
	access, err := api.actions.RepositoryForActions(request.Context(), owner, repositoryName, actor.ID)
	if err != nil {
		api.actionsError(writer, request, "check workflow dispatch permission", err)
		return
	}
	branches, err := api.lore.Branches(request.Context(), loreRepositoryRef(access), api.loreIdentity)
	if err != nil {
		writeProblem(writer, http.StatusBadGateway, "lore_unavailable", "The Lore branch could not be resolved")
		return
	}
	revision, found := latestRevision(branches, branch)
	if !found {
		writeProblem(writer, http.StatusBadRequest, "branch_not_found", "The requested Lore branch does not exist")
		return
	}
	inputs := input.Inputs
	if inputs == nil {
		inputs = map[string]string{}
	}
	payload, err := json.Marshal(map[string]any{
		"ref":        "refs/heads/" + branch,
		"after":      revision,
		"repository": map[string]string{"name": access.Slug, "full_name": access.Owner + "/" + access.Slug},
		"workflow":   request.PathValue("workflow"),
		"inputs":     inputs,
	})
	if err != nil {
		api.internalError(writer, request, "encode workflow dispatch event", err)
		return
	}
	run, err := api.actions.DispatchWorkflow(request.Context(), access, request.PathValue("workflow"), branch,
		revision, payload, actor.ID)
	if err != nil {
		api.actionsError(writer, request, "dispatch Actions workflow", err)
		return
	}
	writer.Header().Set("Location", actionRunLocation(owner, repositoryName, run.RunNumber))
	writeJSON(writer, http.StatusCreated, run)
}

func (api *API) cancelActionRun(writer http.ResponseWriter, request *http.Request) {
	api.mutateActionRun(writer, request, false)
}

func (api *API) rerunActionRun(writer http.ResponseWriter, request *http.Request) {
	api.mutateActionRun(writer, request, true)
}

func (api *API) mutateActionRun(writer http.ResponseWriter, request *http.Request, rerun bool) {
	if api.actions == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "actions_unavailable", "Actions is not configured")
		return
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	runNumber, err := parseActionRunNumber(request)
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_run_number", err.Error())
		return
	}
	owner := request.PathValue("owner")
	repositoryName := request.PathValue("repository")
	access, err := api.actions.RepositoryForActions(request.Context(), owner, repositoryName, actor.ID)
	if err != nil {
		api.actionsError(writer, request, "check Actions write permission", err)
		return
	}
	var run runner.RunRecord
	if rerun {
		run, err = api.actions.RerunActionRun(request.Context(), access, runNumber, actor.ID)
	} else {
		run, err = api.actions.CancelActionRun(request.Context(), access, runNumber, actor.ID)
	}
	if err != nil {
		operation := "cancel Actions run"
		if rerun {
			operation = "rerun Actions run"
		}
		api.actionsError(writer, request, operation, err)
		return
	}
	writer.Header().Set("Location", actionRunLocation(owner, repositoryName, run.RunNumber))
	writeJSON(writer, http.StatusOK, run)
}

func (api *API) actionJobLog(writer http.ResponseWriter, request *http.Request) {
	if api.actions == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "actions_unavailable", "Actions is not configured")
		return
	}
	actorID, ok := api.optionalActor(writer, request)
	if !ok {
		return
	}
	download, err := api.actions.OpenActionJobLog(request.Context(), request.PathValue("owner"),
		request.PathValue("repository"), request.PathValue("jobID"), actorID)
	if err != nil {
		api.actionsError(writer, request, "open Actions job log", err)
		return
	}
	defer func() { _ = download.File.Close() }()
	serveActionDownload(writer, request, download)
}

func (api *API) actionArtifact(writer http.ResponseWriter, request *http.Request) {
	if api.actions == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "actions_unavailable", "Actions is not configured")
		return
	}
	actorID, ok := api.optionalActor(writer, request)
	if !ok {
		return
	}
	download, err := api.actions.OpenActionArtifact(request.Context(), request.PathValue("owner"),
		request.PathValue("repository"), request.PathValue("artifactID"), actorID)
	if err != nil {
		api.actionsError(writer, request, "open Actions artifact", err)
		return
	}
	defer func() { _ = download.File.Close() }()
	serveActionDownload(writer, request, download)
}

func actionRunFilter(request *http.Request) (runner.RunFilter, error) {
	query := request.URL.Query()
	filter := runner.RunFilter{
		EventName: strings.TrimSpace(query.Get("event")),
		Branch:    strings.TrimSpace(query.Get("branch")),
		Status:    strings.TrimSpace(query.Get("status")),
		Page:      1,
		PerPage:   30,
	}
	if raw := query.Get("page"); raw != "" {
		page, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || page < 1 {
			return runner.RunFilter{}, errors.New("page must be a positive integer")
		}
		filter.Page = page
	}
	if raw := query.Get("per_page"); raw != "" {
		perPage, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || perPage < 1 || perPage > 100 {
			return runner.RunFilter{}, errors.New("per_page must be between 1 and 100")
		}
		filter.PerPage = perPage
	}
	if filter.Status != "" && filter.Status != "queued" && filter.Status != "in_progress" &&
		filter.Status != "completed" && filter.Status != "cancelled" {
		return runner.RunFilter{}, errors.New("status is not supported")
	}
	return filter, nil
}

func actionPageRequest(request *http.Request) (runner.PageRequest, error) {
	filter, err := actionRunFilter(request)
	if err != nil {
		return runner.PageRequest{}, err
	}
	return runner.PageRequest{Page: filter.Page, PerPage: filter.PerPage}, nil
}

func parseActionRunNumber(request *http.Request) (int64, error) {
	value := request.PathValue("runNumber")
	runNumber, err := strconv.ParseInt(value, 10, 64)
	if err != nil || runNumber < 1 {
		return 0, errors.New("run number must be a positive integer")
	}
	return runNumber, nil
}

func actionBranchRef(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "refs/heads/") {
		ref = strings.TrimPrefix(ref, "refs/heads/")
	} else if strings.HasPrefix(ref, "refs/") {
		return "", errors.New("only branch refs are supported")
	}
	if ref == "" || ref == "." || ref == ".." || strings.ContainsAny(ref, "\\\x00\r\n") ||
		strings.HasPrefix(ref, "/") || strings.HasSuffix(ref, "/") {
		return "", errors.New("ref must name a Lore branch")
	}
	return ref, nil
}

func loreRepositoryRef(access runner.RepositoryAccess) loreclient.RepositoryRef {
	return loreclient.RepositoryRef{CacheKey: access.ID, URL: access.LoreURL}
}

func serveActionDownload(writer http.ResponseWriter, request *http.Request, download runner.FileDownload) {
	writer.Header().Set("Content-Type", download.ContentType)
	writer.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{
		"filename": download.Name,
	}))
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(writer, request, download.Name, time.Time{}, download.File)
}

func actionRunLocation(owner string, repository string, runNumber int64) string {
	return "/api/v1/repositories/" + url.PathEscape(owner) + "/" + url.PathEscape(repository) +
		"/actions/runs/" + strconv.FormatInt(runNumber, 10)
}

func (api *API) optionalActor(writer http.ResponseWriter, request *http.Request) (string, bool) {
	actor, ok := api.ResolveOptionalActor(writer, request)
	if !ok {
		return "", false
	}
	if actor == nil {
		return "", true
	}
	return actor.ID, true
}

func (api *API) actionsError(writer http.ResponseWriter, request *http.Request, operation string, err error) {
	switch {
	case errors.Is(err, runner.ErrActionNotFound):
		writeProblem(writer, http.StatusNotFound, "actions_not_found", "The Actions resource was not found")
	case errors.Is(err, runner.ErrActionForbidden):
		writeProblem(writer, http.StatusForbidden, "actions_forbidden", "The repository permission is insufficient")
	case errors.Is(err, runner.ErrActionConflict):
		writeProblem(writer, http.StatusConflict, "actions_conflict",
			"The Actions resource cannot be changed in its current state")
	case errors.Is(err, runner.ErrActionInvalid):
		writeProblem(writer, http.StatusBadRequest, "actions_invalid_request", "The Actions request is invalid")
	default:
		api.internalError(writer, request, operation, err)
	}
}
