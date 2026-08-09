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
	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/loreauth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
)

const maxRequestBody = 1 << 20

type Store interface {
	EnsureUser(ctx context.Context, principal auth.Principal) (platform.User, error)
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
	ListPublicIssues(ctx context.Context, owner string, slug string, state string) ([]platform.Issue, error)
	CreateIssue(
		ctx context.Context,
		actor platform.User,
		owner string,
		slug string,
		input platform.CreateIssueInput,
	) (platform.Issue, error)
	ListPublicMergeRequests(
		ctx context.Context,
		owner string,
		slug string,
		state string,
	) ([]platform.MergeRequest, error)
	CreateMergeRequest(
		ctx context.Context,
		actor platform.User,
		owner string,
		slug string,
		input platform.CreateMergeRequestInput,
	) (platform.MergeRequest, error)
	ListPublicCIRuns(ctx context.Context, owner string, slug string) ([]platform.CIRun, error)
}

type RepositoryReader interface {
	RepositoryForRead(
		ctx context.Context,
		actor *platform.User,
		owner string,
		slug string,
	) (platform.Repository, error)
}

type AuthorizedContentReader interface {
	ListIssuesForRead(
		ctx context.Context,
		actor *platform.User,
		repository platform.Repository,
		state string,
	) ([]platform.Issue, error)
	ListMergeRequestsForRead(
		ctx context.Context,
		actor *platform.User,
		repository platform.Repository,
		state string,
	) ([]platform.MergeRequest, error)
	ListCIRunsForRead(
		ctx context.Context,
		actor *platform.User,
		repository platform.Repository,
	) ([]platform.CIRun, error)
}

type BranchStateObserver interface {
	ObserveBranchState(
		ctx context.Context,
		repositoryID string,
		branchID string,
		branchName string,
		latestRevision string,
	) error
}

type HealthChecker interface {
	Ping(ctx context.Context) error
}

type AuthorizationStore interface {
	ListOrganizationMembers(
		ctx context.Context, actor platform.User, organizationSlug string,
	) ([]platform.OrganizationMember, error)
	SetOrganizationMember(
		ctx context.Context, actor platform.User, organizationSlug string,
		input platform.SetOrganizationMemberInput,
	) (platform.OrganizationMember, error)
	ListTeams(ctx context.Context, actor platform.User, organizationSlug string) ([]platform.Team, error)
	CreateTeam(
		ctx context.Context, actor platform.User, organizationSlug string, input platform.SetTeamInput,
	) (platform.Team, error)
	UpdateTeam(
		ctx context.Context, actor platform.User, organizationSlug string, teamSlug string, input platform.SetTeamInput,
	) (platform.Team, error)
	DeleteTeam(ctx context.Context, actor platform.User, organizationSlug string, teamSlug string) error
	ListTeamMembers(
		ctx context.Context, actor platform.User, organizationSlug string, teamSlug string,
	) ([]platform.TeamMember, error)
	SetTeamMember(
		ctx context.Context, actor platform.User, organizationSlug string, teamSlug string,
		input platform.SetTeamMemberInput,
	) (platform.TeamMember, error)
	SetTeamRepositoryRole(
		ctx context.Context, actor platform.User, organizationSlug string, teamSlug string,
		owner string, repositorySlug string, input platform.SetTeamRepositoryRoleInput,
	) (platform.TeamRepositoryRole, error)
	DeleteTeamRepositoryRole(
		ctx context.Context, actor platform.User, organizationSlug string, teamSlug string,
		owner string, repositorySlug string,
	) error
	ListRepositoryCollaborators(
		ctx context.Context, actor platform.User, owner string, repositorySlug string,
	) ([]platform.Collaborator, error)
	SetRepositoryCollaborator(
		ctx context.Context, actor platform.User, owner string, repositorySlug string,
		input platform.SetCollaboratorInput,
	) (platform.Collaborator, error)
	SetRepositoryPolicy(
		ctx context.Context, actor platform.User, owner string, repositorySlug string,
		input platform.SetRepositoryPolicyInput,
	) error
	GetRepositoryPolicy(
		ctx context.Context, actor platform.User, owner string, repositorySlug string,
	) (platform.RepositoryPolicy, error)
	SetObliterateGrant(
		ctx context.Context, actor platform.User, owner string, repositorySlug string,
		username string, active bool,
	) error
	DeclareRepositoryLink(
		ctx context.Context, actor platform.User, sourceOwner string, sourceSlug string,
		targetOwner string, targetSlug string,
	) (platform.RepositoryLink, error)
	ListRepositoryLinks(
		ctx context.Context, actor platform.User, owner string, repositorySlug string,
	) ([]platform.RepositoryLink, error)
	IssueMergeAuthorization(
		ctx context.Context, actor platform.User, input platform.MergeAuthorizationInput,
	) (platform.MergeAuthorization, error)
	CheckPolicy(ctx context.Context, check authz.PolicyCheck) (authz.PolicyDecision, error)
}

