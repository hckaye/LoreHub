package collab

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func (api *API) getMergeRequestMetadata(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.optionalActor(writer, request)
	if !ok {
		return
	}
	repository, ok := api.lookup(writer, request, actor)
	if !ok {
		return
	}
	number, ok := parseNumber(writer, request.PathValue("number"))
	if !ok || !api.requireMergeRequestMetadataStore(writer) {
		return
	}
	metadata, err := api.metadata.GetMergeRequestMetadata(requestContext(request), repository.ID, number)
	if err != nil {
		storeError(writer, request, "get pull request metadata", err, api.logger)
		return
	}
	if actor != nil {
		access, ok := api.permission(writer, request, *actor, repository)
		if !ok {
			return
		}
		canManage := !repositoryReadOnly(repository) && access.AtLeast(PermTriage)
		metadata.ViewerCanManageLabels = canManage
		metadata.ViewerCanManageAssignees = canManage
		metadata.ViewerCanManageMilestone = canManage
	}
	writeJSON(writer, http.StatusOK, metadata)
}

func (api *API) putMergeRequestLabel(writer http.ResponseWriter, request *http.Request) {
	actor, repository, number, ok := api.mergeRequestMetadataMutationContext(writer, request)
	if !ok {
		return
	}
	labelID := strings.TrimSpace(request.PathValue("labelID"))
	if !validMetadataID(labelID) {
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return
	}
	label, applied, err := api.metadata.ApplyMergeRequestLabel(
		requestContext(request), actor, repository.ID, number, labelID,
	)
	if err != nil {
		storeError(writer, request, "apply pull request label", err, api.logger)
		return
	}
	status := http.StatusOK
	if applied {
		status = http.StatusCreated
	}
	writeJSON(writer, status, label)
}

func (api *API) deleteMergeRequestLabel(writer http.ResponseWriter, request *http.Request) {
	actor, repository, number, ok := api.mergeRequestMetadataMutationContext(writer, request)
	if !ok {
		return
	}
	labelID := strings.TrimSpace(request.PathValue("labelID"))
	if !validMetadataID(labelID) {
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return
	}
	if err := api.metadata.RemoveMergeRequestLabel(
		requestContext(request), actor, repository.ID, number, labelID,
	); err != nil {
		storeError(writer, request, "remove pull request label", err, api.logger)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) putMergeRequestAssignee(writer http.ResponseWriter, request *http.Request) {
	actor, repository, number, username, ok := api.mergeRequestAssigneeContext(writer, request)
	if !ok {
		return
	}
	assignee, assigned, err := api.metadata.AssignMergeRequestUser(
		requestContext(request), actor, repository.ID, number, username,
	)
	if errors.Is(err, ErrMergeRequestAssigneeLimit) {
		writeProblem(writer, http.StatusConflict, "assignee_limit", err.Error())
		return
	}
	if err != nil {
		storeError(writer, request, "assign pull request user", err, api.logger)
		return
	}
	status := http.StatusOK
	if assigned {
		status = http.StatusCreated
	}
	writeJSON(writer, status, assignee)
}

func (api *API) deleteMergeRequestAssignee(writer http.ResponseWriter, request *http.Request) {
	actor, repository, number, username, ok := api.mergeRequestAssigneeContext(writer, request)
	if !ok {
		return
	}
	if err := api.metadata.RemoveMergeRequestUser(
		requestContext(request), actor, repository.ID, number, username,
	); err != nil {
		storeError(writer, request, "remove pull request assignee", err, api.logger)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) putMergeRequestMilestone(writer http.ResponseWriter, request *http.Request) {
	actor, repository, number, ok := api.mergeRequestMetadataMutationContext(writer, request)
	if !ok {
		return
	}
	milestoneNumber, ok := parseNumber(writer, request.PathValue("milestoneNumber"))
	if !ok {
		return
	}
	milestone, changed, err := api.metadata.SetMergeRequestMilestone(
		requestContext(request), actor, repository.ID, number, &milestoneNumber,
	)
	if err != nil {
		storeError(writer, request, "set pull request milestone", err, api.logger)
		return
	}
	status := http.StatusOK
	if changed {
		status = http.StatusCreated
	}
	writeJSON(writer, status, milestone)
}

func (api *API) deleteMergeRequestMilestone(writer http.ResponseWriter, request *http.Request) {
	actor, repository, number, ok := api.mergeRequestMetadataMutationContext(writer, request)
	if !ok {
		return
	}
	if _, _, err := api.metadata.SetMergeRequestMilestone(
		requestContext(request), actor, repository.ID, number, nil,
	); err != nil {
		storeError(writer, request, "remove pull request milestone", err, api.logger)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) mergeRequestAssigneeContext(
	writer http.ResponseWriter,
	request *http.Request,
) (platform.User, Repository, int64, string, bool) {
	actor, repository, number, ok := api.mergeRequestMetadataMutationContext(writer, request)
	if !ok {
		return platform.User{}, Repository{}, 0, "", false
	}
	username := strings.TrimSpace(request.PathValue("username"))
	if !validAssigneeValue(username, false) {
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return platform.User{}, Repository{}, 0, "", false
	}
	return actor, repository, number, username, true
}

func (api *API) mergeRequestMetadataMutationContext(
	writer http.ResponseWriter,
	request *http.Request,
) (platform.User, Repository, int64, bool) {
	actor, repository, ok := api.requireMutationActor(writer, request)
	if !ok {
		return platform.User{}, Repository{}, 0, false
	}
	number, ok := parseNumber(writer, request.PathValue("number"))
	if !ok || !api.requireMergeRequestMetadataStore(writer) {
		return platform.User{}, Repository{}, 0, false
	}
	return actor, repository, number, true
}

func (api *API) requireMergeRequestMetadataStore(writer http.ResponseWriter) bool {
	if api.metadata != nil {
		return true
	}
	writeProblem(writer, http.StatusServiceUnavailable, "pull_request_metadata_unavailable",
		"Pull request metadata is unavailable")
	return false
}

func validMetadataID(value string) bool {
	_, err := uuid.Parse(value)
	return err == nil
}
