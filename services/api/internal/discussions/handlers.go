package discussions

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type discussionRequest struct {
	Category string  `json:"category"`
	Title    string  `json:"title"`
	Body     string  `json:"body"`
	State    *string `json:"state,omitempty"`
	Locked   *bool   `json:"locked,omitempty"`
	Pinned   *bool   `json:"pinned,omitempty"`
}

type commentRequest struct {
	ParentID *string `json:"parentId,omitempty"`
	Body     string  `json:"body"`
}

type categoryRequest struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Format      string `json:"format"`
}

func (api *API) listDiscussions(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.optionalRepository(writer, request)
	if !ok {
		return
	}
	filter, ok := listFilter(writer, request)
	if !ok {
		return
	}
	viewerID := ""
	if actor != nil {
		viewerID = actor.ID
	}
	page, err := api.store.List(request.Context(), repository.ID, viewerID, filter)
	if err != nil {
		api.storeError(writer, request, "list discussions", err)
		return
	}
	access, ok := api.access(writer, request, actor, repository)
	if !ok {
		return
	}
	decoratePage(&page, actor, access, repository.ArchivedAt != nil || repository.MigratingAt != nil)
	writeJSON(writer, http.StatusOK, page)
}

func (api *API) getDiscussion(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.optionalRepository(writer, request)
	if !ok {
		return
	}
	number, ok := discussionNumber(writer, request)
	if !ok {
		return
	}
	commentPage, commentsPerPage, ok := commentPagination(writer, request)
	if !ok {
		return
	}
	viewerID := ""
	if actor != nil {
		viewerID = actor.ID
	}
	discussion, err := api.store.Get(
		request.Context(), repository.ID, number, viewerID, commentPage, commentsPerPage,
	)
	if err != nil {
		api.storeError(writer, request, "get discussion", err)
		return
	}
	access, ok := api.access(writer, request, actor, repository)
	if !ok {
		return
	}
	decorateDiscussion(&discussion, actor, access, repository.ArchivedAt != nil || repository.MigratingAt != nil)
	writeJSON(writer, http.StatusOK, discussion)
}

func (api *API) createDiscussion(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.requiredRepository(writer, request)
	if !ok {
		return
	}
	var body discussionRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	discussion, err := api.store.Create(request.Context(), actor, repositoryRef(repository), CreateInput{
		CategorySlug: body.Category, Title: body.Title, Body: body.Body,
	})
	if err != nil {
		api.storeError(writer, request, "create discussion", err)
		return
	}
	access, ok := api.access(writer, request, &actor, repository)
	if !ok {
		return
	}
	decorateDiscussion(&discussion, &actor, access, repository.ArchivedAt != nil || repository.MigratingAt != nil)
	writeJSON(writer, http.StatusCreated, discussion)
}

func (api *API) updateDiscussion(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.requiredRepository(writer, request)
	if !ok {
		return
	}
	number, ok := discussionNumber(writer, request)
	if !ok {
		return
	}
	var body struct {
		Category *string `json:"category,omitempty"`
		Title    *string `json:"title,omitempty"`
		Body     *string `json:"body,omitempty"`
		State    *string `json:"state,omitempty"`
		Locked   *bool   `json:"locked,omitempty"`
		Pinned   *bool   `json:"pinned,omitempty"`
	}
	if !decodeJSON(writer, request, &body) {
		return
	}
	discussion, err := api.store.Update(request.Context(), actor, repositoryRef(repository), number, UpdateInput{
		CategorySlug: body.Category,
		Title:        body.Title,
		Body:         body.Body,
		State:        body.State,
		Locked:       body.Locked,
		Pinned:       body.Pinned,
	})
	if err != nil {
		api.storeError(writer, request, "update discussion", err)
		return
	}
	access, ok := api.access(writer, request, &actor, repository)
	if !ok {
		return
	}
	decorateDiscussion(&discussion, &actor, access, repository.ArchivedAt != nil || repository.MigratingAt != nil)
	writeJSON(writer, http.StatusOK, discussion)
}

