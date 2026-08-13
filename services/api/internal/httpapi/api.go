package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/authz"
	branchesapi "github.com/lorehub/lorehub/services/api/internal/branches"
	"github.com/lorehub/lorehub/services/api/internal/collab"
	discussionsapi "github.com/lorehub/lorehub/services/api/internal/discussions"
	filelocksapi "github.com/lorehub/lorehub/services/api/internal/filelocks"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/loreauth"
	milestonesapi "github.com/lorehub/lorehub/services/api/internal/milestones"
	"github.com/lorehub/lorehub/services/api/internal/platform"
	projectsapi "github.com/lorehub/lorehub/services/api/internal/projects"
	releasesapi "github.com/lorehub/lorehub/services/api/internal/releases"
	reviewthreadsapi "github.com/lorehub/lorehub/services/api/internal/reviewthreads"
	"github.com/lorehub/lorehub/services/api/internal/runner"
	statusesapi "github.com/lorehub/lorehub/services/api/internal/statuses"
	wikiapi "github.com/lorehub/lorehub/services/api/internal/wiki"
)

const maxRequestBody = 1 << 20

type Store interface {
	EnsureUser(ctx context.Context, principal auth.Principal) (platform.User, error)
	ActiveUser(ctx context.Context, userID string) (platform.User, error)
	CreateOrganization(
		ctx context.Context,
		actor platform.User,
		input platform.CreateOrganizationInput,
	) (platform.Organization, error)
	RegisterRepository(
		ctx context.Context,
		actor platform.User,
		organizationSlug string,
		input platform.RegisterRepositoryInput,
	) (platform.Repository, error)
	ExploreRepositories(ctx context.Context, limit int) ([]platform.Repository, error)
	PublicRepository(ctx context.Context, owner string, slug string) (platform.Repository, error)
	RepositoryForWrite(
		ctx context.Context,
		actor platform.User,
		owner string,
		slug string,
	) (platform.Repository, error)
	CreateIssue(
		ctx context.Context,
		actor platform.User,
		owner string,
		slug string,
		input platform.CreateIssueInput,
	) (platform.Issue, error)
	CreateMergeRequest(
		ctx context.Context,
		actor platform.User,
		owner string,
		slug string,
		input platform.CreateMergeRequestInput,
	) (platform.MergeRequest, error)
	ListPublicCIRuns(ctx context.Context, owner string, slug string) ([]platform.CIRun, error)
}

type repositoryProvisioningStore interface {
	BeginRepositoryProvisioning(
		context.Context, platform.User, string, platform.ProvisionRepositoryInput, string,
	) (platform.Repository, error)
	RepositoryForProvisioning(context.Context, platform.User, string, string) (platform.Repository, error)
	MarkRepositoryProvisioned(context.Context, platform.User, string) error
	MarkRepositoryProvisioningFailed(context.Context, platform.User, string, string) error
}

