package discussions

import (
	"log/slog"
	"net/http"

	"github.com/lorehub/lorehub/services/api/internal/collab"
)

func Register(
	mux *http.ServeMux,
	store Store,
	repositories RepositoryStore,
	actors collab.ActorResolver,
	logger *slog.Logger,
) {
	api := NewAPI(store, repositories, actors, logger)
	base := "/api/v1/repositories/{owner}/{repository}/discussions"
	mux.HandleFunc("GET "+base, api.listDiscussions)
	mux.HandleFunc("POST "+base, api.createDiscussion)
	mux.HandleFunc("GET "+base+"/categories", api.listCategories)
	mux.HandleFunc("POST "+base+"/categories", api.createCategory)
	mux.HandleFunc("PATCH "+base+"/categories/{category}", api.updateCategory)
	mux.HandleFunc("DELETE "+base+"/{first}/{second}", api.deleteNested)
	mux.HandleFunc("GET "+base+"/{number}", api.getDiscussion)
	mux.HandleFunc("DELETE "+base+"/{number}", api.deleteDiscussion)
	mux.HandleFunc("PATCH "+base+"/{number}", api.updateDiscussion)
	mux.HandleFunc("PUT "+base+"/{number}/vote", api.addVote)
	mux.HandleFunc("DELETE "+base+"/{number}/vote", api.removeVote)
	mux.HandleFunc("POST "+base+"/{number}/comments", api.createComment)
	mux.HandleFunc("PATCH "+base+"/{number}/comments/{commentID}", api.updateComment)
	mux.HandleFunc("DELETE "+base+"/{number}/comments/{commentID}", api.deleteComment)
	mux.HandleFunc("PUT "+base+"/{number}/comments/{commentID}/answer", api.markAnswer)
	mux.HandleFunc("DELETE "+base+"/{number}/comments/{commentID}/answer", api.unmarkAnswer)
}
