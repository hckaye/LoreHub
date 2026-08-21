package httpapi

import (
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	branchesapi "github.com/lorehub/lorehub/services/api/internal/branches"
	"github.com/lorehub/lorehub/services/api/internal/collab"
	discussionsapi "github.com/lorehub/lorehub/services/api/internal/discussions"
	filelocksapi "github.com/lorehub/lorehub/services/api/internal/filelocks"
	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
	"github.com/lorehub/lorehub/services/api/internal/loreauth"
	milestonesapi "github.com/lorehub/lorehub/services/api/internal/milestones"
	projectsapi "github.com/lorehub/lorehub/services/api/internal/projects"
	releasesapi "github.com/lorehub/lorehub/services/api/internal/releases"
	reviewthreadsapi "github.com/lorehub/lorehub/services/api/internal/reviewthreads"
	"github.com/lorehub/lorehub/services/api/internal/runner"
	statusesapi "github.com/lorehub/lorehub/services/api/internal/statuses"
	wikiapi "github.com/lorehub/lorehub/services/api/internal/wiki"
)

type API struct {
	coreDependencies
	collaborationDependencies
	authenticationDependencies
	actionsDependencies
	infrastructureDependencies
	administrationDependencies
}

type coreDependencies struct {
	store                   Store
	lore                    loreclient.Client
	authenticator           auth.Authenticator
	health                  HealthChecker
	loreIdentity            string
	allowLegacyLoreIdentity bool
	serviceSubjects         loreclient.ServiceSubjects
	loreCredentials         loreclient.CredentialProvider
	managedLoreClient       loreclient.ManagedRepositoryClient
	logger                  *slog.Logger
	operations              *operationalState
}

type collaborationDependencies struct {
	authorization        AuthorizationStore
	loreAuth             *loreauth.Service
	collabStore          collab.Store
	branchObservations   branchesapi.ObservationStore
	fileLockUsers        filelocksapi.UserDirectory
	fileLockObservations filelocksapi.ObservationStore
	projectsStore        projectsapi.Store
	discussionsStore     discussionsapi.Store
	releasesStore        releasesapi.Store
	milestonesStore      milestonesapi.Store
	wikiStore            wikiapi.Store
	reviewThreadsStore   reviewthreadsapi.Store
	statusesStore        statusesapi.Store
	webhooksStore        webhooksManager
}

type authenticationDependencies struct {
	loginProvider        auth.LoginProvider
	loginStore           auth.LoginTransactionStore
	sessionStore         auth.SessionStore
	cleanupStore         auth.CleanupStore
	passwordAuth         PasswordAuthStore
	passwordRegistration bool
	passwordResetSender  PasswordResetSender
	secrets              *auth.SecretCodec
	publicOrigin         string
	cookie               sessionCookieConfig
	sessionTTL           time.Duration
	transactionTTL       time.Duration
	identityStore        IdentityStore
	loginProviders       []string
	personalAccessTokens PersonalAccessTokenStore
}

type actionsDependencies struct {
	actions                 ActionsStore
	actionsEnvironments     ActionsEnvironmentStore
	actionsExecutionContext ActionsExecutionContextStore
	actionsSecurity         ActionsSecurityStore
	actionsJobTokens        runner.JobTokenVerifier
	runnerControl           RunnerControlStore
	runnerExecutionContext  runner.ExecutionContextResolver
	runnerJobTokenIssuer    runner.JobTokenIssuer
	runnerControlConfig     RunnerControlConfig
}

type infrastructureDependencies struct {
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
}

type administrationDependencies struct {
	instanceAdminUsernames                map[string]struct{}
	instanceAdminEnabled                  bool
	instanceSettings                      InstanceSettingsStore
	hostedLoreServerDefault               bool
	globalWorkItems                       GlobalWorkItemStore
	deletionRetention                     time.Duration
	maxOrganizationsPerUserDefault        int64
	maxRepositoriesPerOrganizationDefault int64
	maxRepositorySizeBytes                int64
}

func (api *API) validateConfiguredDependencies() error {
	required := []struct {
		name  string
		value any
	}{
		{"store", api.store}, {"lore", api.lore}, {"authenticator", api.authenticator}, {"health", api.health},
		{"logger", api.logger}, {"managed Lore client", api.managedLoreClient},
		{"Lore credentials", api.loreCredentials}, {"authorization", api.authorization},
		{"Lore auth", api.loreAuth}, {"collaboration", api.collabStore},
		{"branch observations", api.branchObservations}, {"file lock users", api.fileLockUsers},
		{"file lock observations", api.fileLockObservations}, {"projects", api.projectsStore},
		{"discussions", api.discussionsStore}, {"releases", api.releasesStore},
		{"milestones", api.milestonesStore}, {"wiki", api.wikiStore},
		{"review threads", api.reviewThreadsStore}, {"statuses", api.statusesStore},
		{"webhooks", api.webhooksStore}, {"identity", api.identityStore},
		{"personal access tokens", api.personalAccessTokens}, {"entitlements", api.entitlements},
		{"Actions", api.actions}, {"Actions environments", api.actionsEnvironments},
		{"Actions execution context", api.actionsExecutionContext}, {"Actions security", api.actionsSecurity},
		{"Actions job tokens", api.actionsJobTokens}, {"runners", api.runners},
		{"runner secrets", api.runnerSecrets}, {"Lore servers", api.loreServers},
		{"Lore server secrets", api.loresSecrets}, {"Lore server certificates", api.loreServerCertificates},
		{"Lore server certificate issuer", api.loreServerCertIssuer}, {"runner control", api.runnerControl},
		{"runner execution context", api.runnerExecutionContext}, {"runner job token issuer", api.runnerJobTokenIssuer},
		{"instance settings", api.instanceSettings}, {"global work items", api.globalWorkItems},
		{"operational endpoints", api.operations},
	}
	missing := make([]string, 0)
	for _, dependency := range required {
		if isNilDependency(dependency.value) {
			missing = append(missing, dependency.name)
		}
	}
	if strings.TrimSpace(api.runnerCredentialKeyID) == "" {
		missing = append(missing, "runner credential key ID")
	}
	if strings.TrimSpace(api.loresTokenKeyID) == "" {
		missing = append(missing, "Lore server token key ID")
	}
	if len(missing) != 0 {
		return fmt.Errorf("missing HTTP API dependencies: %s", strings.Join(missing, ", "))
	}
	return nil
}

func isNilDependency(value any) bool {
	if value == nil {
		return true
	}
	kind := reflect.ValueOf(value).Kind()
	return (kind == reflect.Chan || kind == reflect.Func || kind == reflect.Interface || kind == reflect.Map ||
		kind == reflect.Pointer || kind == reflect.Slice) && reflect.ValueOf(value).IsNil()
}
