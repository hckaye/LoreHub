package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/lorehub/lorehub/services/api/internal/platform"
	"github.com/lorehub/lorehub/services/api/internal/runner"
)

type ActionsExecutionContextStore interface {
	ListExecutionContextEntries(
		context.Context,
		string,
		runner.ExecutionContextScope,
	) ([]runner.ExecutionContextEntry, error)
	UpsertVariable(
		context.Context,
		runner.ExecutionContextScope,
		string,
		string,
		string,
	) (runner.ExecutionContextEntry, error)
	UpsertSecret(
		context.Context,
		runner.ExecutionContextScope,
		string,
		string,
		string,
	) (runner.ExecutionContextEntry, error)
	DeleteExecutionContextEntry(
		context.Context,
		string,
		runner.ExecutionContextScope,
		string,
		bool,
	) error
}

type actionsSettingInput struct {
	Value *string `json:"value"`
}

func (api *API) listOrganizationActionsSettings(writer http.ResponseWriter, request *http.Request) {
	actor, scope, ok := api.organizationActionsSettingsScope(writer, request)
	if !ok {
		return
	}
	entries, err := api.actionsExecutionContext.ListExecutionContextEntries(
		request.Context(), actor.ID, scope,
	)
	if err != nil {
		api.actionsContextError(writer, request, "list organization Actions settings", err)
		return
	}
	sanitizeActionsContextEntries(entries)
	writeJSON(writer, http.StatusOK, map[string]any{"entries": entries})
}

func (api *API) upsertOrganizationActionsSetting(writer http.ResponseWriter, request *http.Request) {
	actor, scope, ok := api.organizationActionsSettingsScope(writer, request)
	if !ok {
		return
	}
	api.upsertActionsSetting(writer, request, actor.ID, scope)
}

func (api *API) deleteOrganizationActionsSetting(writer http.ResponseWriter, request *http.Request) {
	actor, scope, ok := api.organizationActionsSettingsScope(writer, request)
	if !ok {
		return
	}
	api.deleteActionsSetting(writer, request, actor.ID, scope)
}

func (api *API) listRepositoryActionsSettings(writer http.ResponseWriter, request *http.Request) {
	actor, access, ok := api.repositoryActionsSettingsAccess(writer, request)
	if !ok {
		return
	}
	scope, err := repositoryActionsSettingsScope(request, access)
	if err != nil {
		api.actionsContextError(writer, request, "validate repository Actions settings scope", err)
		return
	}
	entries, err := api.actionsExecutionContext.ListExecutionContextEntries(
		request.Context(), actor.ID, scope,
	)
	if err != nil {
		api.actionsContextError(writer, request, "list repository Actions settings", err)
		return
	}
	sanitizeActionsContextEntries(entries)
	writeJSON(writer, http.StatusOK, map[string]any{"entries": entries})
}

func (api *API) upsertRepositoryActionsSetting(writer http.ResponseWriter, request *http.Request) {
	actor, access, ok := api.repositoryActionsSettingsAccess(writer, request)
	if !ok {
		return
	}
	scope, err := repositoryActionsSettingsScope(request, access)
	if err != nil {
		api.actionsContextError(writer, request, "validate repository Actions settings scope", err)
		return
	}
	api.upsertActionsSetting(writer, request, actor.ID, scope)
}

func (api *API) deleteRepositoryActionsSetting(writer http.ResponseWriter, request *http.Request) {
	actor, access, ok := api.repositoryActionsSettingsAccess(writer, request)
	if !ok {
		return
	}
	scope, err := repositoryActionsSettingsScope(request, access)
	if err != nil {
		api.actionsContextError(writer, request, "validate repository Actions settings scope", err)
		return
	}
	api.deleteActionsSetting(writer, request, actor.ID, scope)
}

func (api *API) upsertActionsSetting(
	writer http.ResponseWriter,
	request *http.Request,
	actorID string,
	scope runner.ExecutionContextScope,
) {
	secret, err := actionsSettingSecret(request.PathValue("valueKind"))
	if err != nil {
		api.actionsContextError(writer, request, "validate Actions setting kind", err)
		return
	}
	var input actionsSettingInput
	if !decodeJSON(writer, request, &input) {
		return
	}
	if input.Value == nil {
		writeProblem(writer, http.StatusBadRequest, "actions_context_invalid", "The value field is required")
		return
	}
	var entry runner.ExecutionContextEntry
	if secret {
		entry, err = api.actionsExecutionContext.UpsertSecret(
			request.Context(), scope, request.PathValue("name"), *input.Value, actorID,
		)
	} else {
		entry, err = api.actionsExecutionContext.UpsertVariable(
			request.Context(), scope, request.PathValue("name"), *input.Value, actorID,
		)
	}
	if err != nil {
		api.actionsContextError(writer, request, "upsert Actions setting", err)
		return
	}
	if entry.Secret || secret {
		entry.Value = ""
	}
	writeJSON(writer, http.StatusOK, entry)
}

