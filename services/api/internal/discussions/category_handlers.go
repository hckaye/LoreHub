package discussions

import (
	"net/http"

	"github.com/lorehub/lorehub/services/api/internal/collab"
)

func (api *API) listCategories(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.optionalRepository(writer, request)
	if !ok {
		return
	}
	categories, err := api.store.ListCategories(request.Context(), repository.ID)
	if err != nil {
		api.storeError(writer, request, "list discussion categories", err)
		return
	}
	access, ok := api.access(writer, request, actor, repository)
	if !ok {
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"categories": categories,
		"viewerCanManage": actor != nil && access.CanManageBranchRules() &&
			repository.ArchivedAt == nil && repository.MigratingAt == nil,
		"viewerCanModerate": actor != nil && access.AtLeast(collab.PermWrite) &&
			repository.ArchivedAt == nil && repository.MigratingAt == nil,
	})
}

func (api *API) createCategory(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.requiredRepository(writer, request)
	if !ok {
		return
	}
	var body categoryRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	category, err := api.store.CreateCategory(
		request.Context(),
		actor,
		repositoryRef(repository),
		CategoryInput{
			Slug: body.Slug, Name: body.Name, Description: body.Description, Format: body.Format,
		},
	)
	if err != nil {
		api.storeError(writer, request, "create discussion category", err)
		return
	}
	writeJSON(writer, http.StatusCreated, category)
}

func (api *API) updateCategory(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.requiredRepository(writer, request)
	if !ok {
		return
	}
	var body categoryRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	category, err := api.store.UpdateCategory(
		request.Context(),
		actor,
		repositoryRef(repository),
		request.PathValue("category"),
		CategoryInput{
			Slug: body.Slug, Name: body.Name, Description: body.Description, Format: body.Format,
		},
	)
	if err != nil {
		api.storeError(writer, request, "update discussion category", err)
		return
	}
	writeJSON(writer, http.StatusOK, category)
}

func (api *API) deleteCategory(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.requiredRepository(writer, request)
	if !ok {
		return
	}
	if err := api.store.DeleteCategory(
		request.Context(), actor, repositoryRef(repository), request.PathValue("category"),
	); err != nil {
		api.storeError(writer, request, "delete discussion category", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}
