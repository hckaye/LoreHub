package releases

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type createRequest struct {
	TagName      string `json:"tagName"`
	Title        string `json:"title"`
	Notes        string `json:"notes"`
	SourceBranch string `json:"sourceBranch"`
	Revision     string `json:"revision"`
	State        string `json:"state"`
}

type updateRequest struct {
	Title           *string `json:"title"`
	Notes           *string `json:"notes"`
	ExpectedVersion int64   `json:"expectedVersion"`
}

type versionRequest struct {
	ExpectedVersion int64 `json:"expectedVersion"`
}

type assetRequest struct {
	Name            string `json:"name"`
	ExternalURL     string `json:"externalUrl"`
	ExpectedVersion int64  `json:"expectedVersion"`
}

func (api *API) listReleases(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actors.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	repository, ok := api.lookup(writer, request, actor)
	if !ok {
		return
	}
	page, perPage, ok := parsePagination(writer, request)
	if !ok {
		return
	}
	viewerCanWrite := false
	if actor != nil {
		viewerCanWrite, ok = api.canWrite(writer, request, *actor, repository)
		if !ok {
			return
		}
	}
	releases, err := api.store.List(request.Context(), repository.ID, viewerCanWrite, page, perPage)
	if err != nil {
		api.storeError(writer, request, "list releases", err)
		return
	}
	releases.ViewerCanWrite = viewerCanWrite
	for index := range releases.Releases {
		releases.Releases[index].ViewerCanWrite = viewerCanWrite
	}
	writeJSON(writer, http.StatusOK, releases)
}

func (api *API) getRelease(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actors.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	repository, ok := api.lookup(writer, request, actor)
	if !ok {
		return
	}
	releaseID, ok := parseUUID(writer, request.PathValue("releaseID"))
	if !ok {
		return
	}
	viewerCanWrite := false
	if actor != nil {
		viewerCanWrite, ok = api.canWrite(writer, request, *actor, repository)
		if !ok {
			return
		}
	}
	release, err := api.store.Get(request.Context(), repository.ID, releaseID, viewerCanWrite)
	if err != nil {
		api.storeError(writer, request, "get release", err)
		return
	}
	release.ViewerCanWrite = viewerCanWrite
	writeJSON(writer, http.StatusOK, release)
}

func (api *API) createRelease(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.requireWrite(writer, request)
	if !ok {
		return
	}
	var body createRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	input, err := validateCreate(CreateInput{
		TagName: body.TagName, Title: body.Title, Notes: body.Notes,
		SourceBranch: body.SourceBranch, Revision: body.Revision, State: body.State,
	})
	if err != nil {
		api.storeError(writer, request, "validate release", err)
		return
	}
	if !api.verifyRevision(writer, request, actor.ID, repository, input) {
		return
	}
	release, err := api.store.Create(request.Context(), actor, repositoryRef(repository), input)
	if err != nil {
		api.storeError(writer, request, "create release", err)
		return
	}
	writer.Header().Set("Location", request.URL.Path+"/"+release.ID)
	writeJSON(writer, http.StatusCreated, release)
}

func (api *API) updateRelease(writer http.ResponseWriter, request *http.Request) {
	actor, repository, releaseID, ok := api.mutationContext(writer, request)
	if !ok {
		return
	}
	var body updateRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	release, err := api.store.Update(request.Context(), actor, repositoryRef(repository), releaseID,
		UpdateInput{Title: body.Title, Notes: body.Notes, ExpectedVersion: body.ExpectedVersion})
	if err != nil {
		api.storeError(writer, request, "update release", err)
		return
	}
	writeJSON(writer, http.StatusOK, release)
}

func (api *API) publishRelease(writer http.ResponseWriter, request *http.Request) {
	actor, repository, releaseID, ok := api.mutationContext(writer, request)
	if !ok {
		return
	}
	var body versionRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	release, err := api.store.Publish(
		request.Context(), actor, repositoryRef(repository), releaseID, body.ExpectedVersion,
	)
	if err != nil {
		api.storeError(writer, request, "publish release", err)
		return
	}
	writeJSON(writer, http.StatusOK, release)
}