func (api *API) deleteDiscussion(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.requiredRepository(writer, request)
	if !ok {
		return
	}
	number, ok := discussionNumber(writer, request)
	if !ok {
		return
	}
	if err := api.store.Delete(request.Context(), actor, repositoryRef(repository), number); err != nil {
		api.storeError(writer, request, "delete discussion", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) deleteNested(writer http.ResponseWriter, request *http.Request) {
	first, second := request.PathValue("first"), request.PathValue("second")
	switch {
	case first == "categories":
		request.SetPathValue("category", second)
		api.deleteCategory(writer, request)
	case second == "vote":
		request.SetPathValue("number", first)
		api.removeVote(writer, request)
	default:
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
	}
}

func (api *API) addVote(writer http.ResponseWriter, request *http.Request) {
	api.setVote(writer, request, true)
}

func (api *API) removeVote(writer http.ResponseWriter, request *http.Request) {
	api.setVote(writer, request, false)
}

func (api *API) setVote(writer http.ResponseWriter, request *http.Request, enabled bool) {
	actor, repository, ok := api.requiredRepository(writer, request)
	if !ok {
		return
	}
	number, ok := discussionNumber(writer, request)
	if !ok {
		return
	}
	summary, err := api.store.SetVote(request.Context(), actor, repositoryRef(repository), number, enabled)
	if err != nil {
		api.storeError(writer, request, "set discussion vote", err)
		return
	}
	access, ok := api.access(writer, request, &actor, repository)
	if !ok {
		return
	}
	decorateSummary(&summary, &actor, access, repository.ArchivedAt != nil || repository.MigratingAt != nil)
	writeJSON(writer, http.StatusOK, summary)
}

func (api *API) createComment(writer http.ResponseWriter, request *http.Request) {
	actor, repository, number, ok := api.commentMutationContext(writer, request)
	if !ok {
		return
	}
	var body commentRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	comment, err := api.store.CreateComment(
		request.Context(), actor, repositoryRef(repository), number, body.ParentID, body.Body,
	)
	if err != nil {
		api.storeError(writer, request, "create discussion comment", err)
		return
	}
	if !api.decorateCommentResponse(writer, request, &actor, repository, number, &comment) {
		return
	}
	writeJSON(writer, http.StatusCreated, comment)
}

func (api *API) updateComment(writer http.ResponseWriter, request *http.Request) {
	actor, repository, number, ok := api.commentMutationContext(writer, request)
	if !ok {
		return
	}
	var body commentRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	comment, err := api.store.UpdateComment(
		request.Context(), actor, repositoryRef(repository), number,
		request.PathValue("commentID"), body.Body,
	)
	if err != nil {
		api.storeError(writer, request, "update discussion comment", err)
		return
	}
	if !api.decorateCommentResponse(writer, request, &actor, repository, number, &comment) {
		return
	}
	writeJSON(writer, http.StatusOK, comment)
}

func (api *API) decorateCommentResponse(
	writer http.ResponseWriter,
	request *http.Request,
	actor *platform.User,
	repository collab.Repository,
	number int64,
	comment *Comment,
) bool {
	access, ok := api.access(writer, request, actor, repository)
	if !ok {
		return false
	}
	discussion, err := api.store.Get(request.Context(), repository.ID, number, actor.ID, 1, 1)
	if err != nil {
		api.storeError(writer, request, "decorate discussion comment", err)
		return false
	}
	decorateComment(comment, discussion.Summary, actor, access,
		repository.ArchivedAt != nil || repository.MigratingAt != nil)
	return true
}

func (api *API) deleteComment(writer http.ResponseWriter, request *http.Request) {
	actor, repository, number, ok := api.commentMutationContext(writer, request)
	if !ok {
		return
	}
	err := api.store.DeleteComment(
		request.Context(), actor, repositoryRef(repository), number, request.PathValue("commentID"),
	)
	if err != nil {
		api.storeError(writer, request, "delete discussion comment", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) markAnswer(writer http.ResponseWriter, request *http.Request) {
	api.setAnswer(writer, request, true)
}

func (api *API) unmarkAnswer(writer http.ResponseWriter, request *http.Request) {
	api.setAnswer(writer, request, false)
}

func (api *API) setAnswer(writer http.ResponseWriter, request *http.Request, accepted bool) {
	actor, repository, number, ok := api.commentMutationContext(writer, request)
	if !ok {
		return
	}
	discussion, err := api.store.SetAnswer(
		request.Context(), actor, repositoryRef(repository), number,
		request.PathValue("commentID"), accepted,
	)
	if err != nil {
		api.storeError(writer, request, "set discussion answer", err)
		return
	}
	access, ok := api.access(writer, request, &actor, repository)
	if !ok {
		return
	}
	decorateDiscussion(&discussion, &actor, access, repository.ArchivedAt != nil || repository.MigratingAt != nil)
	writeJSON(writer, http.StatusOK, discussion)
}

func (api *API) commentMutationContext(
	writer http.ResponseWriter,
	request *http.Request,
) (platform.User, collab.Repository, int64, bool) {
	actor, repository, ok := api.requiredRepository(writer, request)
	if !ok {
		return platform.User{}, collab.Repository{}, 0, false
	}
	number, ok := discussionNumber(writer, request)
	return actor, repository, number, ok
}

func listFilter(writer http.ResponseWriter, request *http.Request) (ListFilter, bool) {
	query := request.URL.Query()
	filter := ListFilter{
		Category: strings.TrimSpace(query.Get("category")),
		State:    strings.TrimSpace(query.Get("state")),
		Query:    strings.TrimSpace(query.Get("q")),
		Sort:     strings.TrimSpace(query.Get("sort")),
		Page:     1,
		PerPage:  50,
	}
	if len(filter.Query) > 200 || len(filter.Category) > 64 {
		writeProblem(writer, http.StatusBadRequest, "invalid_query", "Discussion filters are invalid")
		return ListFilter{}, false
	}
	if filter.State != "" && filter.State != "all" && filter.State != "open" &&
		filter.State != "closed" {
		writeProblem(writer, http.StatusBadRequest, "invalid_query", "state is invalid")
		return ListFilter{}, false
	}
	if filter.Sort != "" && filter.Sort != "newest" && filter.Sort != "oldest" &&
		filter.Sort != "most-commented" && filter.Sort != "most-voted" {
		writeProblem(writer, http.StatusBadRequest, "invalid_query", "sort is invalid")
		return ListFilter{}, false
	}
	var ok bool
	filter.Page, ok = positiveQueryInteger(query.Get("page"), 1, 10_000)
	if !ok {
		writeProblem(writer, http.StatusBadRequest, "invalid_pagination", "page is invalid")
		return ListFilter{}, false
	}
	filter.PerPage, ok = positiveQueryInteger(query.Get("per_page"), 50, 100)
	if !ok {
		writeProblem(writer, http.StatusBadRequest, "invalid_pagination", "per_page is invalid")
		return ListFilter{}, false
	}
	return filter, true
}

func commentPagination(writer http.ResponseWriter, request *http.Request) (int, int, bool) {
	query := request.URL.Query()
	page, ok := positiveQueryInteger(query.Get("comment_page"), 1, 10_000)
	if !ok {
		writeProblem(writer, http.StatusBadRequest, "invalid_pagination", "comment_page is invalid")
		return 0, 0, false
	}
	perPage, ok := positiveQueryInteger(query.Get("comments_per_page"), 100, 100)
	if !ok {
		writeProblem(writer, http.StatusBadRequest, "invalid_pagination", "comments_per_page is invalid")
		return 0, 0, false
	}
	return page, perPage, true
}

func positiveQueryInteger(value string, defaultValue int, maximum int) (int, bool) {
	if value == "" {
		return defaultValue, true
	}
	parsed, err := strconv.Atoi(value)
	return parsed, err == nil && parsed >= 1 && parsed <= maximum
}

func discussionNumber(writer http.ResponseWriter, request *http.Request) (int64, bool) {
	number, err := strconv.ParseInt(request.PathValue("number"), 10, 64)
	if err != nil || number < 1 {
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return 0, false
	}
	return number, true
}
