package wiki

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
	base := "/api/v1/repositories/{owner}/{repository}/wiki"
	mux.HandleFunc("GET "+base, api.listPages)
	mux.HandleFunc("POST "+base, api.createPage)
	mux.HandleFunc("GET "+base+"/{slug}", api.getPage)
	mux.HandleFunc("PATCH "+base+"/{slug}", api.updatePage)
	mux.HandleFunc("DELETE "+base+"/{slug}", api.deletePage)
	mux.HandleFunc("GET "+base+"/{slug}/history", api.pageHistory)
	mux.HandleFunc("GET "+base+"/{slug}/history/{version}", api.pageRevision)
}
