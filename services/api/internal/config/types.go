package config

import (
	"time"

	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
)

type Config struct {
	Environment                       string
	HTTPAddress                       string
	DatabaseURL                       string
	DatabaseTimeout                   time.Duration
	ShutdownTimeout                   time.Duration
	AuthMode                          string
	OIDCIssuer                        string
	OIDCAudience                      string
	OIDCClientID                      string
	OIDCClientSecret                  string
	OIDCRedirectURL                   string
	PublicOrigin                      string
	PublicAPIURL                      string
	PublicGraphQLURL                  string
	ActionSourceURL                   string
	AuthSecret                        string
	SessionCookieName                 string
	LoginBindingCookieName            string
	SessionCookiePath                 string
	SessionCookieDomain               string
	SessionCookieSecure               bool
	SessionTTL                        time.Duration
	LoginTransactionTTL               time.Duration
	IdentityProviders                 []string
	LoreCacheDir                      string
	LoreIdentity                      string
	AllowLegacyLoreIdentity           bool
	LoreCredentials                   map[string]loreclient.CredentialMaterial
	LoreAuthAuthority                 string
	LorePublicReaderSubject           string
	LoreActionsRunnerSubject          string
	LoreObserverSubject               string
	LoreRepositoryRegistrationSubject string
	LoreAllowDevelopmentFallback      bool
	DevLoreIdentity                   string
	DevLoreIdentityFallback           bool
	ActionsSecretKeyID                string
	ActionsSecretKey                  string
	ActionsJobTokenAudience           string
	WebhookSecretKeyID                string
	WebhookSecretKey                  string
	WebhookPollPeriod                 time.Duration
	WebhookRequestTimeout             time.Duration
	WebhookLeaseDuration              time.Duration
	WebhookAllowPrivateTargets        bool
	LoreAuthIssuer                    string
	LoreAuthAudience                  string
	LoreRootDomain                    string
	LoreAuthJWKSURL                   string
	LoreAuthEnvironment               string
	LoreAuthIDP                       string
	LoreAuthTokenTTL                  time.Duration
	LoreAuthSessionTTL                time.Duration
	LoreAuthLoginURL                  string
	LoreAuthURL                       string
	LorePublicURL                     string
	LoreInternalURL                   string
	LoreAuthAddress                   string
	LoreAuthCompatAddress             string
	LoreAuthTLSCert                   string
	LoreAuthTLSKey                    string
	AuthSigningKeyPath                string
	AuthSigningKeyPEM                 string
	AuthSigningKeyKID                 string
	AuthPreviousKeys                  string
	PolicyAddress                     string
	PolicyTLSCert                     string
	PolicyTLSKey                      string
	PolicyTLSClientCA                 string
	LorePolicyEndpoint                string
	LoreObservationEndpoint           string
	LoreBinary                        string
	ActBinary                         string
	RunnerPollPeriod                  time.Duration
	BranchPollPeriod                  time.Duration
	RunnerJobTimeout                  time.Duration
	RunnerLeaseDuration               time.Duration
	RunnerLogMaxBytes                 int64
	RunnerLogMaxLineBytes             int64
	RunnerArtifactMaxCount            int
	RunnerArtifactMaxFile             int64
	RunnerArtifactMaxTotal            int64
	RunnerLogDir                      string
	RunnerArtifactDir                 string
	RunnerWorkDir                     string
	RunnerProxyURL                    string
	RunnerEngineProxyURL              string
	RunnerPlatformImages              map[string]string
	DevActionsContext                 DevActionsContext
	DevActionsContextFallback         bool
	DevActionsJobToken                string
	DevActionsJobTokenFallback        bool
}

type DevActionsContext struct {
	OrganizationVariables map[string]string `json:"organization_variables"`
	RepositoryVariables   map[string]string `json:"repository_variables"`
	EnvironmentVariables  map[string]string `json:"environment_variables"`
	OrganizationSecrets   map[string]string `json:"organization_secrets"`
	RepositorySecrets     map[string]string `json:"repository_secrets"`
	EnvironmentSecrets    map[string]string `json:"environment_secrets"`
}
