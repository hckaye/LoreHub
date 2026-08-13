package collab

import (
	"net/http"

	"github.com/lorehub/lorehub/services/api/internal/platform"
)

func (api *API) putReaction(writer http.ResponseWriter, request *http.Request) {
	api.mutateReaction(writer, request, true)
}

func (api *API) deleteReaction(writer http.ResponseWriter, request *http.Request) {
	api.mutateReaction(writer, request, false)
}

func (api *API) mutateReaction(writer http.ResponseWriter, request *http.Request, enabled bool) {
	actor, repo, ok := api.requireMutationActor(writer, request)
	if !ok {
		return
	}
	access, ok := api.permission(writer, request, actor, repo)
	if !ok || !requireLevel(writer, access, PermRead) {
		return
	}
	store, ok := api.store.(ReactionStore)
	if !ok {
		writeProblem(writer, http.StatusServiceUnavailable, "reactions_unavailable", "Reactions are unavailable")
		return
	}
	var input ReactionInput
	if !decodeJSON(writer, request, &input) {
		return
	}
	input, err := normalizeReactionInput(input)
	if err != nil {
		validationError(writer, err)
		return
	}
	var result ReactionMutation
	if enabled {
		result, err = store.PutReaction(requestContext(request), actor, repo.ID, input)
	} else {
		result, err = store.DeleteReaction(requestContext(request), actor, repo.ID, input)
	}
	if err != nil {
		storeError(writer, request, "mutate reaction", err, api.logger)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func reactionViewer(actor *platform.User) string {
	if actor == nil {
		return ""
	}
	return actor.Username
}

func ensureReactions(reactions []Reaction) []Reaction {
	if reactions == nil {
		return []Reaction{}
	}
	return reactions
}