type AuthorizationStore interface {
	authz.Store
	ListOrganizationMembers(context.Context, platform.User, string) ([]platform.OrganizationMember, error)
	SetOrganizationMember(
		context.Context, platform.User, string, platform.SetOrganizationMemberInput,
	) (platform.OrganizationMember, error)
	ListTeams(context.Context, platform.User, string) ([]platform.Team, error)
	CreateTeam(context.Context, platform.User, string, platform.SetTeamInput) (platform.Team, error)
	UpdateTeam(context.Context, platform.User, string, string, platform.SetTeamInput) (platform.Team, error)
	DeleteTeam(context.Context, platform.User, string, string) error
	ListTeamMembers(context.Context, platform.User, string, string) ([]platform.TeamMember, error)
	SetTeamMember(
		context.Context, platform.User, string, string, platform.SetTeamMemberInput,
	) (platform.TeamMember, error)
	SetTeamRepositoryRole(
		context.Context, platform.User, string, string, string, string, platform.SetTeamRepositoryRoleInput,
	) (platform.TeamRepositoryRole, error)
	DeleteTeamRepositoryRole(context.Context, platform.User, string, string, string, string) error
	ListRepositoryCollaborators(context.Context, platform.User, string, string) ([]platform.Collaborator, error)
	ListRepositoryInvitations(
		context.Context, platform.User, string, string, int, int,
	) (platform.RepositoryInvitationPage, error)
	ListRepositoryInvitationsForUser(
		context.Context, platform.User, int, int,
	) (platform.RepositoryInvitationPage, error)
	CreateRepositoryInvitation(
		context.Context, platform.User, string, string, platform.CreateRepositoryInvitationInput,
	) (platform.RepositoryInvitation, error)
	RevokeRepositoryInvitation(context.Context, platform.User, string, string, string) error
	RespondRepositoryInvitation(
		context.Context, platform.User, string, bool,
	) (platform.RepositoryInvitation, error)
	RevokeRepositoryCollaborator(
		context.Context, platform.User, string, string, string,
	) (platform.Collaborator, error)
	UpdateRepositoryCollaboratorRole(
		context.Context, platform.User, string, string, string, string,
	) (platform.Collaborator, error)
	SetRepositoryPolicy(
		context.Context, platform.User, string, string, platform.SetRepositoryPolicyInput,
	) error
	GetRepositoryPolicy(context.Context, platform.User, string, string) (platform.RepositoryPolicy, error)
	SetObliterateGrant(context.Context, platform.User, string, string, string, bool) error
	SetServicePrincipalGrant(
		context.Context, platform.User, string, string, string, []string, bool,
	) error
	DeclareRepositoryLink(
		context.Context, platform.User, string, string, string, string,
	) (platform.RepositoryLink, error)
	ListRepositoryLinks(context.Context, platform.User, string, string) ([]platform.RepositoryLink, error)
}

type HealthChecker interface {
	Ping(ctx context.Context) error
}

type PersonalAccessTokenStore interface {
	ListPersonalAccessTokens(context.Context, platform.User) ([]platform.PersonalAccessToken, error)
	CreatePersonalAccessToken(
		context.Context,
		platform.User,
		platform.CreatePersonalAccessTokenInput,
	) (platform.PersonalAccessToken, error)
	RevokePersonalAccessToken(context.Context, platform.User, string) error
}

type EntitlementStore interface {
	Grant(
		context.Context, platform.User, platform.EntitlementSubject, string,
	) (platform.Entitlement, error)
	Revoke(context.Context, platform.User, platform.EntitlementSubject, string) error
	List(context.Context) ([]platform.Entitlement, error)
}

type API struct {
	store                   Store
	actions                 ActionsStore
	actionsEnvironments     ActionsEnvironmentStore
	actionsExecutionContext ActionsExecutionContextStore
	actionsSecurity         ActionsSecurityStore
	actionsJobTokens        runner.JobTokenVerifier
	lore                    loreclient.Client
	authenticator           auth.Authenticator
	health                  HealthChecker
	loreIdentity            string
	allowLegacyLoreIdentity bool
	serviceSubjects         loreclient.ServiceSubjects
	loreCredentials         loreclient.CredentialProvider
	managedLoreClient       loreclient.ManagedRepositoryClient
	authorization           AuthorizationStore
	loreAuth                *loreauth.Service
	logger                  *slog.Logger
	collabStore             collab.Store
	branchObservations      branchesapi.ObservationStore
	fileLockUsers           filelocksapi.UserDirectory
	fileLockObservations    filelocksapi.ObservationStore
	projectsStore           projectsapi.Store
	discussionsStore        discussionsapi.Store
	releasesStore           releasesapi.Store
	milestonesStore         milestonesapi.Store
	wikiStore               wikiapi.Store
	reviewThreadsStore      reviewthreadsapi.Store
	statusesStore           statusesapi.Store
	loginProvider           auth.LoginProvider
	loginStore              auth.LoginTransactionStore
	sessionStore            auth.SessionStore
	cleanupStore            auth.CleanupStore
	secrets                 *auth.SecretCodec
	publicOrigin            string
	cookie                  sessionCookieConfig
	sessionTTL              time.Duration
	transactionTTL          time.Duration
	identityStore           IdentityStore
	loginProviders          []string
	webhooksStore           webhooksManager
	personalAccessTokens    PersonalAccessTokenStore
	entitlements            EntitlementStore
	runners                 RunnerStore
	runnerSecrets           *auth.SecretCodec
	runnerCredentialKeyID   string
	loreServers             LoreServerStore
	loresSecrets            *auth.SecretCodec
	loresTokenKeyID         string
	loreAllowPrivateServers bool
	loreServerCertificates  LoreServerCertificateStore
	loreServerCertIssuer    LoreServerCertificateIssuer
	loreHookServers         loreHookServerStore
	runnerControl           RunnerControlStore
	runnerExecutionContext  runner.ExecutionContextResolver
	runnerJobTokenIssuer    runner.JobTokenIssuer
	runnerControlConfig     RunnerControlConfig
	instanceAdminUsernames  map[string]struct{}
	globalWorkItems         GlobalWorkItemStore
	deletionRetention       time.Duration
}