func (api *API) deleteRelease(writer http.ResponseWriter, request *http.Request) {
	actor, repository, releaseID, ok := api.mutationContext(writer, request)
	if !ok {
		return
	}
	var body versionRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	err := api.store.Delete(
		request.Context(), actor, repositoryRef(repository), releaseID, body.ExpectedVersion,
	)
	if err != nil {
		api.storeError(writer, request, "delete release", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) addAsset(writer http.ResponseWriter, request *http.Request) {
	actor, repository, releaseID, ok := api.mutationContext(writer, request)
	if !ok {
		return
	}
	var body assetRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	release, err := api.store.AddAsset(request.Context(), actor, repositoryRef(repository), releaseID,
		AssetInput{Name: body.Name, ExternalURL: body.ExternalURL, ExpectedVersion: body.ExpectedVersion})
	if err != nil {
		api.storeError(writer, request, "create release asset", err)
		return
	}
	writeJSON(writer, http.StatusCreated, release)
}

func (api *API) deleteAsset(writer http.ResponseWriter, request *http.Request) {
	actor, repository, releaseID, ok := api.mutationContext(writer, request)
	if !ok {
		return
	}
	assetID, ok := parseUUID(writer, request.PathValue("assetID"))
	if !ok {
		return
	}
	var body versionRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	release, err := api.store.DeleteAsset(
		request.Context(), actor, repositoryRef(repository), releaseID, assetID, body.ExpectedVersion,
	)
	if err != nil {
		api.storeError(writer, request, "delete release asset", err)
		return
	}
	writeJSON(writer, http.StatusOK, release)
}

func (api *API) mutationContext(
	writer http.ResponseWriter,
	request *http.Request,
) (platform.User, collab.Repository, string, bool) {
	actor, repository, ok := api.requireWrite(writer, request)
	if !ok {
		return platform.User{}, collab.Repository{}, "", false
	}
	releaseID, ok := parseUUID(writer, request.PathValue("releaseID"))
	return actor, repository, releaseID, ok
}

func (api *API) verifyRevision(
	writer http.ResponseWriter,
	request *http.Request,
	actorID string,
	repository collab.Repository,
	input CreateInput,
) bool {
	if api.credentials == nil || api.lore == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "lore_unavailable",
			"Lore revision verification is unavailable")
		return false
	}
	reference := loreRepositoryRef(repository)
	credential, err := api.credentials.ForRepository(request.Context(), loreclient.CredentialRequest{
		Principal: loreclient.UserPrincipal(actorID), Repository: reference,
		Partition: reference.CanonicalPartition(), Scope: loreclient.ScopeRead,
	})
	if err != nil {
		api.loreError(writer, request, "issue release verification credential", err)
		return false
	}
	branches, err := api.lore.Branches(request.Context(), reference, credential)
	if err != nil {
		api.loreError(writer, request, "verify release revision", err)
		return false
	}
	for _, branch := range branches {
		if branch.Name == input.SourceBranch && !branch.Archived {
			if branch.LatestRevision == input.Revision {
				return true
			}
			break
		}
	}
	writeProblem(writer, http.StatusConflict, "source_changed",
		"The Lore branch no longer points to the requested revision")
	return false
}

func parsePagination(writer http.ResponseWriter, request *http.Request) (int, int, bool) {
	page, err := positiveQuery(request.URL.Query().Get("page"), 1, 1_000_000)
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_page", "The page is invalid")
		return 0, 0, false
	}
	perPage, err := positiveQuery(request.URL.Query().Get("perPage"), 20, 100)
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_per_page", "The page size is invalid")
		return 0, 0, false
	}
	return page, perPage, true
}

func positiveQuery(value string, fallback, maximum int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < 1 || number > maximum {
		return 0, ErrInvalidInput
	}
	return number, nil
}

func parseUUID(writer http.ResponseWriter, value string) (string, bool) {
	parsed, err := uuid.Parse(value)
	if err != nil {
		writeProblem(writer, http.StatusNotFound, "not_found", "The release was not found")
		return "", false
	}
	return parsed.String(), true
}

func (api *API) storeError(writer http.ResponseWriter, request *http.Request, operation string, err error) {
	switch {
	case errors.Is(err, platform.ErrNotFound):
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
	case errors.Is(err, platform.ErrForbidden):
		writeProblem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
	case errors.Is(err, platform.ErrConflict):
		writeProblem(writer, http.StatusConflict, "conflict", "A release with this tag already exists")
	case errors.Is(err, ErrVersionConflict):
		writeProblem(writer, http.StatusConflict, "version_conflict", "The release changed; reload and try again")
	case errors.Is(err, ErrInvalidInput):
		writeProblem(writer, http.StatusBadRequest, "invalid_input", err.Error())
	default:
		if api.logger != nil {
			api.logger.Error(operation, "error", err, "method", request.Method, "path", request.URL.Path)
		}
		writeProblem(writer, http.StatusInternalServerError, "internal_error",
			"The request could not be completed")
	}
}

func (api *API) loreError(writer http.ResponseWriter, request *http.Request, operation string, err error) {
	if api.logger != nil {
		api.logger.Error(operation, "error", err, "method", request.Method, "path", request.URL.Path)
	}
	writeProblem(writer, http.StatusBadGateway, "lore_unavailable",
		"Lore could not verify the release revision")
}
