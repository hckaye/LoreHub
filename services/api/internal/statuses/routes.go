package statuses

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
	lore loreclient.CodeClient,
	credentials loreclient.CredentialProvider,
	logger *slog.Logger,
) {
	api := NewAPI(store, repositories, actors, lore, credentials, logger)
	base := "/api/v1/repositories/{owner}/{repository}/revisions/{revision}/statuses"
	mux.HandleFunc("GET "+base, api.listStatuses)
	mux.HandleFunc("POST "+base, api.createStatus)
	mux.HandleFunc(
		"POST /api/v3/repos/{owner}/{repository}/statuses/{revision}",
		api.createGitHubStatus,
	)
}
