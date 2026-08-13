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
//	GET    /api/v1/repositories/{owner}/{repository}/issues/{number}/events
//	GET    /api/v1/repositories/{owner}/{repository}/issues/{number}/comments
//	POST   /api/v1/repositories/{owner}/{repository}/issues/{number}/comments
//	PATCH  /api/v1/repositories/{owner}/{repository}/issues/{number}/comments/{commentID}
//	DELETE /api/v1/repositories/{owner}/{repository}/issues/{number}/comments/{commentID}
//	PUT    /api/v1/repositories/{owner}/{repository}/reactions
//	DELETE /api/v1/repositories/{owner}/{repository}/reactions
//	GET    /api/v1/repositories/{owner}/{repository}/labels
//	POST   /api/v1/repositories/{owner}/{repository}/labels
//	PATCH  /api/v1/repositories/{owner}/{repository}/labels/{labelID}
//	DELETE /api/v1/repositories/{owner}/{repository}/labels/{labelID}
//	PUT    /api/v1/repositories/{owner}/{repository}/issues/{number}/labels/{labelID}
//	DELETE /api/v1/repositories/{owner}/{repository}/issues/{number}/labels/{labelID}
//	GET    /api/v1/repositories/{owner}/{repository}/merge-requests/{number}
//	PATCH  /api/v1/repositories/{owner}/{repository}/merge-requests/{number}
//	PUT    /api/v1/repositories/{owner}/{repository}/merge-requests/{number}/draft
//	DELETE /api/v1/repositories/{owner}/{repository}/merge-requests/{number}/draft
//	GET    /api/v1/repositories/{owner}/{repository}/merge-requests/{number}/events
//	GET    /api/v1/repositories/{owner}/{repository}/merge-requests/{number}/reviews
//	POST   /api/v1/repositories/{owner}/{repository}/merge-requests/{number}/reviews
//	PUT    /api/v1/repositories/{owner}/{repository}/star
//	DELETE /api/v1/repositories/{owner}/{repository}/star
//	PUT    /api/v1/repositories/{owner}/{repository}/watch
//	DELETE /api/v1/repositories/{owner}/{repository}/watch
//	GET    /api/v1/repositories/{owner}/{repository}/branch-rules
//	POST   /api/v1/repositories/{owner}/{repository}/branch-rules
//	PATCH  /api/v1/repositories/{owner}/{repository}/branch-rules/{ruleID}
//	DELETE /api/v1/repositories/{owner}/{repository}/branch-rules/{ruleID}
func Register(mux *http.ServeMux, store Store, actors ActorResolver, logger *slog.Logger) {
	api := NewAPI(store, actors, logger)
	base := "/api/v1/repositories/{owner}/{repository}"

	mux.HandleFunc("GET "+base+"/issues/{number}", api.getIssue)
	mux.HandleFunc("PATCH "+base+"/issues/{number}", api.patchIssue)
	mux.HandleFunc("GET "+base+"/assignees", api.listAssignableUsers)
	mux.HandleFunc("PUT "+base+"/issues/{number}/assignees/{username}", api.putIssueAssignee)
	mux.HandleFunc("DELETE "+base+"/issues/{number}/assignees/{username}", api.deleteIssueAssignee)

	mux.HandleFunc("GET "+base+"/issues/{number}/comments", api.listIssueComments)
	mux.HandleFunc("POST "+base+"/issues/{number}/comments", api.createIssueComment)
	mux.HandleFunc("PATCH "+base+"/issues/{number}/comments/{commentID}", api.patchIssueComment)
	mux.HandleFunc("DELETE "+base+"/issues/{number}/comments/{commentID}", api.deleteIssueComment)
	mux.HandleFunc("PUT "+base+"/reactions", api.putReaction)
	mux.HandleFunc("DELETE "+base+"/reactions", api.deleteReaction)

	mux.HandleFunc("GET "+base+"/issues/{number}/events", api.listIssueEvents)

	mux.HandleFunc("GET "+base+"/labels", api.listLabels)
	mux.HandleFunc("POST "+base+"/labels", api.createLabel)
	mux.HandleFunc("PATCH "+base+"/labels/{labelID}", api.patchLabel)
	mux.HandleFunc("DELETE "+base+"/labels/{labelID}", api.deleteLabel)

	mux.HandleFunc("PUT "+base+"/issues/{number}/labels/{labelID}", api.putIssueLabel)
	mux.HandleFunc("DELETE "+base+"/issues/{number}/labels/{labelID}", api.deleteIssueLabel)

	mux.HandleFunc("GET "+base+"/merge-requests/{number}", api.getMergeRequest)
	mux.HandleFunc("PATCH "+base+"/merge-requests/{number}", api.patchMergeRequest)
	mux.HandleFunc("GET "+base+"/merge-requests/{number}/comments", api.listMergeRequestComments)
	mux.HandleFunc("POST "+base+"/merge-requests/{number}/comments", api.createMergeRequestComment)
	mux.HandleFunc("PATCH "+base+"/merge-requests/{number}/comments/{commentID}", api.patchMergeRequestComment)
	mux.HandleFunc("DELETE "+base+"/merge-requests/{number}/comments/{commentID}", api.deleteMergeRequestComment)
	mux.HandleFunc("GET "+base+"/merge-requests/{number}/events", api.listMergeRequestEvents)
	mux.HandleFunc("GET "+base+"/merge-requests/{number}/reviews", api.listReviews)
	mux.HandleFunc("POST "+base+"/merge-requests/{number}/reviews", api.createReview)
	mux.HandleFunc("GET "+base+"/merge-requests/{number}/review-requests", api.listReviewRequests)
	mux.HandleFunc("GET "+base+"/merge-requests/{number}/review-candidates", api.listReviewCandidates)
	mux.HandleFunc("PUT "+base+"/merge-requests/{number}/review-requests/users/{username}",
		api.putUserReviewRequest)
	mux.HandleFunc("DELETE "+base+"/merge-requests/{number}/review-requests/users/{username}",
		api.deleteUserReviewRequest)
	mux.HandleFunc("PUT "+base+"/merge-requests/{number}/review-requests/teams/{team}",
		api.putTeamReviewRequest)
	mux.HandleFunc("DELETE "+base+"/merge-requests/{number}/review-requests/teams/{team}",
		api.deleteTeamReviewRequest)
	mux.HandleFunc("GET "+base+"/merge-requests/{number}/metadata", api.getMergeRequestMetadata)
	mux.HandleFunc("PUT "+base+"/merge-requests/{number}/metadata/labels/{labelID}",
		api.putMergeRequestLabel)
	mux.HandleFunc("DELETE "+base+"/merge-requests/{number}/metadata/labels/{labelID}",
		api.deleteMergeRequestLabel)
	mux.HandleFunc("PUT "+base+"/merge-requests/{number}/metadata/assignees/{username}",
		api.putMergeRequestAssignee)
	mux.HandleFunc("DELETE "+base+"/merge-requests/{number}/metadata/assignees/{username}",
		api.deleteMergeRequestAssignee)
	mux.HandleFunc("PUT "+base+"/merge-requests/{number}/metadata/milestone/{milestoneNumber}",
		api.putMergeRequestMilestone)
	mux.HandleFunc("DELETE "+base+"/merge-requests/{number}/metadata/milestone",
		api.deleteMergeRequestMilestone)
	mux.HandleFunc("PUT "+base+"/merge-requests/{number}/draft", api.putMergeRequestDraft)
	mux.HandleFunc("DELETE "+base+"/merge-requests/{number}/draft", api.deleteMergeRequestDraft)

	mux.HandleFunc("PUT "+base+"/star", api.putRepositoryStar)
	mux.HandleFunc("DELETE "+base+"/star", api.deleteRepositoryStar)
	mux.HandleFunc("PUT "+base+"/watch", api.putRepositoryWatch)
	mux.HandleFunc("DELETE "+base+"/watch", api.deleteRepositoryWatch)

	mux.HandleFunc("GET "+base+"/branch-rules", api.listBranchRules)
	mux.HandleFunc("POST "+base+"/branch-rules", api.createBranchRule)
	mux.HandleFunc("PATCH "+base+"/branch-rules/{ruleID}", api.patchBranchRule)
	mux.HandleFunc("DELETE "+base+"/branch-rules/{ruleID}", api.deleteBranchRule)
}
