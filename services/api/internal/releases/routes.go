package releases

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
	lore loreclient.Client,
	credentials loreclient.CredentialProvider,
	logger *slog.Logger,
) {
	api := NewAPI(store, repositories, actors, lore, credentials, logger)
	base := "/api/v1/repositories/{owner}/{repository}/releases"
	mux.HandleFunc("GET "+base, api.listReleases)
	mux.HandleFunc("POST "+base, api.createRelease)
	mux.HandleFunc("GET "+base+"/{releaseID}", api.getRelease)
	mux.HandleFunc("PATCH "+base+"/{releaseID}", api.updateRelease)
	mux.HandleFunc("DELETE "+base+"/{releaseID}", api.deleteRelease)
	mux.HandleFunc("POST "+base+"/{releaseID}/publish", api.publishRelease)
	mux.HandleFunc("POST "+base+"/{releaseID}/assets", api.addAsset)
	mux.HandleFunc("DELETE "+base+"/{releaseID}/assets/{assetID}", api.deleteAsset)
}