func WithRepositoryDeletion(retention time.Duration) Option {
	return func(api *API) {
		api.deletionRetention = retention
	}
}

func New(
	store Store,
	lore loreclient.Client,
	authenticator auth.Authenticator,
	health HealthChecker,
	loreIdentity string,
	logger *slog.Logger,
	options ...Option,
) http.Handler {
	api := &API{
		store:         store,
		lore:          lore,
		authenticator: authenticator,
		health:        health,
		loreIdentity:  loreIdentity,
		logger:        logger,
	}
	for _, option := range options {
		if option != nil {
			option(api)
		}
	}
	if managedClient, ok := lore.(loreclient.ManagedRepositoryClient); ok {
		api.managedLoreClient = managedClient
	}
	mux := http.NewServeMux()
	api.registerRoutes(mux)
	return api.recoverPanic(api.securityHeaders(api.requestLog(mux)))
}

// WithCollaboration mounts the collaboration API using the same actor and
// session resolver as the rest of this HTTP API.
func WithCollaboration(store collab.Store) Option {
	return func(api *API) {
		api.collabStore = store
	}
}

func WithReviewThreads(store reviewthreadsapi.Store) Option {
	return func(api *API) {
		api.reviewThreadsStore = store
	}
}

func WithBranchObservations(store branchesapi.ObservationStore) Option {
	return func(api *API) {
		api.branchObservations = store
	}
}

func WithFileLocks(users filelocksapi.UserDirectory, observations filelocksapi.ObservationStore) Option {
	return func(api *API) {
		api.fileLockUsers = users
		api.fileLockObservations = observations
	}
}

func WithProjects(store projectsapi.Store) Option {
	return func(api *API) {
		api.projectsStore = store
	}
}

func WithLoreCredentials(provider loreclient.CredentialProvider) Option {
	return func(api *API) {
		api.loreCredentials = provider
	}
}

func WithActionsSecurity(store ActionsSecurityStore, verifier runner.JobTokenVerifier) Option {
	return func(api *API) {
		api.actionsSecurity = store
		api.actionsJobTokens = verifier
	}
}

func WithActionsExecutionContext(store ActionsExecutionContextStore) Option {
	return func(api *API) { api.actionsExecutionContext = store }
}

func WithActionsEnvironments(store ActionsEnvironmentStore) Option {
	return func(api *API) { api.actionsEnvironments = store }
}

func WithPersonalAccessTokens(store PersonalAccessTokenStore, secrets *auth.SecretCodec) Option {
	return func(api *API) {
		api.personalAccessTokens = store
		if api.secrets == nil {
			api.secrets = secrets
		}
	}
}

func WithEntitlements(store EntitlementStore) Option {
	return func(api *API) { api.entitlements = store }
}

func WithInstanceAdminUsernames(usernames []string) Option {
	return func(api *API) {
		api.instanceAdminUsernames = make(map[string]struct{}, len(usernames))
		for _, value := range usernames {
			username := strings.ToLower(strings.TrimSpace(value))
			if username != "" {
				api.instanceAdminUsernames[username] = struct{}{}
			}
		}
	}
}

func WithAuthorization(store AuthorizationStore) Option {
	return func(api *API) { api.authorization = store }
}

func WithLoreAuth(service *loreauth.Service) Option {
	return func(api *API) { api.loreAuth = service }
}

func WithLegacyLoreIdentityAllowed(allowed bool) Option {
	return func(api *API) { api.allowLegacyLoreIdentity = allowed }
}

