package projects

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
	base := "/api/v1/repositories/{owner}/{repository}/projects"
	mux.HandleFunc("GET "+base, api.listProjects)
	mux.HandleFunc("POST "+base, api.createProject)
	mux.HandleFunc("GET "+base+"/{number}", api.getProject)
	mux.HandleFunc("PATCH "+base+"/{number}", api.updateProject)
	mux.HandleFunc("DELETE "+base+"/{number}", api.deleteProject)
	mux.HandleFunc("POST "+base+"/{number}/columns", api.createColumn)
	mux.HandleFunc("PATCH "+base+"/{number}/columns/{columnID}", api.updateColumn)
	mux.HandleFunc("DELETE "+base+"/{number}/columns/{columnID}", api.deleteColumn)
	mux.HandleFunc("POST "+base+"/{number}/items", api.createItem)
	mux.HandleFunc("PATCH "+base+"/{number}/items/{itemID}", api.updateItem)
	mux.HandleFunc("DELETE "+base+"/{number}/items/{itemID}", api.deleteItem)
}
