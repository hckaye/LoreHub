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
	codeapi "github.com/lorehub/lorehub/services/api/internal/code"
	"github.com/lorehub/lorehub/services/api/internal/collab"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	mergeapi "github.com/lorehub/lorehub/services/api/internal/merge"
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

type HealthChecker interface {
	Ping(ctx context.Context) error
}

type API struct {
	store           Store
	lore            loreclient.Client
	authenticator   auth.Authenticator
	health          HealthChecker
	loreIdentity    string
	loreCredentials loreclient.CredentialProvider
	logger          *slog.Logger
	collabStore     collab.Store
	loginProvider   auth.LoginProvider
	loginStore      auth.LoginTransactionStore
	sessionStore    auth.SessionStore
	cleanupStore    auth.CleanupStore
	secrets         *auth.SecretCodec
	publicOrigin    string
	cookie          sessionCookieConfig
	sessionTTL      time.Duration
	transactionTTL  time.Duration
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
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", api.live)
	mux.HandleFunc("GET /health/ready", api.ready)
	mux.HandleFunc("GET /auth/login", api.login)
	mux.HandleFunc("GET /auth/callback", api.callback)
	mux.HandleFunc("POST /auth/logout", api.logout)
	mux.HandleFunc("GET /api/v1/auth/session", api.session)
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
		if codeClient, ok := api.lore.(loreclient.CodeClient); ok {
			codeapi.Register(mux, api.collabStore, api.lore, codeClient, api, api.loreCredentials, logger)
		}
		if workflow, ok := api.collabStore.(collab.MergeWorkflowStore); ok {
			if mergeClient, mergeOK := api.lore.(loreclient.MergeClient); mergeOK {
				pushAuthorizer, _ := api.collabStore.(loreclient.PushAuthorizer)
				mergeapi.Register(mux, api.collabStore, workflow, api.lore, mergeClient, api,
					api.loreCredentials, pushAuthorizer, logger)
			}
		}
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

func WithLoreCredentials(provider loreclient.CredentialProvider) Option {
	return func(api *API) {
		api.loreCredentials = provider
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
	if api.collabStore != nil {
		actor, ok := api.ResolveOptionalActor(writer, request)
		if !ok {
			return
		}
		repository, err := api.collabStore.LookupRepository(request.Context(), actor,
			request.PathValue("owner"), request.PathValue("repository"))
		if err != nil {
			api.platformError(writer, request, "get repository", err)
			return
		}
		writeJSON(writer, http.StatusOK, repository)
		return
	}
	repository, err := api.store.PublicRepository(
		request.Context(),
		request.PathValue("owner"),
		request.PathValue("repository"),
	)
	if err != nil {
		api.platformError(writer, request, "get repository", err)
		return
	}
	writeJSON(writer, http.StatusOK, repository)
}

func (api *API) repositoryBranches(writer http.ResponseWriter, request *http.Request) {
	var repositoryLoreURL, repositoryID, repositoryLoreID string
	principal := loreclient.ServicePrincipal(loreclient.ServicePurposePublicReader)
	if api.collabStore != nil {
		actor, ok := api.ResolveOptionalActor(writer, request)
		if !ok {
			return
		}
		repository, err := api.collabStore.LookupRepository(request.Context(), actor,
			request.PathValue("owner"), request.PathValue("repository"))
		if err != nil {
			api.platformError(writer, request, "get repository branches", err)
			return
		}
		repositoryLoreURL = repository.LoreURL
		repositoryLoreID = repository.LoreRepositoryID
		repositoryID = repository.ID
		if actor != nil {
			principal = loreclient.UserPrincipal(actor.ID)
		}
	} else {
		repository, err := api.store.PublicRepository(
			request.Context(),
			request.PathValue("owner"),
			request.PathValue("repository"),
		)
		if err != nil {
			api.platformError(writer, request, "get repository branches", err)
			return
		}
		repositoryLoreURL = repository.LoreURL
		repositoryID = repository.ID
	}
	ref := loreclient.RepositoryRef{
		CacheKey:         repositoryID,
		URL:              repositoryLoreURL,
		LoreRepositoryID: repositoryLoreID,
	}
	credential, err := api.loreCredential(request.Context(), ref, principal, loreclient.ScopeRead)
	if err != nil {
		api.internalError(writer, request, "get Lore read credential", err)
		return
	}
	branches, err := api.lore.Branches(request.Context(), ref, credential)
	if err != nil {
		api.internalError(writer, request, "list Lore branches", err)
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
	credential, credentialErr := api.loreCredential(request.Context(), loreclient.RepositoryRef{
		URL: input.LoreURL,
	}, loreclient.ServicePrincipal(loreclient.ServicePurposeRepositoryRegistration), loreclient.ScopeRead)
	if credentialErr != nil {
		writeProblem(writer, http.StatusBadGateway, "lore_unavailable", "Lore credentials are not configured")
		return
	}
	loreRepository, err := api.lore.RepositoryInfo(request.Context(), input.LoreURL, credential)
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
	if api.collabStore != nil {
		if reader, ok := api.collabStore.(collab.RepositoryReadStore); ok {
			actor, actorOK := api.ResolveOptionalActor(writer, request)
			if !actorOK {
				return
			}
			repository, err := api.collabStore.LookupRepository(request.Context(), actor,
				request.PathValue("owner"), request.PathValue("repository"))
			if err != nil {
				api.platformError(writer, request, "list issues", err)
				return
			}
			issues, err := reader.ListIssuesForRepository(request.Context(), repository.ID, request.URL.Query().Get("state"))
			if err != nil {
				api.internalError(writer, request, "list issues", err)
				return
			}
			writeJSON(writer, http.StatusOK, map[string]any{"issues": issues})
			return
		}
	}
	issues, err := api.store.ListPublicIssues(
		request.Context(),
		request.PathValue("owner"),
		request.PathValue("repository"),
		request.URL.Query().Get("state"),
	)
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
	if api.collabStore != nil {
		if reader, ok := api.collabStore.(collab.RepositoryReadStore); ok {
			actor, actorOK := api.ResolveOptionalActor(writer, request)
			if !actorOK {
				return
			}
			repository, err := api.collabStore.LookupRepository(request.Context(), actor,
				request.PathValue("owner"), request.PathValue("repository"))
			if err != nil {
				api.platformError(writer, request, "list merge requests", err)
				return
			}
			mergeRequests, err := reader.ListMergeRequestsForRepository(request.Context(), repository.ID,
				request.URL.Query().Get("state"))
			if err != nil {
				api.internalError(writer, request, "list merge requests", err)
				return
			}
			writeJSON(writer, http.StatusOK, map[string]any{"mergeRequests": mergeRequests})
			return
		}
	}
	mergeRequests, err := api.store.ListPublicMergeRequests(
		request.Context(),
		request.PathValue("owner"),
		request.PathValue("repository"),
		request.URL.Query().Get("state"),
	)
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
	if api.collabStore != nil {
		if reader, ok := api.collabStore.(collab.RepositoryReadStore); ok {
			actor, actorOK := api.ResolveOptionalActor(writer, request)
			if !actorOK {
				return
			}
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
