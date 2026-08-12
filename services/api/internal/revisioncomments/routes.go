package revisioncomments

import (
	"log/slog"
	"net/http"

	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
)

func Register(
	mux *http.ServeMux,
	store collab.RevisionCommentStore,
	repositories RepositoryStore,
	actors collab.ActorResolver,
	code loreclient.CodeClient,
	credentials loreclient.CredentialProvider,
	publicReaderSubject string,
	logger *slog.Logger,
) {
	api := NewAPI(
		store, repositories, actors, code, credentials,
		publicReaderSubject, logger,
	)
	base := "/api/v1/repositories/{owner}/{repository}/revisions/{revision}/comments"
	mux.HandleFunc("GET "+base, api.list)
	mux.HandleFunc("POST "+base, api.create)
	mux.HandleFunc("PATCH "+base+"/{commentID}", api.update)
	mux.HandleFunc("DELETE "+base+"/{commentID}", api.delete)
}
