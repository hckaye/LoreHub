package reviewthreads

import (
	"log/slog"
	"net/http"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
)

func Register(
	mux *http.ServeMux,
	store Store,
	repositories RepositoryStore,
	actors collab.ActorResolver,
	code loreclient.CodeClient,
	credentials loreclient.CredentialProvider,
	logger *slog.Logger,
) {
	api := NewAPI(store, repositories, actors, code, credentials, logger)
	base := "/api/v1/repositories/{owner}/{repository}/merge-requests/{number}/review-threads"
	mux.HandleFunc("GET "+base, api.listThreads)
	mux.HandleFunc("POST "+base, api.createThread)
	mux.HandleFunc("PATCH "+base+"/{threadID}", api.updateThread)
	mux.HandleFunc("POST "+base+"/{threadID}/comments", api.reply)
	mux.HandleFunc("PATCH "+base+"/{threadID}/comments/{commentID}", api.updateComment)
	mux.HandleFunc("DELETE "+base+"/{threadID}/comments/{commentID}", api.deleteComment)
}
