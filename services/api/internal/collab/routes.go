package collab

import (
	"log/slog"
	"net/http"
)

// Register mounts all collaboration routes onto the provided mux, constructing
// the collaboration API from the supplied store, authenticator and logger.
// Routes use the Go 1.22+ method-pattern ServeMux so path parameters are
// available via request.PathValue. The owner/repository path segment names
// match the rest of the API for consistency.
//
// Mounted routes:
//
//	GET    /api/v1/repositories/{owner}/{repository}/issues/{number}
//	PATCH  /api/v1/repositories/{owner}/{repository}/issues/{number}
//	GET    /api/v1/repositories/{owner}/{repository}/issues/{number}/comments
//	POST   /api/v1/repositories/{owner}/{repository}/issues/{number}/comments
//	PATCH  /api/v1/repositories/{owner}/{repository}/issues/{number}/comments/{commentID}
//	DELETE /api/v1/repositories/{owner}/{repository}/issues/{number}/comments/{commentID}
//	GET    /api/v1/repositories/{owner}/{repository}/labels
//	POST   /api/v1/repositories/{owner}/{repository}/labels
//	PATCH  /api/v1/repositories/{owner}/{repository}/labels/{labelID}
//	DELETE /api/v1/repositories/{owner}/{repository}/labels/{labelID}
//	PUT    /api/v1/repositories/{owner}/{repository}/issues/{number}/labels/{labelID}
//	DELETE /api/v1/repositories/{owner}/{repository}/issues/{number}/labels/{labelID}
//	GET    /api/v1/repositories/{owner}/{repository}/merge-requests/{number}
//	PATCH  /api/v1/repositories/{owner}/{repository}/merge-requests/{number}
//	GET    /api/v1/repositories/{owner}/{repository}/merge-requests/{number}/reviews
//	POST   /api/v1/repositories/{owner}/{repository}/merge-requests/{number}/reviews
//	GET    /api/v1/repositories/{owner}/{repository}/branch-rules
//	POST   /api/v1/repositories/{owner}/{repository}/branch-rules
//	PATCH  /api/v1/repositories/{owner}/{repository}/branch-rules/{ruleID}
//	DELETE /api/v1/repositories/{owner}/{repository}/branch-rules/{ruleID}
func Register(mux *http.ServeMux, store Store, actors ActorResolver, logger *slog.Logger) {
	api := NewAPI(store, actors, logger)
	base := "/api/v1/repositories/{owner}/{repository}"

	mux.HandleFunc("GET "+base+"/issues/{number}", api.getIssue)
	mux.HandleFunc("PATCH "+base+"/issues/{number}", api.patchIssue)

	mux.HandleFunc("GET "+base+"/issues/{number}/comments", api.listIssueComments)
	mux.HandleFunc("POST "+base+"/issues/{number}/comments", api.createIssueComment)
	mux.HandleFunc("PATCH "+base+"/issues/{number}/comments/{commentID}", api.patchIssueComment)
	mux.HandleFunc("DELETE "+base+"/issues/{number}/comments/{commentID}", api.deleteIssueComment)

	mux.HandleFunc("GET "+base+"/labels", api.listLabels)
	mux.HandleFunc("POST "+base+"/labels", api.createLabel)
	mux.HandleFunc("PATCH "+base+"/labels/{labelID}", api.patchLabel)
	mux.HandleFunc("DELETE "+base+"/labels/{labelID}", api.deleteLabel)

	mux.HandleFunc("PUT "+base+"/issues/{number}/labels/{labelID}", api.putIssueLabel)
	mux.HandleFunc("DELETE "+base+"/issues/{number}/labels/{labelID}", api.deleteIssueLabel)

	mux.HandleFunc("GET "+base+"/merge-requests/{number}", api.getMergeRequest)
	mux.HandleFunc("PATCH "+base+"/merge-requests/{number}", api.patchMergeRequest)
	mux.HandleFunc("GET "+base+"/merge-requests/{number}/reviews", api.listReviews)
	mux.HandleFunc("POST "+base+"/merge-requests/{number}/reviews", api.createReview)

	mux.HandleFunc("GET "+base+"/branch-rules", api.listBranchRules)
	mux.HandleFunc("POST "+base+"/branch-rules", api.createBranchRule)
	mux.HandleFunc("PATCH "+base+"/branch-rules/{ruleID}", api.patchBranchRule)
	mux.HandleFunc("DELETE "+base+"/branch-rules/{ruleID}", api.deleteBranchRule)
}
