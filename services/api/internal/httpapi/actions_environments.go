package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/lorehub/lorehub/services/api/internal/runner"
)

type ActionsEnvironmentStore interface {
	ListEnvironments(
		context.Context,
		runner.RepositoryAccess,
		string,
	) ([]runner.EnvironmentRecord, error)
	UpsertEnvironment(
		context.Context,
		runner.RepositoryAccess,
		string,
		string,
		runner.EnvironmentInput,
	) (runner.EnvironmentRecord, error)
	DeleteEnvironment(context.Context, runner.RepositoryAccess, string, string) error
	ListDeployments(
		context.Context,
		runner.RepositoryAccess,
		string,
		int,
	) ([]runner.DeploymentRecord, error)
	ReviewDeployment(
		context.Context,
		runner.RepositoryAccess,
		string,
		string,
		bool,
	) (runner.DeploymentRecord, error)
}

type actionEnvironmentInput struct {
	WaitTimerMinutes  int      `json:"waitTimerMinutes"`
	PreventSelfReview *bool    `json:"preventSelfReview"`
	Reviewers         []string `json:"reviewers"`
}

type deploymentReviewInput struct {
	State string `json:"state"`
}

func (api *API) listActionEnvironments(writer http.ResponseWriter, request *http.Request) {
	actor, access, ok := api.actionEnvironmentAccess(writer, request, true)
	if !ok {
		return
	}
	environments, err := api.actionsEnvironments.ListEnvironments(request.Context(), access, actor)
	if err != nil {
		api.actionsError(writer, request, "list Actions environments", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"environments": environments})
}

func (api *API) upsertActionEnvironment(writer http.ResponseWriter, request *http.Request) {
	actor, access, ok := api.actionEnvironmentAccess(writer, request, true)
	if !ok {
		return
	}
	var input actionEnvironmentInput
	if !decodeJSON(writer, request, &input) {
		return
	}
	preventSelfReview := true
	if input.PreventSelfReview != nil {
		preventSelfReview = *input.PreventSelfReview
	}
	environment, err := api.actionsEnvironments.UpsertEnvironment(
		request.Context(),
		access,
		actor,
		request.PathValue("environment"),
		runner.EnvironmentInput{
			WaitTimerMinutes:  input.WaitTimerMinutes,
			PreventSelfReview: preventSelfReview,
			ReviewerUsernames: input.Reviewers,
		},
	)
	if err != nil {
		api.actionsError(writer, request, "save Actions environment", err)
		return
	}
	writeJSON(writer, http.StatusOK, environment)
}

func (api *API) deleteActionEnvironment(writer http.ResponseWriter, request *http.Request) {
	actor, access, ok := api.actionEnvironmentAccess(writer, request, true)
	if !ok {
		return
	}
	err := api.actionsEnvironments.DeleteEnvironment(
		request.Context(), access, actor, request.PathValue("environment"),
	)
	if err != nil {
		api.actionsError(writer, request, "delete Actions environment", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) listDeployments(writer http.ResponseWriter, request *http.Request) {
	actor, access, ok := api.actionEnvironmentAccess(writer, request, false)
	if !ok {
		return
	}
	limit := 50
	if value := request.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 100 {
			writeProblem(writer, http.StatusBadRequest, "actions_invalid_pagination", "limit must be from 1 to 100")
			return
		}
		limit = parsed
	}
	deployments, err := api.actionsEnvironments.ListDeployments(request.Context(), access, actor, limit)
	if err != nil {
		api.actionsError(writer, request, "list Actions deployments", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"deployments": deployments})
}

func (api *API) reviewDeployment(writer http.ResponseWriter, request *http.Request) {
	actor, access, ok := api.actionEnvironmentAccess(writer, request, true)
	if !ok {
		return
	}
	var input deploymentReviewInput
	if !decodeJSON(writer, request, &input) {
		return
	}
	if input.State != "approved" && input.State != "rejected" {
		writeProblem(writer, http.StatusBadRequest, "actions_invalid", "state must be approved or rejected")
		return
	}
	deployment, err := api.actionsEnvironments.ReviewDeployment(
		request.Context(), access, actor, request.PathValue("deploymentID"), input.State == "approved",
	)
	if err != nil {
		api.actionsError(writer, request, "review Actions deployment", err)
		return
	}
	writeJSON(writer, http.StatusOK, deployment)
}

func (api *API) actionEnvironmentAccess(
	writer http.ResponseWriter,
	request *http.Request,
	requireActor bool,
) (string, runner.RepositoryAccess, bool) {
	if api.actions == nil || api.actionsEnvironments == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "actions_unavailable", "Actions is not configured")
		return "", runner.RepositoryAccess{}, false
	}
	actor := ""
	if requireActor {
		user, ok := api.actor(writer, request)
		if !ok {
			return "", runner.RepositoryAccess{}, false
		}
		actor = user.ID
	} else {
		var ok bool
		actor, ok = api.optionalActor(writer, request)
		if !ok {
			return "", runner.RepositoryAccess{}, false
		}
	}
	access, err := api.actions.RepositoryForActions(
		request.Context(), request.PathValue("owner"), request.PathValue("repository"), actor,
	)
	if err != nil {
		api.actionsError(writer, request, "find Actions environment repository", err)
		return "", runner.RepositoryAccess{}, false
	}
	return actor, access, true
}
