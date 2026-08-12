package statuses

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type createRequest struct {
	Context        string  `json:"context"`
	State          string  `json:"state"`
	Description    string  `json:"description"`
	TargetURL      string  `json:"targetUrl"`
	IdempotencyKey *string `json:"idempotencyKey"`
}

type githubCreateRequest struct {
	Context     string `json:"context"`
	State       string `json:"state"`
	Description string `json:"description"`
	TargetURL   string `json:"target_url"`
}

type githubCreator struct {
	ID        string `json:"id"`
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
}

type githubStatus struct {
	ID          string        `json:"id"`
	State       string        `json:"state"`
	Description string        `json:"description"`
	TargetURL   string        `json:"target_url"`
	Context     string        `json:"context"`
	Creator     githubCreator `json:"creator"`
	CreatedAt   string        `json:"created_at"`
	UpdatedAt   string        `json:"updated_at"`
}

func (api *API) listStatuses(writer http.ResponseWriter, request *http.Request) {
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
		api.storeError(writer, request, "validate Lore revision", err)
		return
	}
	page, perPage, ok := parsePagination(writer, request)
	if !ok {
		return
	}
	statuses, err := api.store.List(request.Context(), repository.ID, revision, page, perPage)
	if err != nil {
		api.storeError(writer, request, "list revision statuses", err)
		return
	}
	writeJSON(writer, http.StatusOK, statuses)
}

func (api *API) createStatus(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.requireWrite(writer, request)
	if !ok {
		return
	}
	var body createRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	api.create(writer, request, actor, repository, CreateInput{
		Revision: request.PathValue("revision"), Context: body.Context,
		State: body.State, Description: body.Description, TargetURL: body.TargetURL,
		IdempotencyKey: body.IdempotencyKey,
	}, false)
}

func (api *API) createGitHubStatus(writer http.ResponseWriter, request *http.Request) {
	actor, repository, ok := api.requireWrite(writer, request)
	if !ok {
		return
	}
	var body githubCreateRequest
	if !decodeJSON(writer, request, &body) {
		return
	}
	api.create(writer, request, actor, repository, CreateInput{
		Revision: request.PathValue("revision"), Context: body.Context,
		State: body.State, Description: body.Description, TargetURL: body.TargetURL,
	}, true)
}

func (api *API) create(
	writer http.ResponseWriter,
	request *http.Request,
	actor platform.User,
	repository collab.Repository,
	input CreateInput,
	github bool,
) {
	input, err := withIdempotencyHeader(input, request.Header.Get("Idempotency-Key"))
	if err != nil {
		api.storeError(writer, request, "validate idempotency key", err)
		return
	}
	input, err = validateCreate(input)
	if err != nil {
		api.storeError(writer, request, "validate revision status", err)
		return
	}
	if !api.verifyRevision(writer, request, actor, repository, input.Revision) {
		return
	}
	result, err := api.store.Create(request.Context(), actor, repositoryRef(repository), input)
	if err != nil {
		api.storeError(writer, request, "create revision status", err)
		return
	}
	statusCode := http.StatusCreated
	if !result.Created {
		statusCode = http.StatusOK
	}
	if github {
		writeJSON(writer, statusCode, githubStatusResponse(result.Status))
		return
	}
	writeJSON(writer, statusCode, result.Status)
}

func (api *API) verifyRevision(
	writer http.ResponseWriter,
	request *http.Request,
	actor platform.User,
	repository collab.Repository,
	revision string,
) bool {
	if api.lore == nil || api.credentials == nil {
		writeProblem(
			writer, http.StatusServiceUnavailable, "lore_unavailable",
			"Lore revision verification is unavailable",
		)
		return false
	}
	reference := loreRepositoryRef(repository)
	credential, err := api.credentials.ForRepository(request.Context(), loreclient.CredentialRequest{
		Principal: loreclient.UserPrincipal(actor.ID), Repository: reference,
		Partition: reference.CanonicalPartition(), Scope: loreclient.ScopeRead,
	})
	if err != nil {
		api.loreError(writer, request, "issue revision status verification credential", err)
		return false
	}
	detail, err := api.lore.RevisionInfo(request.Context(), reference, revision, credential)
	if err != nil {
		api.loreError(writer, request, "verify revision status revision", err)
		return false
	}
	if detail.Revision != revision {
		api.loreError(writer, request, "verify revision status revision", loreclient.ErrNotFound)
		return false
	}
	return true
}

func withIdempotencyHeader(input CreateInput, header string) (CreateInput, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return input, nil
	}
	if input.IdempotencyKey != nil && strings.TrimSpace(*input.IdempotencyKey) != header {
		return CreateInput{}, invalid("Idempotency-Key does not match idempotencyKey")
	}
	input.IdempotencyKey = &header
	return input, nil
}

func githubStatusResponse(status Status) githubStatus {
	timestamp := status.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	return githubStatus{
		ID: status.ID, State: status.State, Description: status.Description,
		TargetURL: status.TargetURL, Context: status.Context,
		Creator: githubCreator{
			ID: status.Creator.ID, Login: status.Creator.Username,
			AvatarURL: status.Creator.AvatarURL,
		},
		CreatedAt: timestamp, UpdatedAt: timestamp,
	}
}

func parsePagination(writer http.ResponseWriter, request *http.Request) (int, int, bool) {
	page, err := positiveQuery(request.URL.Query().Get("page"), 1, 1_000_000)
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_page", "The page is invalid")
		return 0, 0, false
	}
	perPage, err := positiveQuery(
		request.URL.Query().Get("perPage"), defaultHistoryPerPage, maxHistoryPerPage,
	)
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_per_page", "The page size is invalid")
		return 0, 0, false
	}
	return page, perPage, true
}

func positiveQuery(value string, fallback int, maximum int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < 1 || number > maximum {
		return 0, errors.New("invalid positive query value")
	}
	return number, nil
}