func (api *API) deleteActionsSetting(
	writer http.ResponseWriter,
	request *http.Request,
	actorID string,
	scope runner.ExecutionContextScope,
) {
	secret, err := actionsSettingSecret(request.PathValue("valueKind"))
	if err != nil {
		api.actionsContextError(writer, request, "validate Actions setting kind", err)
		return
	}
	err = api.actionsExecutionContext.DeleteExecutionContextEntry(
		request.Context(), actorID, scope, request.PathValue("name"), secret,
	)
	if err != nil {
		api.actionsContextError(writer, request, "delete Actions setting", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) organizationActionsSettingsScope(
	writer http.ResponseWriter,
	request *http.Request,
) (platform.User, runner.ExecutionContextScope, bool) {
	if api.actionsExecutionContext == nil || api.identityStore == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "actions_context_unavailable",
			"Actions settings are not configured")
		return platform.User{}, runner.ExecutionContextScope{}, false
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return platform.User{}, runner.ExecutionContextScope{}, false
	}
	organization, err := api.identityStore.Organization(
		request.Context(), &actor, request.PathValue("organization"),
	)
	if err != nil {
		api.platformError(writer, request, "find Actions settings organization", err)
		return platform.User{}, runner.ExecutionContextScope{}, false
	}
	return actor, runner.ExecutionContextScope{
		Kind:           "organization",
		OrganizationID: organization.ID,
	}, true
}

func (api *API) repositoryActionsSettingsAccess(
	writer http.ResponseWriter,
	request *http.Request,
) (platform.User, runner.RepositoryAccess, bool) {
	if api.actionsExecutionContext == nil || api.actions == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "actions_context_unavailable",
			"Actions settings are not configured")
		return platform.User{}, runner.RepositoryAccess{}, false
	}
	actor, ok := api.actor(writer, request)
	if !ok {
		return platform.User{}, runner.RepositoryAccess{}, false
	}
	access, err := api.actions.RepositoryForActions(
		request.Context(), request.PathValue("owner"), request.PathValue("repository"), actor.ID,
	)
	if err != nil {
		api.actionsError(writer, request, "find Actions settings repository", err)
		return platform.User{}, runner.RepositoryAccess{}, false
	}
	return actor, access, true
}

func repositoryActionsSettingsScope(
	request *http.Request,
	access runner.RepositoryAccess,
) (runner.ExecutionContextScope, error) {
	scopeKind := request.PathValue("scopeKind")
	if scopeKind == "" {
		scopeKind = request.URL.Query().Get("scopeKind")
	}
	if scopeKind == "" {
		scopeKind = "repository"
	}
	scope := runner.ExecutionContextScope{
		Kind:           scopeKind,
		OrganizationID: access.OrganizationID,
		RepositoryID:   access.ID,
	}
	switch scopeKind {
	case "repository":
		if request.URL.Query().Has("environment") {
			return runner.ExecutionContextScope{}, invalidActionsContext("repository scope cannot name an environment")
		}
	case "environment":
		environments, present := request.URL.Query()["environment"]
		if !present || len(environments) != 1 || environments[0] == "" {
			return runner.ExecutionContextScope{}, invalidActionsContext(
				"environment scope requires one environment query parameter",
			)
		}
		scope.Environment = environments[0]
	default:
		return runner.ExecutionContextScope{}, invalidActionsContext("scope kind must be repository or environment")
	}
	return scope, nil
}

func actionsSettingSecret(valueKind string) (bool, error) {
	switch valueKind {
	case "variable":
		return false, nil
	case "secret":
		return true, nil
	default:
		return false, invalidActionsContext("value kind must be variable or secret")
	}
}

func sanitizeActionsContextEntries(entries []runner.ExecutionContextEntry) {
	for index := range entries {
		if entries[index].Secret {
			entries[index].Value = ""
		}
	}
}

func invalidActionsContext(detail string) error {
	return fmt.Errorf("%w: %s", runner.ErrExecutionContextInvalid, detail)
}

func (api *API) actionsContextError(
	writer http.ResponseWriter,
	request *http.Request,
	operation string,
	err error,
) {
	switch {
	case errors.Is(err, runner.ErrExecutionContextInvalid):
		writeProblem(writer, http.StatusBadRequest, "actions_context_invalid", "The Actions setting is invalid")
	case errors.Is(err, runner.ErrExecutionContextUnauthorized):
		writeProblem(writer, http.StatusForbidden, "actions_context_forbidden",
			"The repository permission is insufficient")
	case errors.Is(err, runner.ErrExecutionContextEntryNotFound):
		writeProblem(writer, http.StatusNotFound, "actions_context_not_found",
			"The Actions setting was not found")
	default:
		api.internalError(writer, request, operation, err)
	}
}
