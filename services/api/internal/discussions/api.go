package discussions

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

type RepositoryStore interface {
	LookupRepository(context.Context, *platform.User, string, string) (collab.Repository, error)
	RepositoryPermission(context.Context, platform.User, collab.Repository) (collab.Access, error)
}

type API struct {
	store        Store
	repositories RepositoryStore
	actors       collab.ActorResolver
	logger       *slog.Logger
}

func NewAPI(
	store Store,
	repositories RepositoryStore,
	actors collab.ActorResolver,
	logger *slog.Logger,
) *API {
	return &API{store: store, repositories: repositories, actors: actors, logger: logger}
}

func (api *API) lookup(
	writer http.ResponseWriter,
	request *http.Request,
	actor *platform.User,
) (collab.Repository, bool) {
	repository, err := api.repositories.LookupRepository(
		request.Context(), actor, request.PathValue("owner"), request.PathValue("repository"),
	)
	if err != nil {
		api.storeError(writer, request, "lookup discussions repository", err)
		return collab.Repository{}, false
	}
	return repository, true
}

func (api *API) optionalRepository(
	writer http.ResponseWriter,
	request *http.Request,
) (*platform.User, collab.Repository, bool) {
	actor, ok := api.actors.ResolveOptionalActor(writer, request)
	if !ok {
		return nil, collab.Repository{}, false
	}
	repository, ok := api.lookup(writer, request, actor)
	return actor, repository, ok
}

func (api *API) requiredRepository(
	writer http.ResponseWriter,
	request *http.Request,
) (platform.User, collab.Repository, bool) {
	actor, ok := api.actors.ResolveActor(writer, request)
	if !ok {
		return platform.User{}, collab.Repository{}, false
	}
	repository, ok := api.lookup(writer, request, &actor)
	return actor, repository, ok
}

func (api *API) access(
	writer http.ResponseWriter,
	request *http.Request,
	actor *platform.User,
	repository collab.Repository,
) (collab.Access, bool) {
	if actor == nil {
		return collab.Access{}, true
	}
	access, err := api.repositories.RepositoryPermission(request.Context(), *actor, repository)
	if err != nil {
		api.storeError(writer, request, "resolve discussion permission", err)
		return collab.Access{}, false
	}
	return access, true
}

func decoratePage(
	page *Page,
	actor *platform.User,
	access collab.Access,
	archived bool,
) {
	page.ViewerCanCreate = actor != nil && !archived
	for index := range page.Discussions {
		decorateSummary(&page.Discussions[index], actor, access, archived)
	}
}

func decorateDiscussion(
	discussion *Discussion,
	actor *platform.User,
	access collab.Access,
	archived bool,
) {
	decorateSummary(&discussion.Summary, actor, access, archived)
	moderator := actor != nil && access.AtLeast(collab.PermWrite) && !archived
	participant := actor != nil && !archived
	discussion.ViewerCanComment =
		(participant && discussion.State == "open" && !discussion.Locked) || moderator
	for index := range discussion.Comments {
		decorateComment(&discussion.Comments[index], discussion.Summary, actor, access, archived)
	}
}

func decorateComment(
	comment *Comment,
	discussion Summary,
	actor *platform.User,
	access collab.Access,
	archived bool,
) {
	moderator := actor != nil && access.AtLeast(collab.PermWrite) && !archived
	comment.ViewerCanEdit = actor != nil && !archived &&
		((comment.Author.ID == actor.ID && discussion.State == "open" && !discussion.Locked) || moderator)
	comment.ViewerCanDelete = actor != nil && !archived &&
		((comment.Author.ID == actor.ID && !discussion.Locked) || moderator)
	comment.ViewerCanMarkAnswer = actor != nil && !archived &&
		discussion.Category.Format == "question" &&
		(discussion.Author.ID == actor.ID || moderator)
}

func decorateSummary(
	summary *Summary,
	actor *platform.User,
	access collab.Access,
	archived bool,
) {
	moderator := actor != nil && access.AtLeast(collab.PermWrite) && !archived
	summary.ViewerCanEdit = actor != nil && !archived &&
		(moderator || (summary.Author.ID == actor.ID && !summary.Locked))
	summary.ViewerCanModerate = moderator
	summary.ViewerCanVote = actor != nil && !archived
}

func repositoryRef(repository collab.Repository) RepositoryRef {
	return RepositoryRef{ID: repository.ID, OrganizationID: repository.OrganizationID}
}