type API struct {
	store                   Store
	lore                    loreclient.Client
	authenticator           auth.Authenticator
	health                  HealthChecker
	loreIdentity            string
	allowLegacyLoreIdentity bool
	loreCredentialClient    loreclient.CredentialClient
	logger                  *slog.Logger
	collabStore             collab.Store
	authorization           AuthorizationStore
	loreAuth                *loreauth.Service
	loginProvider           auth.LoginProvider
	loginStore              auth.LoginTransactionStore
	sessionStore            auth.SessionStore
	cleanupStore            auth.CleanupStore
	secrets                 *auth.SecretCodec
	publicOrigin            string
	cookie                  sessionCookieConfig
	sessionTTL              time.Duration
	transactionTTL          time.Duration
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
	if credentialClient, ok := lore.(loreclient.CredentialClient); ok {
		api.loreCredentialClient = credentialClient
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", api.live)
	mux.HandleFunc("GET /health/ready", api.ready)
	mux.HandleFunc("GET /auth/login", api.login)
	mux.HandleFunc("GET /auth/callback", api.callback)
	mux.HandleFunc("POST /auth/logout", api.logout)
	mux.HandleFunc("GET /api/v1/auth/session", api.session)
	mux.HandleFunc("GET /.well-known/jwks.json", api.jwks)
	mux.HandleFunc("GET /auth/lore/confirm", api.loreAuthConfirm)
	mux.HandleFunc("POST /auth/lore/confirm", api.loreAuthConfirm)
	mux.HandleFunc("GET /api/v1/explore/repositories", api.exploreRepositories)
	mux.HandleFunc("POST /api/v1/organizations", api.createOrganization)
	mux.HandleFunc("POST /api/v1/organizations/{organization}/repositories", api.registerRepository)
	mux.HandleFunc("GET /api/v1/repositories/{owner}/{repository}", api.publicRepository)
	mux.HandleFunc("GET /api/v1/repositories/{owner}/{repository}/branches", api.repositoryBranches)
	mux.HandleFunc("GET /api/v1/repositories/{owner}/{repository}/issues", api.listIssues)
	mux.HandleFunc("POST /api/v1/repositories/{owner}/{repository}/issues", api.createIssue)
	mux.HandleFunc("GET /api/v1/repositories/{owner}/{repository}/merge-requests", api.listMergeRequests)
	mux.HandleFunc("POST /api/v1/repositories/{owner}/{repository}/merge-requests", api.createMergeRequest)
	mux.HandleFunc("GET /api/v1/repositories/{owner}/{repository}/actions/runs", api.listCIRuns)
	if api.collabStore != nil {
		collab.Register(mux, api.collabStore, api, logger)
	}
	if api.authorization != nil {
		registerAuthorizationRoutes(mux, api)
	}
	return api.recoverPanic(api.securityHeaders(api.requestLog(mux)))
}

// WithCollaboration mounts the collaboration API using the same actor and
// session resolver as the rest of this HTTP API.
func WithCollaboration(store collab.Store) Option {
	return func(api *API) {
		api.collabStore = store
	}
}

func WithAuthorization(store AuthorizationStore) Option {
	return func(api *API) {
		api.authorization = store
	}
}

func WithLoreAuth(service *loreauth.Service) Option {
	return func(api *API) {
		api.loreAuth = service
	}
}

func WithLegacyLoreIdentityAllowed(allowed bool) Option {
	return func(api *API) {
		api.allowLegacyLoreIdentity = allowed
	}
}

// ResolveActor exposes the common authenticated-actor path to route packages
// mounted by this API. Cookie sessions require CSRF on state-changing methods;
// bearer requests retain their existing compatibility behavior.
func (api *API) ResolveActor(writer http.ResponseWriter, request *http.Request) (platform.User, bool) {
	return api.actor(writer, request)
}

// ResolveOptionalActor resolves a valid browser session or bearer actor when
// present, while allowing anonymous public reads. Invalid or expired cookies
// are treated as anonymous and never authorize access to private resources.
func (api *API) ResolveOptionalActor(
	writer http.ResponseWriter,
	request *http.Request,
) (*platform.User, bool) {
	if strings.TrimSpace(request.Header.Get("Authorization")) == "" {
		session, _, found, err := api.lookupSession(request)
		if err != nil {
			api.internalError(writer, request, "look up authentication session", err)
			return nil, false
		}
		if !found {
			return nil, true
		}
		if stateChangingMethod(request.Method) && !api.validCSRF(request, session.CSRFDigest) {
			writeProblem(writer, http.StatusForbidden, "csrf_failed", "A valid CSRF token is required")
			return nil, false
		}
		user := userFromSession(session)
		return &user, true
	}
	user, ok := api.actor(writer, request)
	if !ok {
		return nil, false
	}
	return &user, true
}

func (api *API) live(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (api *API) ready(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
	defer cancel()
	if err := api.health.Ping(ctx); err != nil {
		writeProblem(writer, http.StatusServiceUnavailable, "not_ready", "PostgreSQL is unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
}

func (api *API) exploreRepositories(writer http.ResponseWriter, request *http.Request) {
	limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
	repositories, err := api.store.ExploreRepositories(request.Context(), limit)
	if err != nil {
		api.internalError(writer, request, "list public repositories", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"repositories": repositories})
}

func (api *API) publicRepository(writer http.ResponseWriter, request *http.Request) {
	repository, _, ok := api.repositoryForRead(writer, request)
	if !ok {
		return
	}
	writeJSON(writer, http.StatusOK, repository)
}

func (api *API) repositoryBranches(writer http.ResponseWriter, request *http.Request) {
	repository, actor, ok := api.repositoryForRead(writer, request)
	if !ok {
		return
	}
	if actor == nil {
		writeProblem(writer, http.StatusUnauthorized, "authentication_required",
			"Authentication is required to read Lore branch data")
		return
	}
	branches, err := api.listLoreBranches(request.Context(), *actor, repository)
	if err != nil {
		if errors.Is(err, errScopedLoreCredentialUnavailable) {
			api.internalError(writer, request, "get scoped Lore credential", err)
			return
		}
		writeProblem(writer, http.StatusBadGateway, "lore_unavailable", "Lore branches could not be read")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"branches": branches})
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
	var input struct {
		Slug        string `json:"slug"`
		DisplayName string `json:"displayName"`
		Description string `json:"description"`
		Visibility  string `json:"visibility"`
		LoreURL     string `json:"loreUrl"`
	}
	if !decodeJSON(writer, request, &input) {
		return
	}
	if !validVisibility(input.Visibility) || input.LoreURL == "" || len(input.Description) > 10_000 {
		writeProblem(writer, http.StatusBadRequest, "invalid_input", "Repository fields are invalid")
		return
	}
	loreRepository, err := api.repositoryInfoForRegistration(request.Context(), request, actor, input.LoreURL)
	if err != nil {
		writeProblem(writer, http.StatusBadGateway, "lore_unavailable", "Lore repository could not be verified")
		return
	}
	if input.DisplayName == "" {
		input.DisplayName = loreRepository.Name
	}
	if input.Description == "" {
		input.Description = loreRepository.Description
	}
	repository, err := api.store.RegisterRepository(
		request.Context(),
		actor,
		request.PathValue("organization"),
		platform.RegisterRepositoryInput{
			Slug:             input.Slug,
			DisplayName:      input.DisplayName,
			Description:      input.Description,
			Visibility:       input.Visibility,
			LoreRepositoryID: loreRepository.ID,
			LoreURL:          input.LoreURL,
			DefaultBranch:    loreRepository.DefaultBranch,
		},
	)
	if err != nil {
		api.platformError(writer, request, "register repository", err)
		return
	}
	writeJSON(writer, http.StatusCreated, repository)
}

func (api *API) listIssues(writer http.ResponseWriter, request *http.Request) {
	repository, actor, ok := api.repositoryForRead(writer, request)
	if !ok {
		return
	}
	var issues []platform.Issue
	var err error
	if reader, supported := api.store.(AuthorizedContentReader); supported {
		issues, err = reader.ListIssuesForRead(request.Context(), actor, repository, request.URL.Query().Get("state"))
	} else {
		issues, err = api.store.ListPublicIssues(
			request.Context(),
			request.PathValue("owner"),
			request.PathValue("repository"),
			request.URL.Query().Get("state"),
		)
	}
	if err != nil {
		api.platformError(writer, request, "list issues", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"issues": issues})
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
	if reader, supported := api.store.(RepositoryReader); supported {
		if _, err := reader.RepositoryForRead(
			request.Context(), &actor, request.PathValue("owner"), request.PathValue("repository"),
		); err != nil {
			api.platformError(writer, request, "check issue repository visibility", err)
			return
		}
	}
	if _, err := api.store.RepositoryForWrite(request.Context(), actor, request.PathValue("owner"),
		request.PathValue("repository")); err != nil {
		api.platformError(writer, request, "check issue permission", err)
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

func (api *API) listMergeRequests(writer http.ResponseWriter, request *http.Request) {
	repository, actor, ok := api.repositoryForRead(writer, request)
	if !ok {
		return
	}
	var mergeRequests []platform.MergeRequest
	var err error
	if reader, supported := api.store.(AuthorizedContentReader); supported {
		mergeRequests, err = reader.ListMergeRequestsForRead(
			request.Context(), actor, repository, request.URL.Query().Get("state"),
		)
	} else {
		mergeRequests, err = api.store.ListPublicMergeRequests(
			request.Context(),
			request.PathValue("owner"),
			request.PathValue("repository"),
			request.URL.Query().Get("state"),
		)
	}
	if err != nil {
		api.platformError(writer, request, "list merge requests", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"mergeRequests": mergeRequests})
}

func (api *API) createMergeRequest(writer http.ResponseWriter, request *http.Request) {
	actor, ok := api.actor(writer, request)
	if !ok {
		return
	}
	var input struct {
		Title        string `json:"title"`
		Body         string `json:"body"`
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
	if reader, supported := api.store.(RepositoryReader); supported {
		if _, err := reader.RepositoryForRead(
			request.Context(), &actor, request.PathValue("owner"), request.PathValue("repository"),
		); err != nil {
			api.platformError(writer, request, "check merge request repository visibility", err)
			return
		}
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
	branches, err := api.listLoreBranches(request.Context(), actor, repository)
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
	repository, actor, ok := api.repositoryForRead(writer, request)
	if !ok {
		return
	}
	var runs []platform.CIRun
	var err error
	if reader, supported := api.store.(AuthorizedContentReader); supported {
		runs, err = reader.ListCIRunsForRead(request.Context(), actor, repository)
	} else {
		runs, err = api.store.ListPublicCIRuns(
			request.Context(),
			request.PathValue("owner"),
			request.PathValue("repository"),
		)
	}
	if err != nil {
		api.platformError(writer, request, "list CI runs", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"runs": runs})
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
	authorization := strings.TrimSpace(request.Header.Get("Authorization"))
	if authorization == "" {
		if session, _, found, err := api.lookupSession(request); err != nil {
			api.internalError(writer, request, "look up authentication session", err)
			return platform.User{}, false
		} else if found {
			if stateChangingMethod(request.Method) && !api.validCSRF(request, session.CSRFDigest) {
				writeProblem(writer, http.StatusForbidden, "csrf_failed", "A valid CSRF token is required")
				return platform.User{}, false
			}
			return userFromSession(session), true
		}
	}
	principal, err := api.authenticator.Authenticate(request.Context(), authorization)
	if err != nil {
		if errors.Is(err, auth.ErrNotConfigured) {
			writeProblem(writer, http.StatusServiceUnavailable, "authentication_unavailable", err.Error())
		} else {
			writeProblem(writer, http.StatusUnauthorized, "authentication_required", "Authentication is required")
		}
		return platform.User{}, false
	}
	user, err := api.store.EnsureUser(request.Context(), principal)
	if err != nil {
		if errors.Is(err, platform.ErrForbidden) {
			writeProblem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
			return platform.User{}, false
		}
		api.internalError(writer, request, "provision authenticated user", err)
		return platform.User{}, false
	}
	return user, true
}

func (api *API) platformError(writer http.ResponseWriter, request *http.Request, operation string, err error) {
	switch {
	case errors.Is(err, platform.ErrNotFound):
		writeProblem(writer, http.StatusNotFound, "not_found", "The requested resource was not found")
	case errors.Is(err, platform.ErrForbidden):
		writeProblem(writer, http.StatusForbidden, "forbidden", "This operation is not permitted")
	case errors.Is(err, platform.ErrConflict):
		writeProblem(writer, http.StatusConflict, "conflict", "The resource already exists")
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
