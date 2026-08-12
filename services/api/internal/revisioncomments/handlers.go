package revisioncomments

import (
	"net/http"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type bodyRequest struct {
	Body string `json:"body"`
}

func (api *API) list(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actors.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	repository, ok := api.lookup(writer, request, actor)
	if !ok {
		return
	}
	revision, err := validateRevision(request.PathValue("revision"))
	if err != nil {
		api.storeError(writer, request, "validate revision comment revision", err)
		return
	}
	page, perPage, err := parsePage(request.URL.Query())
	if err != nil {
		api.storeError(writer, request, "validate revision comment page", err)
		return
	}
	if !api.verifyRevision(writer, request, actor, repository, revision) {
		return
	}
	comments, err := api.store.ListRevisionComments(
		request.Context(), actor, repository, revision, page, perPage,
	)
	if err != nil {
		api.storeError(writer, request, "list revision comments", err)
		return
	}
	writeJSON(writer, http.StatusOK, comments)
}

func (api *API) create(writer http.ResponseWriter, request *http.Request) {
	actor, repository, revision, ok := api.mutation(writer, request)
	if !ok {
		return
	}
	var input bodyRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	body, err := validateBody(input.Body)
	if err != nil {
		api.storeError(writer, request, "validate revision comment body", err)
		return
	}
	if !api.verifyRevision(writer, request, &actor, repository, revision) {
		return
	}
	comment, err := api.store.CreateRevisionComment(
		request.Context(), actor, repository, revision, body,
	)
	if err != nil {
		api.storeError(writer, request, "create revision comment", err)
		return
	}
	writer.Header().Set("Location", request.URL.Path+"/"+comment.ID)
	writeJSON(writer, http.StatusCreated, comment)
}

func (api *API) update(writer http.ResponseWriter, request *http.Request) {
	actor, repository, revision, ok := api.mutation(writer, request)
	if !ok {
		return
	}
	commentID, err := validateCommentID(request.PathValue("commentID"))
	if err != nil {
		api.storeError(writer, request, "validate revision comment identifier", err)
		return
	}
	var input bodyRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	body, err := validateBody(input.Body)
	if err != nil {
		api.storeError(writer, request, "validate revision comment body", err)
		return
	}
	comment, err := api.store.UpdateRevisionComment(
		request.Context(), actor, repository, revision, commentID, body,
	)
	if err != nil {
		api.storeError(writer, request, "update revision comment", err)
		return
	}
	writeJSON(writer, http.StatusOK, comment)
}

func (api *API) delete(writer http.ResponseWriter, request *http.Request) {
	actor, repository, revision, ok := api.mutation(writer, request)
	if !ok {
		return
	}
	commentID, err := validateCommentID(request.PathValue("commentID"))
	if err != nil {
		api.storeError(writer, request, "validate revision comment identifier", err)
		return
	}
	if err := api.store.DeleteRevisionComment(
		request.Context(), actor, repository, revision, commentID,
	); err != nil {
		api.storeError(writer, request, "delete revision comment", err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (api *API) mutation(
	writer http.ResponseWriter,
	request *http.Request,
) (platform.User, collab.Repository, string, bool) {
	actor, ok := api.actors.ResolveActor(writer, request)
	if !ok {
		return platform.User{}, collab.Repository{}, "", false
	}
	repository, ok := api.lookup(writer, request, &actor)
	if !ok {
		return platform.User{}, collab.Repository{}, "", false
	}
	if repository.ArchivedAt != nil {
		writeProblem(writer, http.StatusForbidden, "repository_archived", "The repository is read-only")
		return platform.User{}, collab.Repository{}, "", false
	}
	revision, err := validateRevision(request.PathValue("revision"))
	if err != nil {
		api.storeError(writer, request, "validate revision comment revision", err)
		return platform.User{}, collab.Repository{}, "", false
	}
	return actor, repository, revision, true
}

func (api *API) verifyRevision(
	writer http.ResponseWriter,
	request *http.Request,
	actor *platform.User,
	repository collab.Repository,
	revision string,
) bool {
	if api.code == nil || api.credentials == nil {
		writeProblem(
			writer, http.StatusServiceUnavailable, "lore_unavailable",
			"Lore revision verification is unavailable",
		)
		return false
	}
	principal := loreclient.ServicePrincipal(
		loreclient.ServicePurposePublicReader, api.publicReaderSubject,
	)
	if actor != nil {
		principal = loreclient.UserPrincipal(actor.ID)
	}
	reference := loreRepositoryRef(repository)
	credential, err := api.credentials.ForRepository(request.Context(), loreclient.CredentialRequest{
		Principal: principal, Repository: reference,
		Partition: reference.CanonicalPartition(), Scope: loreclient.ScopeRead,
	})
	if err != nil {
		api.loreError(writer, request, "issue revision comment verification credential", err)
		return false
	}
	detail, err := api.code.RevisionInfo(request.Context(), reference, revision, credential)
	if err != nil {
		api.loreError(writer, request, "verify revision comment revision", err)
		return false
	}
	if detail.Revision != revision {
		api.loreError(writer, request, "verify revision comment revision", loreclient.ErrNotFound)
		return false
	}
	return true
}
