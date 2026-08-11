package collab

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

// ActorResolver is implemented by the top-level HTTP API. It centralizes
// bearer authentication, browser session lookup and cookie CSRF checks for
// every route, including collaboration routes.
type ActorResolver interface {
	ResolveActor(http.ResponseWriter, *http.Request) (platform.User, bool)
	ResolveOptionalActor(http.ResponseWriter, *http.Request) (*platform.User, bool)
}

// API holds the dependencies shared by all collaboration handlers.
type API struct {
	store          Store
	assignees      IssueAssigneeStore
	reviewRequests ReviewRequestStore
	metadata       MergeRequestMetadataStore
	drafts         MergeRequestDraftStore
	actors         ActorResolver
	logger         *slog.Logger
}

// NewAPI constructs a collaboration API backed by the given store.
func NewAPI(store Store, actors ActorResolver, logger *slog.Logger) *API {
	assignees, _ := store.(IssueAssigneeStore)
	reviewRequests, _ := store.(ReviewRequestStore)
	metadata, _ := store.(MergeRequestMetadataStore)
	drafts, _ := store.(MergeRequestDraftStore)
	return &API{
		store: store, assignees: assignees, reviewRequests: reviewRequests,
		metadata: metadata, drafts: drafts, actors: actors, logger: logger,
	}
}

// actor authenticates a mutating request and provisions the local user.
func (api *API) actor(writer http.ResponseWriter, request *http.Request) (platform.User, bool) {
	return api.actors.ResolveActor(writer, request)
}

// optionalActor delegates optional browser-session or bearer resolution to the
// shared HTTP authentication layer; anonymous callers receive a nil user.
func (api *API) optionalActor(writer http.ResponseWriter, request *http.Request) (*platform.User, bool) {
	return api.actors.ResolveOptionalActor(writer, request)
}

// lookup resolves a repository visible to the actor. On failure it writes the
// appropriate error response and returns ok=false.
func (api *API) lookup(
	writer http.ResponseWriter,
	request *http.Request,
	actor *platform.User,
) (Repository, bool) {
	repo, err := api.store.LookupRepository(requestContext(request), actor,
		request.PathValue("owner"), request.PathValue("repository"))
	if err != nil {
		storeError(writer, request, "lookup repository", err, api.logger)
		return Repository{}, false
	}
	return repo, true
}

// permission computes the actor's access on the repository.
func (api *API) permission(
	writer http.ResponseWriter,
	request *http.Request,
	actor platform.User,
	repo Repository,
) (Access, bool) {
	access, err := api.store.RepositoryPermission(requestContext(request), actor, repo)
	if err != nil {
		storeError(writer, request, "compute repository permission", err, api.logger)
		return Access{}, false
	}
	return access, true
}

// requireMutationActor resolves an authenticated actor and a visible repository.
// It is the common preamble for mutating endpoints.
func (api *API) requireMutationActor(
	writer http.ResponseWriter,
	request *http.Request,
) (platform.User, Repository, bool) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return platform.User{}, Repository{}, false
	}
	repo, ok := api.lookup(writer, request, &actor)
	if !ok {
		return platform.User{}, Repository{}, false
	}
	return actor, repo, true
}

// requireLevel writes a 403 response and returns false when the actor's
// permission is below the required level.
func requireLevel(writer http.ResponseWriter, access Access, level Permission) bool {
	if access.AtLeast(level) {
		return true
	}
	writeProblem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
	return false
}

// parseNumber extracts a positive int64 path value, writing a 404 on malformed
// input so that non-numeric identifiers do not leak existence.
func parseNumber(writer http.ResponseWriter, value string) (int64, bool) {
	number, ok := parseInt64(value)
	if !ok || number < 1 {
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
		return 0, false
	}
	return number, true
}

func parseInt64(value string) (int64, bool) {
	if value == "" {
		return 0, false
	}
	var result int64
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, false
		}
		result = result*10 + int64(character-'0')
		if result > 1<<62 {
			return 0, false
		}
	}
	return result, true
}

// validationError maps a validation sentinel error to a 400 problem response.
func validationError(writer http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, ErrBlankBody),
		errors.Is(err, ErrTitleTooLong),
		errors.Is(err, ErrBodyTooLong),
		errors.Is(err, ErrInvalidState),
		errors.Is(err, ErrInvalidColor),
		errors.Is(err, ErrInvalidLabel),
		errors.Is(err, ErrInvalidDecision),
		errors.Is(err, ErrInvalidPattern),
		errors.Is(err, ErrInvalidApprovals),
		errors.Is(err, ErrInvalidPrecondition):
		writeProblem(writer, http.StatusBadRequest, "invalid_input", err.Error())
		return true
	}
	return false
}