// WithLoreServiceSubjects supplies the immutable JWT subjects for service
// purposes. An empty subject remains invalid and fails credential resolution.
func WithLoreServiceSubjects(subjects loreclient.ServiceSubjects) Option {
	return func(api *API) {
		api.serviceSubjects = subjects
	}
}

// WithDevelopmentLoreCredentials is explicit and intended only for local
// development. Production wiring must provide partition-scoped credentials.
func WithDevelopmentLoreCredentials(identity string) Option {
	return func(api *API) {
		api.loreCredentials = loreclient.NewDevelopmentCredentialProvider(identity)
	}
}

// ResolveActor exposes the common authenticated-actor path to route packages
// mounted by this API. Cookie sessions require CSRF on state-changing methods;
// bearer requests retain their existing compatibility behavior.
func (api *API) ResolveActor(writer http.ResponseWriter, request *http.Request) (platform.User, bool) {
	return api.actor(writer, request)
}

// ResolveOptionalActor resolves a valid active actor or an anonymous request.
// A presented but invalid or inactive credential is never treated as anonymous.
func (api *API) ResolveOptionalActor(
	writer http.ResponseWriter,
	request *http.Request,
) (*platform.User, bool) {
	if strings.TrimSpace(request.Header.Get("Authorization")) == "" {
		session, sessionToken, found, err := api.lookupSession(request)
		if err != nil {
			api.internalError(writer, request, "look up authentication session", err)
			return nil, false
		}
		if !found {
			if sessionToken != "" {
				writeProblem(writer, http.StatusUnauthorized, "authentication_required",
					"Authentication is required")
				return nil, false
			}
			return nil, true
		}
		if stateChangingMethod(request.Method) && !api.validCSRF(request, session.CSRFDigest) {
			writeProblem(writer, http.StatusForbidden, "csrf_failed", "A valid CSRF token is required")
			return nil, false
		}
		user, err := api.store.ActiveUser(request.Context(), session.UserID)
		if err != nil {
			writeProblem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
			return nil, false
		}
		return &user, true
	}
	user, ok := api.actor(writer, request)
	if !ok {
		return nil, false
	}
	return &user, true
}

func (api *API) loreCredential(
	ctx context.Context,
	repository loreclient.RepositoryRef,
	principal loreclient.Principal,
	scope loreclient.Scope,
) (loreclient.Credential, error) {
	if api.loreCredentials == nil {
		return loreclient.Credential{}, loreclient.ErrCredentialUnavailable
	}
	return api.loreCredentials.ForRepository(ctx, loreclient.CredentialRequest{
		Principal:  principal,
		Repository: repository,
		Partition:  repository.CanonicalPartition(),
		Scope:      scope,
	})
}

