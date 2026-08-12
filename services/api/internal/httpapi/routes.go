package httpapi

import (
	"net/http"

	branchesapi "github.com/lorehub/lorehub/services/api/internal/branches"
	codeapi "github.com/lorehub/lorehub/services/api/internal/code"
	"github.com/lorehub/lorehub/services/api/internal/collab"
	filelocksapi "github.com/lorehub/lorehub/services/api/internal/filelocks"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	mergeapi "github.com/lorehub/lorehub/services/api/internal/merge"
	milestonesapi "github.com/lorehub/lorehub/services/api/internal/milestones"
	projectsapi "github.com/lorehub/lorehub/services/api/internal/projects"
	releasesapi "github.com/lorehub/lorehub/services/api/internal/releases"
	reviewthreadsapi "github.com/lorehub/lorehub/services/api/internal/reviewthreads"
	webhooksapi "github.com/lorehub/lorehub/services/api/internal/webhooks"
	wikiapi "github.com/lorehub/lorehub/services/api/internal/wiki"
)

func (api *API) registerRoutes(mux *http.ServeMux) {
	api.registerCoreRoutes(mux)
	api.registerOrganizationRoutes(mux)
	api.registerRepositoryRoutes(mux)
	api.registerActionsRoutes(mux)
	api.registerCodeScanningRoutes(mux)
	api.registerFeatureRoutes(mux)
}

func (api *API) registerCoreRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health/live", api.live)
	mux.HandleFunc("GET /health/ready", api.ready)
	mux.HandleFunc("GET /auth/login", api.login)
	mux.HandleFunc("GET /auth/callback", api.callback)
	mux.HandleFunc("POST /auth/logout", api.logout)
	mux.HandleFunc("GET /api/v1/auth/session", api.session)
	mux.HandleFunc("GET /api/v1/auth/providers", api.providers)
	mux.HandleFunc("GET /.well-known/jwks.json", api.jwks)
	mux.HandleFunc("GET /auth/lore/confirm", api.loreAuthConfirm)
	mux.HandleFunc("POST /auth/lore/confirm", api.loreAuthConfirm)
	mux.HandleFunc("GET /api/v1/dashboard", api.dashboard)
	mux.HandleFunc("GET /api/v1/issues", api.globalIssues)
	mux.HandleFunc("GET /api/v1/pulls", api.globalPullRequests)
	mux.HandleFunc("GET /api/v1/search", api.search)
	mux.HandleFunc("GET /api/v1/users/{username}", api.userProfile)
	mux.HandleFunc("GET /api/v1/users/{username}/repositories", api.userRepositories)
	mux.HandleFunc("PATCH /api/v1/account/profile", api.updateProfile)
	mux.HandleFunc("GET /api/v1/account/personal-access-tokens", api.listPersonalAccessTokens)
	mux.HandleFunc("POST /api/v1/account/personal-access-tokens", api.createPersonalAccessToken)
	mux.HandleFunc(
		"DELETE /api/v1/account/personal-access-tokens/{tokenID}",
		api.revokePersonalAccessToken,
	)
	mux.HandleFunc("GET /api/v1/account/notification-preferences", api.notificationPreferences)
	mux.HandleFunc("PATCH /api/v1/account/notification-preferences", api.updateNotificationPreferences)
	mux.HandleFunc("GET /api/v1/notifications", api.notifications)
	mux.HandleFunc("GET /api/v1/notifications/unread-count", api.unreadNotificationCount)
	mux.HandleFunc("PATCH /api/v1/notifications/{notificationID}/read", api.markNotificationRead)
	mux.HandleFunc("POST /api/v1/notifications/read-all", api.markAllNotificationsRead)
	mux.HandleFunc("GET /api/v1/explore/repositories", api.exploreRepositories)
}

