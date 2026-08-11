package milestones

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
	base := "/api/v1/repositories/{owner}/{repository}"
	milestones := base + "/milestones"
	mux.HandleFunc("GET "+milestones, api.listMilestones)
	mux.HandleFunc("POST "+milestones, api.createMilestone)
	mux.HandleFunc("GET "+milestones+"/{number}", api.getMilestone)
	mux.HandleFunc("PATCH "+milestones+"/{number}", api.updateMilestone)
	mux.HandleFunc("DELETE "+milestones+"/{number}", api.deleteMilestone)
	mux.HandleFunc("PUT "+base+"/issues/{issueNumber}/milestone/{number}", api.assignIssue)
	mux.HandleFunc("DELETE "+base+"/issues/{issueNumber}/milestone", api.removeIssue)
}