func (api *API) live(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (api *API) ready(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
	defer cancel()
	if err := api.health.Ping(ctx); err != nil {
		writeProblem(
			writer,
			http.StatusServiceUnavailable,
			"not_ready",
			"LoreHub service prerequisites are unavailable",
		)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
}

func (api *API) exploreRepositories(writer http.ResponseWriter, request *http.Request) {
	if _, ok := api.ResolveOptionalActor(writer, request); !ok {
		return
	}
	limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
	repositories, err := api.store.ExploreRepositories(request.Context(), limit)
	if err != nil {
		api.internalError(writer, request, "list public repositories", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"repositories": repositories})
}

func (api *API) publicRepository(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.ResolveOptionalActor(writer, request)
	if !ok {
		return
	}
	if api.collabStore != nil {
		repository, err := api.collabStore.LookupRepository(request.Context(), actor,
			request.PathValue("owner"), request.PathValue("repository"))
		if err != nil {
			api.platformError(writer, request, "get repository", err)
			return
		}
		writeJSON(writer, http.StatusOK, repository)
		return
	}
	var repository platform.Repository
	var err error
	if reader, supported := api.store.(RepositoryReader); supported {
		repository, err = reader.RepositoryForRead(request.Context(), actor,
			request.PathValue("owner"), request.PathValue("repository"))
	} else {
		repository, err = api.store.PublicRepository(request.Context(), request.PathValue("owner"),
			request.PathValue("repository"))
	}
	if err != nil {
		api.platformError(writer, request, "get repository", err)
		return
	}
	writeJSON(writer, http.StatusOK, repository)
}

func (api *API) createOrganization(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input struct {
		Slug        string `json:"slug"`
		DisplayName string `json:"displayName"`
		Description string `json:"description"`
		Visibility  string `json:"visibility"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	if input.DisplayName == "" || len(input.DisplayName) > 160 || len(input.Description) > 10_000 {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Organization fields are invalid")
		return
	}
	if !validVisibility(input.Visibility) {
		writeProblem(writer, http.StatusBadRequest, "invalid_visibility", "Visibility is invalid")
		return
	}
	organization, err := api.store.CreateOrganization(request.Context(), actor, platform.CreateOrganizationInput{
		Slug:        input.Slug,
		DisplayName: input.DisplayName,
		Description: input.Description,
		Visibility:  input.Visibility,
	})
	if err != nil {
		api.platformError(writer, request, "create organization", err)
		return
	}
	writeJSON(writer, http.StatusCreated, organization)
}

func (api *API) registerRepository(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input repositoryRequest
	if !decodeJSON(writer, request, &input) {
		return
	}
	if !validVisibility(input.Visibility) || input.LoreURL != "" || len(input.Description) > 10_000 {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Repository fields are invalid")
		return
	}
	if input.DisplayName == "" {
		input.DisplayName = input.Slug
	}
	provisioner, supported := api.store.(repositoryProvisioningStore)
	if !supported || api.managedLoreClient == nil || api.loreAuth == nil {
		writeProblem(writer, http.StatusServiceUnavailable, "provisioning_unavailable",
			"Lore repository provisioning is unavailable")
		return
	}
	repository, err := provisioner.BeginRepositoryProvisioning(request.Context(), actor,
		request.PathValue("organization"), platform.ProvisionRepositoryInput{
			Slug: input.Slug, DisplayName: input.DisplayName, Description: input.Description,
			Visibility: input.Visibility, DefaultBranch: input.DefaultBranch,
		}, input.LoreServerID)
	if err != nil {
		api.platformError(writer, request, "begin repository provisioning", err)
		return
	}
	if err := api.provisionManagedRepository(request, actor, repository, provisioner); err != nil {
		api.logger.Error("provision Lore repository", "error", err,
			"repository_id", repository.ID, "lore_repository_id", repository.LoreRepositoryID)
		writeProblem(writer, http.StatusBadGateway, "lore_unavailable", "Lore repository provisioning failed")
		return
	}
	repository.LifecycleState = "active"
	repository.ProvisioningError = ""
	writeJSON(writer, http.StatusCreated, repository)
}

func (api *API) createIssue(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" || len(input.Title) > 512 || len(input.Body) > 1_000_000 {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Issue fields are invalid")
		return
	}
	issue, err := api.store.CreateIssue(
		request.Context(),
		actor,
		request.PathValue("owner"),
		request.PathValue("repository"),
		platform.CreateIssueInput{Title: input.Title, Body: input.Body},
	)
	if err != nil {
		api.platformError(writer, request, "create issue", err)
		return
	}
	writeJSON(writer, http.StatusCreated, issue)
}

func (api *API) createMergeRequest(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input struct {
		Title        string `json:"title"`
		Body         string `json:"body"`
		IsDraft      bool   `json:"isDraft"`
		SourceBranch string `json:"sourceBranch"`
		TargetBranch string `json:"targetBranch"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" || len(input.Title) > 512 || len(input.Body) > 1_000_000 ||
		input.SourceBranch == input.TargetBranch {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Merge request fields are invalid")
		return
	}
	repository, err := api.store.RepositoryForWrite(
		request.Context(),
		actor,
		request.PathValue("owner"),
		request.PathValue("repository"),
	)
	if err != nil {
		api.platformError(writer, request, "check merge request permission", err)
		return
	}
	ref := loreclient.RepositoryRef{
		CacheKey:         repository.ID,
		URL:              repository.LoreURL,
		LoreRepositoryID: repository.LoreRepositoryID,
	}
	credential, credentialErr := api.loreCredential(request.Context(), ref, loreclient.UserPrincipal(actor.ID),
		loreclient.ScopeRead)
	if credentialErr != nil {
		writeProblem(writer, http.StatusBadGateway, "lore_unavailable", "Lore credentials are not configured")
		return
	}
	branches, err := api.lore.Branches(request.Context(), ref, credential)
	if err != nil {
		writeProblem(writer, http.StatusBadGateway, "lore_unavailable", "Lore branches could not be verified")
		return
	}
	sourceRevision, sourceFound := latestRevision(branches, input.SourceBranch)
	targetRevision, targetFound := latestRevision(branches, input.TargetBranch)
	if !sourceFound || !targetFound {
		writeProblem(writer, http.StatusBadRequest, "branch_not_found", "A selected Lore branch does not exist")
		return
	}
	mergeRequest, err := api.store.CreateMergeRequest(
		request.Context(),
		actor,
		repository.Owner,
		repository.Slug,
		platform.CreateMergeRequestInput{
			Title:          input.Title,
			Body:           input.Body,
			IsDraft:        input.IsDraft,
			SourceBranch:   input.SourceBranch,
			TargetBranch:   input.TargetBranch,
			SourceRevision: sourceRevision,
			TargetRevision: targetRevision,
		},
	)
	if err != nil {
		api.platformError(writer, request, "create merge request", err)
		return
	}
	writeJSON(writer, http.StatusCreated, mergeRequest)
}

func (api *API) listCIRuns(writer http.ResponseWriter, request *http.Request) {
	if api.actions != nil {
		api.actionRuns(writer, request)
		return
	}
	actor, actorOK := api.ResolveOptionalActor(writer, request)
	if !actorOK {
		return
	}
	if api.collabStore != nil {
		if reader, ok := api.collabStore.(collab.RepositoryReadStore); ok {
			repository, err := api.collabStore.LookupRepository(request.Context(), actor,
				request.PathValue("owner"), request.PathValue("repository"))
			if err != nil {
				api.platformError(writer, request, "list CI runs", err)
				return
			}
			runs, err := reader.ListCIRunsForRepository(request.Context(), repository.ID)
			if err != nil {
				api.internalError(writer, request, "list CI runs", err)
				return
			}
			writeJSON(writer, http.StatusOK, map[string]any{"runs": runs})
			return
		}
	}
	if err := api.authorizeFallbackRepositoryRead(request.Context(), actor, request); err != nil {
		api.platformError(writer, request, "list CI runs", err)
		return
	}
	runs, err := api.store.ListPublicCIRuns(
		request.Context(),
		request.PathValue("owner"),
		request.PathValue("repository"),
	)
	if err != nil {
		api.platformError(writer, request, "list CI runs", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"runs": runs})
}

func (api *API) authorizeFallbackRepositoryRead(
	ctx context.Context,
	actor *platform.User,
	request *http.Request,
) error {
	if actor == nil {
		return nil
	}
	reader, ok := api.store.(RepositoryReader)
	if !ok {
		return errors.New("authenticated repository visibility is unavailable")
	}
	_, err := reader.RepositoryForRead(ctx, actor, request.PathValue("owner"), request.PathValue("repository"))
	return err
}

func latestRevision(branches []loreclient.Branch, name string) (string, bool) {
	for _, branch := range branches {
		if branch.Name == name && !branch.Archived && branch.LatestRevision != "" {
			return branch.LatestRevision, true
		}
	}
	return "", false
}

func (api *API) actor(writer http.ResponseWriter, request *http.Request) (platform.User, bool) {
	user, _, ok := api.resolveActor(writer, request)
	return user, ok
}

func (api *API) resolveActor(
	writer http.ResponseWriter,
	request *http.Request,
) (platform.User, auth.Principal, bool) {
	authorization := strings.TrimSpace(request.Header.Get("Authorization"))
	if authorization == "" {
		if session, sessionToken, found, err := api.lookupSession(request); err != nil {
			api.internalError(writer, request, "look up authentication session", err)
			return platform.User{}, auth.Principal{}, false
		} else if found {
			if stateChangingMethod(request.Method) && !api.validCSRF(request, session.CSRFDigest) {
				writeProblem(writer, http.StatusForbidden, "csrf_failed", "A valid CSRF token is required")
				return platform.User{}, auth.Principal{}, false
			}
			user, err := api.store.ActiveUser(request.Context(), session.UserID)
			if err != nil {
				writeProblem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
				return platform.User{}, auth.Principal{}, false
			}
			return user, auth.Principal{}, true
		} else if sessionToken != "" {
			writeProblem(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required")
			return platform.User{}, auth.Principal{}, false
		}
	}
	principal, err := api.authenticator.Authenticate(request.Context(), authorization)
	if err != nil {
		if errors.Is(err, auth.ErrNotConfigured) || errors.Is(err, auth.ErrAuthenticationUnavailable) {
			writeProblem(writer, http.StatusServiceUnavailable, "authentication_unavailable", err.Error())
		} else {
			writeProblem(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required")
		}
		return platform.User{}, auth.Principal{}, false
	}
	if principal.CredentialKind == auth.CredentialPersonalAccessToken &&
		!auth.PersonalAccessTokenAllowsAPI(principal.Scopes, stateChangingMethod(request.Method)) {
		writer.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope"`)
		writeProblem(writer, http.StatusForbidden, "insufficient_token_scope",
			"The personal access token does not allow this operation")
		return platform.User{}, auth.Principal{}, false
	}
	var user platform.User
	if principal.CredentialKind == auth.CredentialPersonalAccessToken {
		if principal.InternalUserID == "" || principal.CredentialID == "" {
			writeProblem(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required")
			return platform.User{}, auth.Principal{}, false
		}
		user, err = api.store.ActiveUser(request.Context(), principal.InternalUserID)
	} else {
		user, err = api.store.EnsureUser(request.Context(), principal)
	}
	if err != nil {
		if errors.Is(err, platform.ErrForbidden) {
			writeProblem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
			return platform.User{}, auth.Principal{}, false
		}
		api.internalError(writer, request, "resolve authenticated user", err)
		return platform.User{}, auth.Principal{}, false
	}
	return user, principal, true
}

func (api *API) platformError(writer http.ResponseWriter, request *http.Request, operation string, err error) {
	var selectionError *platform.LoreServerSelectionError
	switch {
	case errors.As(err, &selectionError):
		writeProblem(writer, http.StatusConflict, selectionError.Reason, selectionError.Error())
	case errors.Is(err, platform.ErrNotFound):
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
	case errors.Is(err, platform.ErrForbidden):
		writeProblem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
	case errors.Is(err, platform.ErrConflict):
		writeProblem(writer, http.StatusConflict, "conflict", "The resource already exists")
	case errors.Is(err, platform.ErrInvalidInput):
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "The request contains invalid values")
	default:
		api.internalError(writer, request, operation, err)
	}
}

func (api *API) internalError(writer http.ResponseWriter, request *http.Request, operation string, err error) {
	api.logger.Error(operation, "error", err, "method", request.Method, "path", request.URL.Path)
	writeProblem(writer, http.StatusInternalServerError, "internal_error", "The request could not be completed")
}

func validVisibility(value string) bool {
	return value == "private" || value == "internal" || value == "public"
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, target any) bool {
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBody)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_json", "Request body is invalid")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeProblem(writer, http.StatusBadRequest, "invalid_json", "Request body must contain one JSON value")
		return false
	}
	return true
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeProblem(writer http.ResponseWriter, status int, code string, detail string) {
	writeJSON(writer, status, map[string]any{
		"error": map[string]string{
			"code":   code,
			"detail": detail,
		},
	})
}

func (api *API) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(writer, request)
	})
}

func (api *API) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		requestID := request.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.NewString()
		}
		writer.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(writer, request)
		api.logger.Info(
			"HTTP request",
			"request_id", requestID,
			"method", request.Method,
			"path", request.URL.Path,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	})
}

func (api *API) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				api.logger.Error("panic while serving request", "panic", recovered, "path", request.URL.Path)
				writeProblem(writer, http.StatusInternalServerError, "internal_error", "The request could not be completed")
			}
		}()
		next.ServeHTTP(writer, request)
	})
}