func (api *API) registerOrganizationRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/organizations", api.createOrganization)
	mux.HandleFunc("GET /api/v1/organizations/{organization}", api.organization)
	mux.HandleFunc("PATCH /api/v1/organizations/{organization}/settings", api.updateOrganization)
	mux.HandleFunc("GET /api/v1/organizations/{organization}/audit-log", api.organizationAuditLog)
	mux.HandleFunc("GET /api/v1/organizations/{organization}/actions/settings",
		api.listOrganizationActionsSettings)
	mux.HandleFunc("PUT /api/v1/organizations/{organization}/actions/settings/{valueKind}/{name}",
		api.upsertOrganizationActionsSetting)
	mux.HandleFunc("DELETE /api/v1/organizations/{organization}/actions/settings/{valueKind}/{name}",
		api.deleteOrganizationActionsSetting)
	mux.HandleFunc("GET /api/v1/organizations/{organization}/repositories", api.organizationRepositories)
	mux.HandleFunc(
		"GET /api/v1/organizations/{organization}/deleted-repositories",
		api.listDeletedRepositories,
	)
	mux.HandleFunc(
		"POST /api/v1/organizations/{organization}/deleted-repositories/{repository}/restore",
		api.restoreRepository,
	)
	mux.HandleFunc("GET /api/v1/organizations/{organization}/teams/{team}", api.team)
	mux.HandleFunc("PATCH /api/v1/organizations/{organization}/teams/{team}/settings", api.updateIdentityTeam)
	mux.HandleFunc("POST /api/v1/organizations/{organization}/teams/{team}/members", api.addTeamMember)
	mux.HandleFunc(
		"DELETE /api/v1/organizations/{organization}/teams/{team}/members/{username}",
		api.removeTeamMember,
	)
	mux.HandleFunc("POST /api/v1/organizations/{organization}/repositories", api.registerRepository)
	mux.HandleFunc("POST /api/v1/organizations/{organization}/repositories/import", api.importRepository)
}

func (api *API) registerRepositoryRoutes(mux *http.ServeMux) {
	base := "/api/v1/repositories/{owner}/{repository}"
	mux.HandleFunc("POST "+base+"/provision", api.retryRepositoryProvisioning)
	mux.HandleFunc("GET "+base, api.publicRepository)
	mux.HandleFunc("GET "+base+"/settings", api.repositorySettings)
	mux.HandleFunc("PATCH "+base+"/settings", api.updateRepositorySettings)
	mux.HandleFunc("PUT "+base+"/archive", api.archiveRepository)
	mux.HandleFunc("DELETE "+base+"/archive", api.unarchiveRepository)
	mux.HandleFunc("DELETE "+base, api.scheduleRepositoryDeletion)
	mux.HandleFunc("GET "+base+"/insights", api.repositoryInsights)
	mux.HandleFunc("GET "+base+"/branches", api.repositoryBranches)
	mux.HandleFunc("GET "+base+"/issues", api.listIssues)
	mux.HandleFunc("POST "+base+"/issues", api.createIssue)
	mux.HandleFunc("GET "+base+"/merge-requests", api.listMergeRequests)
	mux.HandleFunc("POST "+base+"/merge-requests", api.createMergeRequest)
}

func (api *API) registerActionsRoutes(mux *http.ServeMux) {
	base := "/api/v1/repositories/{owner}/{repository}/actions"
	mux.HandleFunc("GET "+base+"/runs", api.listCIRuns)
	mux.HandleFunc("GET "+base+"/settings", api.listRepositoryActionsSettings)
	mux.HandleFunc("PUT "+base+"/settings/{scopeKind}/{valueKind}/{name}",
		api.upsertRepositoryActionsSetting)
	mux.HandleFunc("DELETE "+base+"/settings/{scopeKind}/{valueKind}/{name}",
		api.deleteRepositoryActionsSetting)
	mux.HandleFunc("GET "+base+"/workflows", api.listActionWorkflows)
	mux.HandleFunc("GET "+base+"/runs/{runNumber}", api.actionRunDetail)
	mux.HandleFunc("POST "+base+"/workflows/{workflow}/dispatches", api.dispatchActionWorkflow)
	mux.HandleFunc("POST "+base+"/dispatches", api.dispatchRepositoryEvent)
	mux.HandleFunc("POST "+base+"/events/pull_request", api.dispatchPullRequestEvent)
	mux.HandleFunc("POST "+base+"/runs/{runNumber}/cancel", api.cancelActionRun)
	mux.HandleFunc("POST "+base+"/runs/{runNumber}/rerun", api.rerunActionRun)
	mux.HandleFunc("GET "+base+"/jobs/{jobID}/logs", api.actionJobLog)
	mux.HandleFunc("GET "+base+"/artifacts/{artifactID}", api.actionArtifact)
	mux.HandleFunc("GET "+base+"/artifacts/{artifactID}/download", api.actionArtifact)
}

