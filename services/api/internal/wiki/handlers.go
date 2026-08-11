package wiki

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type createPageRequest struct {
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	EditSummary string `json:"editSummary"`
}

type updatePageRequest struct {
	Slug            *string `json:"slug"`
	Title           *string `json:"title"`
	Body            *string `json:"body"`
	EditSummary     string  `json:"editSummary"`
	ExpectedVersion int     `json:"expectedVersion"`
}

type deletePageRequest struct {
	ExpectedVersion int `json:"expectedVersion"`
}

func (api *API) listPages(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actors.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	repository, ok := api.lookup(writer, request, actor)
	if !ok {
		return
	}
	query := strings.TrimSpace(request.URL.Query().Get("q"))
	if len(query) > 200 {
		writeProblem(writer, http.StatusBadRequest, "invalid_query", "Search query is too long")
		return
	}
	pages, err := api.store.List(request.Context(), repository.ID, query, 100)
	if err != nil {
		api.storeError(writer, request, "list wiki pages", err)
		return
	}
	viewerCanWrite := false
	if actor != nil {
		viewerCanWrite, ok = api.canWrite(writer, request, *actor, repository)
		if !ok {
			return
		}
	}
	writeJSON(writer, http.StatusOK, PageList{Pages: pages, ViewerCanWrite: viewerCanWrite})
}

func (api *API) getPage(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actors.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	repository, ok := api.lookup(writer, request, actor)
	if !ok {
		return
	}
	slug, ok := pathSlug(writer, request)
	if !ok {
		return
	}
	page, err := api.store.Get(request.Context(), repository.ID, slug)
	if err != nil {
		api.storeError(writer, request, "get wiki page", err)
		return
	}
	if actor != nil {
		page.ViewerCanWrite, ok = api.canWrite(writer, request, *actor, repository)
		if !ok {
			return
		}
	}
	writeJSON(writer, http.StatusOK, page)
}

func (api *API) pageHistory(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actors.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	repository, ok := api.lookup(writer, request, actor)
	if !ok {
		return
	}
	slug, ok := pathSlug(writer, request)
	if !ok {
		return
	}
	history, err := api.store.History(request.Context(), repository.ID, slug, 100)
	if err != nil {
		api.storeError(writer, request, "list wiki history", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"revisions": history})
}

func (api *API) pageRevision(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actors.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	repository, ok := api.lookup(writer, request, actor)
	if !ok {
		return
	}
	slug, ok := pathSlug(writer, request)
	if !ok {
		return
	}
	version, err := strconv.Atoi(request.PathValue("version"))
	if err != nil || version < 1 {
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return
	}
	revision, err := api.store.Revision(request.Context(), repository.ID, slug, version)
	if err != nil {
		api.storeError(writer, request, "get wiki revision", err)
		return
	}
	writeJSON(writer, http.StatusOK, revision)
}

func (api *API) createPage(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.requireWrite(writer, request)
	if !ok {
		return
	}
	var body createPageRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	page, err := api.store.Create(request.Context(), actor, repositoryRef(repository), CreateInput{
		Slug: body.Slug, Title: body.Title, Body: body.Body, EditSummary: body.EditSummary,
	})
	if err != nil {
		api.storeError(writer, request, "create wiki page", err)
		return
	}
	writer.Header().Set("Location", request.URL.Path+"/"+url.PathEscape(page.Slug))
	writeJSON(writer, http.StatusCreated, page)
}

func (api *API) updatePage(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.requireWrite(writer, request)
	if !ok {
		return
	}
	slug, ok := pathSlug(writer, request)
	if !ok {
		return
	}
	var body updatePageRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	page, err := api.store.Update(request.Context(), actor, repositoryRef(repository), slug, UpdateInput{
		Slug: body.Slug, Title: body.Title, Body: body.Body,
		EditSummary: body.EditSummary, ExpectedVersion: body.ExpectedVersion,
	})
	if err != nil {
		api.storeError(writer, request, "update wiki page", err)
		return
	}
	writeJSON(writer, http.StatusOK, page)
}

func (api *API) deletePage(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.requireWrite(writer, request)
	if !ok {
		return
	}
	slug, ok := pathSlug(writer, request)
	if !ok {
		return
	}
	var body deletePageRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	if err := api.store.Delete(
		request.Context(), actor, repositoryRef(repository), slug, body.ExpectedVersion,
	); err != nil {
		api.storeError(writer, request, "delete wiki page", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func pathSlug(writer http.ResponseWriter, request *http.Request) (string, bool) {
	slug := slugify(request.PathValue("slug"))
	if slug == "" || len(slug) > maxSlugBytes {
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return "", false
	}
	return slug, true
}
