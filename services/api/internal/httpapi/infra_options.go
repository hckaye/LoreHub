package httpapi

import (
	"context"
	"strings"
	"time"

	"github.com/lorehub/lorehub/services/api/internal/auth"
	"github.com/lorehub/lorehub/services/api/internal/platform"
	"github.com/lorehub/lorehub/services/api/internal/runner"
	"github.com/lorehub/lorehub/services/api/internal/servercert"
)

type RunnerStore interface {
	CreateRegistrationToken(
		context.Context,
		platform.User,
		platform.CreateRunnerRegistrationTokenInput,
	) (platform.RunnerRegistrationToken, error)
	ConsumeRegistrationToken(context.Context, []byte) (platform.RunnerRegistrationToken, error)
	RegisterRunner(context.Context, platform.RegisterRunnerInput) (platform.Runner, error)
	ListRunners(context.Context, platform.User, platform.RunnerScope) ([]platform.Runner, error)
	RevokeRunner(context.Context, platform.User, platform.RunnerScope, string) error
	TouchRunnerSeen(context.Context, string, time.Time) error
	AuthenticateRunner(context.Context, []byte, string, time.Time) (platform.Runner, error)
}

type LoreServerStore interface {
	CreateLoreServerRegistrationToken(
		context.Context, platform.User, string, platform.CreateLoreServerRegistrationTokenInput,
	) (platform.LoreServerRegistrationToken, error)
	RegisterServer(
		context.Context, []byte, platform.RegisterLoreServerInput,
	) (platform.LoreServer, error)
	ListServers(context.Context, platform.User, string) ([]platform.LoreServer, error)
	RevokeServer(context.Context, platform.User, string, string) error
	AuthenticateServer(context.Context, []byte, string, time.Time) (platform.LoreServer, error)
	UpdateServerHealth(context.Context, string, time.Time, string, string, map[string]any) error
	GetOrganizationDefaultServer(context.Context, platform.User, string) (*platform.LoreServer, error)
	SetOrganizationDefaultServer(
		context.Context, platform.User, string, string,
	) (*platform.LoreServer, error)
	ValidateRepositoryImportServer(context.Context, platform.User, string, string, string) error
}

type LoreServerCertificateStore interface {
	AuthenticateServer(context.Context, []byte, string, time.Time) (platform.LoreServer, error)
	RecordServerCertificate(context.Context, string, string, time.Time, time.Time) error
}

type LoreServerCertificateIssuer interface {
	Issue(string, time.Time) (servercert.Certificate, error)
}

type RunnerControlStore interface {
	RunnerClaimJob(context.Context, []byte, string, time.Time, time.Duration) (*runner.Job, error)
	RunnerLeaseJob(context.Context, string, string) (runner.Job, error)
	RunnerHeartbeatJob(context.Context, string, string, time.Duration) error
	RunnerCancellationRequested(context.Context, string, string) (bool, error)
	AppendRunnerJobLog(context.Context, string, string, int64, []byte, int64) (int64, error)
	AppendRunnerArtifact(
		context.Context, string, string, string, int64, []byte, bool, int64, int64, int,
	) (int64, error)
	CompleteJob(context.Context, runner.Job, string, string, string, []runner.Artifact) error
}

type RunnerControlConfig struct {
	LeaseDuration         time.Duration
	LogMaxBytes           int64
	ArtifactMaxCount      int
	ArtifactMaxFileBytes  int64
	ArtifactMaxTotalBytes int64
	Principal             runner.CredentialPrincipal
	RESTScope             string
	GraphQLScope          string
}

func WithRunners(store RunnerStore, secrets *auth.SecretCodec, credentialKeyID string) Option {
	return func(api *API) {
		api.runners = store
		api.runnerSecrets = secrets
		api.runnerCredentialKeyID = strings.TrimSpace(credentialKeyID)
	}
}

func WithLoreServers(
	store LoreServerStore,
	secrets *auth.SecretCodec,
	tokenKeyID string,
	allowPrivateServers bool,
) Option {
	return func(api *API) {
		api.loreServers = store
		api.loresSecrets = secrets
		api.loresTokenKeyID = strings.TrimSpace(tokenKeyID)
		api.loreAllowPrivateServers = allowPrivateServers
	}
}

func WithLoreServerCertificates(
	store LoreServerCertificateStore,
	issuer LoreServerCertificateIssuer,
) Option {
	return func(api *API) {
		api.loreServerCertificates = store
		api.loreServerCertIssuer = issuer
	}
}

func WithRunnerControlPlane(
	store RunnerControlStore,
	executionContext runner.ExecutionContextResolver,
	jobTokenIssuer runner.JobTokenIssuer,
	config RunnerControlConfig,
) Option {
	return func(api *API) {
		api.runnerControl = store
		api.runnerExecutionContext = executionContext
		api.runnerJobTokenIssuer = jobTokenIssuer
		api.runnerControlConfig = config
	}
}