func (api *API) registerCodeScanningRoutes(mux *http.ServeMux) {
	base := "/api/v1/repositories/{owner}/{repository}/code-scanning"
	mux.HandleFunc("POST /api/v3/repos/{owner}/{repository}/code-scanning/sarifs", api.uploadSARIF)
	mux.HandleFunc("GET "+base+"/sarif-uploads", api.listSARIFUploads)
	mux.HandleFunc("GET "+base+"/sarif-uploads/{uploadID}", api.getSARIFUpload)
	mux.HandleFunc("GET "+base+"/alerts", api.listCodeScanningAlerts)
}

func (api *API) registerFeatureRoutes(mux *http.ServeMux) {
	if api.webhooksStore != nil {
		webhooksapi.Register(mux, api.webhooksStore, api, api.logger)
	}
	if api.collabStore != nil {
		collab.Register(mux, api.collabStore, api, api.logger)
		branchClient, branchClientOK := api.lore.(branchesapi.LoreClient)
		if branchClientOK && api.branchObservations != nil {
			branchesapi.Register(
				mux, api.collabStore, api.branchObservations, api,
				branchClient, api.loreCredentials, api.logger,
			)
		}
		if api.projectsStore != nil {
			projectsapi.Register(mux, api.projectsStore, api.collabStore, api, api.logger)
		}
		if api.releasesStore != nil {
			releasesapi.Register(
				mux, api.releasesStore, api.collabStore, api,
				api.lore, api.loreCredentials, api.logger,
			)
		}
		if api.milestonesStore != nil {
			milestonesapi.Register(mux, api.milestonesStore, api.collabStore, api, api.logger)
		}
		if api.wikiStore != nil {
			wikiapi.Register(mux, api.wikiStore, api.collabStore, api, api.logger)
		}
		if codeClient, ok := api.lore.(loreclient.CodeClient); ok {
			codeapi.Register(mux, api.collabStore, api.lore, codeClient, api, api.loreCredentials,
				api.serviceSubjects.PublicReader, api.logger)
			if api.reviewThreadsStore != nil {
				reviewthreadsapi.Register(
					mux, api.reviewThreadsStore, api.collabStore, api, codeClient,
					api.loreCredentials, api.logger,
				)
			}
		}
		if lockClient, ok := api.lore.(filelocksapi.LoreClient); ok &&
			api.fileLockUsers != nil && api.fileLockObservations != nil {
			filelocksapi.Register(
				mux, api.collabStore, api.fileLockUsers, api.fileLockObservations, api,
				lockClient, api.loreCredentials, api.serviceSubjects.PublicReader, api.logger,
			)
		}
		if workflow, ok := api.collabStore.(collab.MergeWorkflowStore); ok {
			if mergeClient, mergeOK := api.lore.(loreclient.MergeClient); mergeOK {
				pushAuthorizer, _ := api.collabStore.(loreclient.PushAuthorizer)
				mergeAuthorization, _ := api.authorization.(mergeapi.MergeAuthorizationStore)
				mergeapi.Register(mux, api.collabStore, workflow, api.lore, mergeClient, api,
					api.loreCredentials, pushAuthorizer, mergeAuthorization, api.logger)
			}
		}
	}
	if api.authorization != nil {
		registerAuthorizationRoutes(mux, api)
	}
}
