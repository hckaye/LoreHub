package reviewthreads

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type createThreadRequest struct {
	Path                 string `json:"path"`
	Side                 Side   `json:"side"`
	LineNumber           int    `json:"lineNumber"`
	Body                 string `json:"body"`
	ExpectedBaseRevision string `json:"expectedBaseRevision"`
	ExpectedHeadRevision string `json:"expectedHeadRevision"`
}

type commentRequest struct {
	Body            string `json:"body"`
	ExpectedVersion int    `json:"expectedVersion"`
}

type updateThreadRequest struct {
	Resolved        bool `json:"resolved"`
	ExpectedVersion int  `json:"expectedVersion"`
}

func (api *API) listThreads(writer http.ResponseWriter, request *http.Request) {
	repository, actor, ok := api.visibleRepository(writer, request)
	if !ok {
		return
	}
	number, ok := requestNumber(writer, request)
	if !ok {
		return
	}
	threads, err := api.store.List(request.Context(), repository.ID, number)
	if err != nil {
		api.storeError(writer, request, "list review threads", err)
		return
	}
	if actor != nil {
		access, err := api.repositories.RepositoryPermission(request.Context(), *actor, repository)
		if err != nil {
			api.storeError(writer, request, "compute review permissions", err)
			return
		}
		for index := range threads {
			threads[index].ViewerCanResolve = repository.ArchivedAt == nil &&
				(threads[index].createdByID == actor.ID || threads[index].mergeAuthorID == actor.ID ||
					access.AtLeast(collab.PermWrite))
			for commentIndex := range threads[index].Comments {
				comment := &threads[index].Comments[commentIndex]
				comment.ViewerCanUpdate = repository.ArchivedAt == nil && !comment.Deleted &&
					(comment.authorID == actor.ID || access.AtLeast(collab.PermWrite))
			}
		}
	}
	writeJSON(writer, http.StatusOK, ThreadList{Threads: threads})
}

func (api *API) createThread(writer http.ResponseWriter, request *http.Request) {
	repository, actor, ok := api.mutationRepository(writer, request)
	if !ok {
		return
	}
	number, ok := requestNumber(writer, request)
	if !ok {
		return
	}
	var body createThreadRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	input, err := normalizeCreate(CreateInput{
		Path: body.Path, Side: body.Side, LineNumber: body.LineNumber, Body: body.Body,
		ExpectedBaseRevision: body.ExpectedBaseRevision,
		ExpectedHeadRevision: body.ExpectedHeadRevision,
	})
	if err != nil {
		api.storeError(writer, request, "validate review thread", err)
		return
	}
	mergeRequest, err := api.repositories.GetMergeRequest(request.Context(), repository.ID, number)
	if err != nil {
		api.storeError(writer, request, "get pull request for review", err)
		return
	}
	if mergeRequest.State != "open" || mergeRequest.TargetRevision != input.ExpectedBaseRevision ||
		mergeRequest.SourceRevision != input.ExpectedHeadRevision {
		api.storeError(writer, request, "validate pull request revisions", platform.ErrConflict)
		return
	}
	if api.code == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "lore_unavailable", "Lore review validation is unavailable")
		return
	}
	credential, err := api.credential(request, repository, actor)
	if err != nil {
		api.storeError(writer, request, "get Lore review credential", err)
		return
	}
	diff, err := api.code.RevisionDiff(
		request.Context(), loreReference(repository), input.ExpectedBaseRevision,
		input.ExpectedHeadRevision, []string{input.Path}, credential, maxDiffFiles, maxDiffPatchBytes,
	)
	if err != nil {
		api.storeError(writer, request, "validate Lore review line", err)
		return
	}
	if diff.Source != input.ExpectedBaseRevision || diff.Target != input.ExpectedHeadRevision {
		api.storeError(writer, request, "validate Lore review revisions", platform.ErrConflict)
		return
	}
	input.LineContent, err = lineFromDiff(diff, input.Path, input.Side, input.LineNumber)
	if err != nil {
		api.storeError(writer, request, "validate Lore review line", err)
		return
	}
	thread, err := api.store.Create(request.Context(), actor, repositoryRef(repository), number, input)
	if err != nil {
		api.storeError(writer, request, "create review thread", err)
		return
	}
	writer.Header().Set("Location", request.URL.Path+"/"+thread.ID)
	writeJSON(writer, http.StatusCreated, thread)
}

func (api *API) reply(writer http.ResponseWriter, request *http.Request) {
	repository, actor, ok := api.mutationRepository(writer, request)
	if !ok {
		return
	}
	number, ok := requestNumber(writer, request)
	if !ok {
		return
	}
	var body commentRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	comment, err := api.store.Reply(
		request.Context(), actor, repositoryRef(repository), number,
		strings.TrimSpace(request.PathValue("threadID")), body.Body,
	)
	if err != nil {
		api.storeError(writer, request, "reply to review thread", err)
		return
	}
	writeJSON(writer, http.StatusCreated, comment)
}

func (api *API) updateComment(writer http.ResponseWriter, request *http.Request) {
	repository, actor, ok := api.mutationRepository(writer, request)
	if !ok {
		return
	}
	number, ok := requestNumber(writer, request)
	if !ok {
		return
	}
	var body commentRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	comment, err := api.store.UpdateComment(
		request.Context(), actor, repositoryRef(repository), number,
		strings.TrimSpace(request.PathValue("threadID")),
		strings.TrimSpace(request.PathValue("commentID")), body.Body, body.ExpectedVersion,
	)
	if err != nil {
		api.storeError(writer, request, "update review comment", err)
		return
	}
	writeJSON(writer, http.StatusOK, comment)
}

func (api *API) deleteComment(writer http.ResponseWriter, request *http.Request) {
	repository, actor, ok := api.mutationRepository(writer, request)
	if !ok {
		return
	}
	number, ok := requestNumber(writer, request)
	if !ok {
		return
	}
	var body commentRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	if strings.TrimSpace(body.Body) != "" {
		api.storeError(writer, request, "validate review comment deletion", invalid("body must be omitted"))
		return
	}
	err := api.store.DeleteComment(
		request.Context(), actor, repositoryRef(repository), number,
		strings.TrimSpace(request.PathValue("threadID")),
		strings.TrimSpace(request.PathValue("commentID")), body.ExpectedVersion,
	)
	if err != nil {
		api.storeError(writer, request, "delete review comment", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) updateThread(writer http.ResponseWriter, request *http.Request) {
	repository, actor, ok := api.mutationRepository(writer, request)
	if !ok {
		return
	}
	number, ok := requestNumber(writer, request)
	if !ok {
		return
	}
	var body updateThreadRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	thread, err := api.store.SetResolved(
		request.Context(), actor, repositoryRef(repository), number,
		strings.TrimSpace(request.PathValue("threadID")), body.Resolved, body.ExpectedVersion,
	)
	if err != nil {
		api.storeError(writer, request, "update review thread", err)
		return
	}
	writeJSON(writer, http.StatusOK, thread)
}

func requestNumber(writer http.ResponseWriter, request *http.Request) (int64, bool) {
	number, err := strconv.ParseInt(request.PathValue("number"), 10, 64)
	if err != nil || number < 1 {
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return 0, false
	}
	return number, true
}
