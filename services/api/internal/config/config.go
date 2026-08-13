package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	loreclient "github.com/lorehub/lorehub/services/api/internal/lore"
)

const (
	AuthModeDisabled    = "disabled"
	AuthModeBearer      = "bearer"
	AuthModeInteractive = "interactive"
	maxSessionLifetime  = 30 * 24 * time.Hour
	maxTransactionLife  = 15 * time.Minute
	maxRunnerJobTimeout = 24 * time.Hour
)

func Load() (Config, error) {
	return LoadFor("serve")
}

func LoadFor(command string) (Config, error) {
	if command != "serve" && command != "runner" && command != "migrate" {
		return Config{}, fmt.Errorf("unsupported LoreHub command %q", command)
	}
	if command == "migrate" {
		return loadMigrationConfig()
	}
	environment := envOrDefault("LOREHUB_ENV", "development")
	loreAuthIssuer := os.Getenv("LOREHUB_LORE_AUTH_ISSUER")
	loreAuthLoginURL := os.Getenv("LOREHUB_LORE_AUTH_LOGIN_URL")
	loreAuthURL := os.Getenv("LOREHUB_LORE_AUTH_URL")
	loreRootDomain := os.Getenv("LOREHUB_LORE_ROOT_DOMAIN")
	loreAuthJWKSURL := os.Getenv("LOREHUB_LORE_AUTH_JWKS_URL")
	lorePublicURL := os.Getenv("LOREHUB_LORE_PUBLIC_URL")
	loreInternalURL := os.Getenv("LOREHUB_LORE_INTERNAL_URL")
	lorePolicyEndpoint := os.Getenv("LOREHUB_LORE_POLICY_ENDPOINT")
	loreObservationEndpoint := os.Getenv("LOREHUB_LORE_OBSERVATION_ENDPOINT")
	loreAuthAuthority := os.Getenv("LOREHUB_LORE_AUTHORITY")
	publicReaderSubject := os.Getenv("LOREHUB_LORE_PUBLIC_READER_SUBJECT")
	actionsRunnerSubject := os.Getenv("LOREHUB_LORE_ACTIONS_RUNNER_SUBJECT")
	observerSubject := os.Getenv("LOREHUB_OBSERVER_SERVICE_PRINCIPAL")
	registrationSubject := os.Getenv("LOREHUB_LORE_REPOSITORY_REGISTRATION_SUBJECT")
	lifecycleSubject := os.Getenv("LOREHUB_LORE_REPOSITORY_LIFECYCLE_SUBJECT")
	signingKeyPath := os.Getenv("LOREHUB_AUTH_SIGNING_KEY_PATH")
	loreAuthTLSCert := os.Getenv("LOREHUB_LORE_AUTH_TLS_CERT")
	loreAuthTLSKey := os.Getenv("LOREHUB_LORE_AUTH_TLS_KEY")
	policyTLSCert := os.Getenv("LOREHUB_POLICY_TLS_CERT")
	policyTLSKey := os.Getenv("LOREHUB_POLICY_TLS_KEY")
	policyTLSCA := os.Getenv("LOREHUB_POLICY_TLS_CLIENT_CA")
	signingKeyKID := os.Getenv("LOREHUB_AUTH_SIGNING_KEY_KID")
	loreCredentials, err := parseLoreCredentials(os.Getenv("LOREHUB_LORE_CREDENTIALS"))
	if err != nil {
		return Config{}, err
	}
	allowLegacyLoreIdentity, err := boolSetting("LOREHUB_ALLOW_LEGACY_LORE_IDENTITY", false)
	if err != nil {
		return Config{}, err
	}
	allowDevelopmentFallback, err := boolSetting("LOREHUB_LORE_ALLOW_DEVELOPMENT_FALLBACK", false)
	if err != nil {
		return Config{}, err
	}
	if environment != "production" {
		loreRootDomain = defaultIfEmpty(loreRootDomain, "lorehub.localhost")
		loreAuthIssuer = defaultIfEmpty(loreAuthIssuer, "auth."+loreRootDomain)
		loreAuthLoginURL = defaultIfEmpty(loreAuthLoginURL,
			"http://"+loreRootDomain+":8080/auth/lore/confirm")
		loreAuthURL = defaultIfEmpty(loreAuthURL, "ucs-auth://auth."+loreRootDomain+":8443")
		loreAuthJWKSURL = defaultIfEmpty(loreAuthJWKSURL,
			"http://"+loreRootDomain+":8080/.well-known/jwks.json")
		lorePublicURL = defaultIfEmpty(lorePublicURL, "lores://"+loreRootDomain+":41337")
		lorePolicyEndpoint = defaultIfEmpty(lorePolicyEndpoint,
			"https://"+loreRootDomain+":8444/internal/lore/policy")
		loreObservationEndpoint = defaultIfEmpty(loreObservationEndpoint,
			"https://"+loreRootDomain+":8444/internal/lore/observation")
		loreAuthAuthority = defaultIfEmpty(loreAuthAuthority, "auth."+loreRootDomain+":8443")
		publicReaderSubject = defaultIfEmpty(publicReaderSubject,
			"00000000-0000-4000-8000-000000000001")
		actionsRunnerSubject = defaultIfEmpty(actionsRunnerSubject,
			"00000000-0000-4000-8000-000000000002")
		observerSubject = defaultIfEmpty(observerSubject,
			"00000000-0000-4000-8000-000000000003")
		registrationSubject = defaultIfEmpty(registrationSubject,
			"00000000-0000-4000-8000-000000000004")
		lifecycleSubject = defaultIfEmpty(lifecycleSubject,
			"00000000-0000-4000-8000-000000000005")
		signingKeyPath = defaultIfEmpty(signingKeyPath, "/var/lib/lorehub/keys/lore-auth.pem")
		loreAuthTLSCert = defaultIfEmpty(loreAuthTLSCert, "/var/lib/lorehub/tls/server.crt")
		loreAuthTLSKey = defaultIfEmpty(loreAuthTLSKey, "/var/lib/lorehub/tls/server.key")
		policyTLSCert = defaultIfEmpty(policyTLSCert, "/var/lib/lorehub/tls/server.crt")
		policyTLSKey = defaultIfEmpty(policyTLSKey, "/var/lib/lorehub/tls/server.key")
		policyTLSCA = defaultIfEmpty(policyTLSCA, "/var/lib/lorehub/tls/ca.crt")
		signingKeyKID = defaultIfEmpty(signingKeyKID, "local-current")
	}
	loreInternalURL = defaultIfEmpty(loreInternalURL, lorePublicURL)
	oidcIssuer := os.Getenv("LOREHUB_OIDC_ISSUER")
	oidcAudience := os.Getenv("LOREHUB_OIDC_AUDIENCE")
	oidcClientID := os.Getenv("LOREHUB_OIDC_CLIENT_ID")
	authMode := authModeFromEnvironment(
		os.Getenv("LOREHUB_AUTH_MODE"),
		oidcIssuer,
		oidcAudience,
		os.Getenv("LOREHUB_OIDC_CLIENT_ID"),
		os.Getenv("LOREHUB_OIDC_CLIENT_SECRET"),
		os.Getenv("LOREHUB_OIDC_REDIRECT_URL"),
	)
	cookieSecure, err := cookieSecureSetting(environment)
	if err != nil {
		return Config{}, err
	}
	sessionTTL, err := durationSetting("LOREHUB_SESSION_TTL", 30*24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	transactionTTL, err := durationSetting("LOREHUB_LOGIN_TRANSACTION_TTL", 10*time.Minute)
	if err != nil {
		return Config{}, err
	}
	logMaxBytes, err := byteSetting("LOREHUB_RUNNER_LOG_MAX_BYTES", 10<<20)
	if err != nil {
		return Config{}, err
	}
	logMaxLineBytes, err := byteSetting("LOREHUB_RUNNER_LOG_MAX_LINE_BYTES", 1<<20)
	if err != nil {
		return Config{}, err
	}
	artifactMaxFile, err := byteSetting("LOREHUB_RUNNER_ARTIFACT_MAX_FILE_BYTES", 100<<20)
	if err != nil {
		return Config{}, err
	}
	artifactMaxTotal, err := byteSetting("LOREHUB_RUNNER_ARTIFACT_MAX_TOTAL_BYTES", 500<<20)
	if err != nil {
		return Config{}, err
	}
	artifactMaxCount, err := intSetting("LOREHUB_RUNNER_ARTIFACT_MAX_COUNT", 100)
	if err != nil {
		return Config{}, err
	}
	devLoreIdentityFallback, err := boolSetting("LOREHUB_DEV_ALLOW_LORE_IDENTITY_FALLBACK", false)
	if err != nil {
		return Config{}, err
	}
	devActionsContextFallback, err := boolSetting("LOREHUB_DEV_ALLOW_ACTIONS_CONTEXT_FALLBACK", false)
	if err != nil {
		return Config{}, err
	}
	devActionsJobTokenFallback, err := boolSetting("LOREHUB_DEV_ALLOW_ACTIONS_JOB_TOKEN_FALLBACK", false)
	if err != nil {
		return Config{}, err
	}
	webhookAllowPrivateTargets := false
	webhookPollPeriod := 2 * time.Second
	webhookRequestTimeout := 10 * time.Second
	webhookLeaseDuration := 30 * time.Second
	repositoryDeletionRetention := 30 * 24 * time.Hour
	repositoryDeletionPollPeriod := 30 * time.Second
	repositoryDeletionTimeout := 90 * time.Second
	repositoryDeletionLeaseDuration := 2 * time.Minute
	if command == "serve" {
		webhookAllowPrivateTargets, err = boolSetting("LOREHUB_WEBHOOK_ALLOW_PRIVATE_TARGETS", false)
		if err != nil {
			return Config{}, err
		}
		webhookPollPeriod, err = durationSetting("LOREHUB_WEBHOOK_POLL_PERIOD", webhookPollPeriod)
		if err != nil {
			return Config{}, err
		}
		webhookRequestTimeout, err = durationSetting(
			"LOREHUB_WEBHOOK_REQUEST_TIMEOUT",
			webhookRequestTimeout,
		)
		if err != nil {
			return Config{}, err
		}
		webhookLeaseDuration, err = durationSetting("LOREHUB_WEBHOOK_LEASE_DURATION", webhookLeaseDuration)
		if err != nil {
			return Config{}, err
		}
		repositoryDeletionRetention, err = durationSetting(
			"LOREHUB_REPOSITORY_DELETION_RETENTION",
			repositoryDeletionRetention,
		)
		if err != nil {
			return Config{}, err
		}
		repositoryDeletionPollPeriod, err = durationSetting(
			"LOREHUB_REPOSITORY_DELETION_POLL_PERIOD",
			repositoryDeletionPollPeriod,
		)
		if err != nil {
			return Config{}, err
		}
		repositoryDeletionTimeout, err = durationSetting(
			"LOREHUB_REPOSITORY_DELETION_TIMEOUT",
			repositoryDeletionTimeout,
		)
		if err != nil {
			return Config{}, err
		}
		repositoryDeletionLeaseDuration, err = durationSetting(
			"LOREHUB_REPOSITORY_DELETION_LEASE_DURATION",
			repositoryDeletionLeaseDuration,
		)
		if err != nil {
			return Config{}, err
		}
	}
	platformImages, err := parseJSONMapSetting("LOREHUB_RUNNER_PLATFORM_IMAGES")
	if err != nil {
		return Config{}, err
	}
	devActionsContext, err := parseDevActionsContext(os.Getenv("LOREHUB_DEV_ACTIONS_CONTEXT_JSON"))
	if err != nil {
		return Config{}, err
	}
	publicOrigin := strings.TrimRight(os.Getenv("LOREHUB_PUBLIC_ORIGIN"), "/")
	if publicOrigin == "" &&
		(environment == "development" || environment == "local" || environment == "local-insecure") {
		publicOrigin = "http://localhost:3000"
	}
	publicAPIURL := strings.TrimRight(os.Getenv("LOREHUB_PUBLIC_API_URL"), "/")
	if publicAPIURL == "" && publicOrigin != "" {
		publicAPIURL = publicOrigin + "/api/v3"
	}
	publicGraphQLURL := strings.TrimRight(os.Getenv("LOREHUB_PUBLIC_GRAPHQL_URL"), "/")
	if publicGraphQLURL == "" && publicOrigin != "" {
		publicGraphQLURL = publicOrigin + "/api/graphql"
	}
	config := Config{
		Environment:                       environment,
		HTTPAddress:                       envOrDefault("LOREHUB_HTTP_ADDRESS", ":8080"),
		DatabaseURL:                       os.Getenv("DATABASE_URL"),
		DatabaseTimeout:                   durationOrDefault("LOREHUB_DATABASE_TIMEOUT", 10*time.Second),
		ShutdownTimeout:                   durationOrDefault("LOREHUB_SHUTDOWN_TIMEOUT", 15*time.Second),
		AuthMode:                          authMode,
		OIDCIssuer:                        oidcIssuer,
		OIDCAudience:                      oidcAudience,
		OIDCClientID:                      oidcClientID,
		OIDCClientSecret:                  os.Getenv("LOREHUB_OIDC_CLIENT_SECRET"),
		OIDCRedirectURL:                   os.Getenv("LOREHUB_OIDC_REDIRECT_URL"),
		PublicOrigin:                      publicOrigin,
		PublicAPIURL:                      publicAPIURL,
		PublicGraphQLURL:                  publicGraphQLURL,
		ActionSourceURL:                   envOrDefault("LOREHUB_ACTION_SOURCE_URL", "https://github.com"),
		AuthSecret:                        os.Getenv("LOREHUB_AUTH_SECRET"),
		SessionCookieName:                 envOrDefault("LOREHUB_SESSION_COOKIE_NAME", "lorehub_session"),
		LoginBindingCookieName:            envOrDefault("LOREHUB_LOGIN_BINDING_COOKIE_NAME", "lorehub_login_binding"),
		SessionCookiePath:                 envOrDefault("LOREHUB_SESSION_COOKIE_PATH", "/"),
		SessionCookieDomain:               os.Getenv("LOREHUB_SESSION_COOKIE_DOMAIN"),
		SessionCookieSecure:               cookieSecure,
		SessionTTL:                        sessionTTL,
		LoginTransactionTTL:               transactionTTL,
		IdentityProviders:                 configuredIdentityProviders(),
		InstanceAdminUsernames:            commaSeparatedUsernames(os.Getenv("LOREHUB_INSTANCE_ADMIN_USERNAMES")),
		LoreCacheDir:                      envOrDefault("LOREHUB_LORE_CACHE_DIR", ".cache/lorehub/repositories"),
		LoreIdentity:                      os.Getenv("LOREHUB_LORE_IDENTITY"),
		AllowLegacyLoreIdentity:           allowLegacyLoreIdentity,
		LoreCredentials:                   loreCredentials,
		LoreAuthAuthority:                 strings.TrimSpace(loreAuthAuthority),
		LorePublicReaderSubject:           publicReaderSubject,
		LoreActionsRunnerSubject:          actionsRunnerSubject,
		LoreObserverSubject:               observerSubject,
		LoreRepositoryRegistrationSubject: registrationSubject,
		LoreRepositoryLifecycleSubject:    lifecycleSubject,
		LoreAllowDevelopmentFallback:      allowDevelopmentFallback,
		DevLoreIdentity:                   os.Getenv("LOREHUB_DEV_LORE_IDENTITY"),
		DevLoreIdentityFallback:           devLoreIdentityFallback,
		ActionsSecretKeyID:                os.Getenv("LOREHUB_ACTIONS_SECRET_KEY_ID"),
		ActionsSecretKey:                  os.Getenv("LOREHUB_ACTIONS_SECRET_KEY"),
		ActionsJobTokenAudience:           envOrDefault("LOREHUB_ACTIONS_JOB_TOKEN_AUDIENCE", publicAPIURL),
		WebhookSecretKeyID:                os.Getenv("LOREHUB_WEBHOOK_SECRET_KEY_ID"),
		WebhookSecretKey:                  os.Getenv("LOREHUB_WEBHOOK_SECRET_KEY"),
		WebhookPollPeriod:                 webhookPollPeriod,
		WebhookRequestTimeout:             webhookRequestTimeout,
		WebhookLeaseDuration:              webhookLeaseDuration,
		WebhookAllowPrivateTargets:        webhookAllowPrivateTargets,
		RepositoryDeletionRetention:       repositoryDeletionRetention,
		RepositoryDeletionPollPeriod:      repositoryDeletionPollPeriod,
		RepositoryDeletionTimeout:         repositoryDeletionTimeout,
		RepositoryDeletionLeaseDuration:   repositoryDeletionLeaseDuration,
		LoreAuthIssuer:                    loreAuthIssuer,
		LoreAuthAudience:                  envOrDefault("LOREHUB_LORE_AUTH_AUDIENCE", loreRootDomain),
		LoreRootDomain:                    loreRootDomain,
		LoreAuthJWKSURL:                   loreAuthJWKSURL,
		LoreAuthEnvironment:               envOrDefault("LOREHUB_LORE_AUTH_ENVIRONMENT", environment),
		LoreAuthIDP:                       envOrDefault("LOREHUB_LORE_AUTH_IDP", "keycloak"),
		LoreAuthTokenTTL:                  durationOrDefault("LOREHUB_LORE_AUTH_TOKEN_TTL", 5*time.Minute),
		LoreAuthSessionTTL:                durationOrDefault("LOREHUB_LORE_AUTH_SESSION_TTL", 5*time.Minute),
		LoreAuthLoginURL:                  loreAuthLoginURL,
		LoreAuthURL:                       loreAuthURL,
		LorePublicURL:                     lorePublicURL,
		LoreInternalURL:                   loreInternalURL,
		LoreAuthAddress:                   envOrDefault("LOREHUB_LORE_AUTH_ADDRESS", ":8443"),
		LoreAuthCompatAddress:             os.Getenv("LOREHUB_LORE_AUTH_COMPAT_ADDRESS"),
		LoreAuthTLSCert:                   loreAuthTLSCert,
		LoreAuthTLSKey:                    loreAuthTLSKey,
		AuthSigningKeyPath:                signingKeyPath,
		AuthSigningKeyPEM:                 os.Getenv("LOREHUB_AUTH_SIGNING_KEY"),
		AuthSigningKeyKID:                 signingKeyKID,
		AuthPreviousKeys:                  os.Getenv("LOREHUB_AUTH_PREVIOUS_KEYS"),
		PolicyAddress:                     envOrDefault("LOREHUB_POLICY_ADDRESS", ":8444"),
		PolicyTLSCert:                     policyTLSCert,
		PolicyTLSKey:                      policyTLSKey,
		PolicyTLSClientCA:                 policyTLSCA,
		LorePolicyEndpoint:                lorePolicyEndpoint,
		LoreObservationEndpoint:           loreObservationEndpoint,
		LoreBinary:                        envOrDefault("LOREHUB_LORE_BINARY", "lore"),
		ActBinary:                         envOrDefault("LOREHUB_ACT_BINARY", "act"),
		RunnerPollPeriod:                  durationOrDefault("LOREHUB_RUNNER_POLL_PERIOD", 2*time.Second),
		BranchPollPeriod:                  durationOrDefault("LOREHUB_BRANCH_POLL_PERIOD", 15*time.Second),
		RunnerJobTimeout:                  durationOrDefault("LOREHUB_RUNNER_JOB_TIMEOUT", 60*time.Minute),
		RunnerLeaseDuration:               durationOrDefault("LOREHUB_RUNNER_LEASE_DURATION", 2*time.Minute),
		RunnerLogMaxBytes:                 logMaxBytes,
		RunnerLogMaxLineBytes:             logMaxLineBytes,
		RunnerArtifactMaxCount:            artifactMaxCount,
		RunnerArtifactMaxFile:             artifactMaxFile,
		RunnerArtifactMaxTotal:            artifactMaxTotal,
		RunnerLogDir:                      envOrDefault("LOREHUB_RUNNER_LOG_DIR", ".cache/lorehub/runner-logs"),
		RunnerArtifactDir:                 envOrDefault("LOREHUB_RUNNER_ARTIFACT_DIR", ".cache/lorehub/runner-artifacts"),
		RunnerWorkDir:                     envOrDefault("LOREHUB_RUNNER_WORK_DIR", ".cache/lorehub/runner-work"),
		RunnerProxyURL:                    envOrDefault("LOREHUB_RUNNER_PROXY_URL", "http://172.28.241.10:3128"),
		RunnerEngineProxyURL:              envOrDefault("LOREHUB_RUNNER_ENGINE_PROXY_URL", "http://172.28.241.10:3128"),
		RunnerPlatformImages:              platformImages,
		DevActionsContext:                 devActionsContext,
		DevActionsContextFallback:         devActionsContextFallback,
		DevActionsJobToken:                os.Getenv("LOREHUB_DEV_ACTIONS_JOB_TOKEN"),
		DevActionsJobTokenFallback:        devActionsJobTokenFallback,
	}
	if err := applyNotificationEmailConfig(&config, command); err != nil {
		return Config{}, err
	}

	if err := validateWebhookConfig(config, command); err != nil {
		return Config{}, err
	}
	if err := validateRepositoryDeletionConfig(config, command); err != nil {
		return Config{}, err
	}
	if err := validate(config, command); err != nil {
		return Config{}, err
	}
	absCacheDir, err := filepath.Abs(config.LoreCacheDir)
	if err != nil {
		return Config{}, fmt.Errorf("resolve Lore cache directory: %w", err)
	}
	config.LoreCacheDir = absCacheDir
	for source, target := range map[string]*string{
		config.RunnerArtifactDir: &config.RunnerArtifactDir,
		config.RunnerLogDir:      &config.RunnerLogDir,
		config.RunnerWorkDir:     &config.RunnerWorkDir,
	} {
		absolute, err := filepath.Abs(source)
		if err != nil {
			return Config{}, fmt.Errorf("resolve runner directory: %w", err)
		}
		*target = absolute
	}

	return config, nil
}

func loadMigrationConfig() (Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if strings.TrimSpace(databaseURL) == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	databaseTimeout, err := durationSetting("LOREHUB_DATABASE_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	if databaseTimeout <= 0 {
		return Config{}, errors.New("LOREHUB_DATABASE_TIMEOUT must be greater than zero")
	}
	return Config{DatabaseURL: databaseURL, DatabaseTimeout: databaseTimeout}, nil
}

func configuredIdentityProviders() []string {
	providers := make([]string, 0, 4)
	for _, setting := range identityProviderSettings() {
		if strings.TrimSpace(os.Getenv(setting.client)) != "" &&
			strings.TrimSpace(os.Getenv(setting.secret)) != "" {
			providers = append(providers, setting.id)
		}
	}
	return providers
}

func commaSeparatedUsernames(value string) []string {
	usernames := make([]string, 0)
	seen := make(map[string]struct{})
	for _, entry := range strings.Split(value, ",") {
		username := strings.ToLower(strings.TrimSpace(entry))
		if username == "" {
			continue
		}
		if _, duplicate := seen[username]; duplicate {
			continue
		}
		seen[username] = struct{}{}
		usernames = append(usernames, username)
	}
	return usernames
}

type identityProviderSetting struct {
	id     string
	client string
	secret string
}

func identityProviderSettings() []identityProviderSetting {
	return []identityProviderSetting{
		{id: "google", client: "LOREHUB_IDP_GOOGLE_CLIENT_ID", secret: "LOREHUB_IDP_GOOGLE_CLIENT_SECRET"},
		{id: "github", client: "LOREHUB_IDP_GITHUB_CLIENT_ID", secret: "LOREHUB_IDP_GITHUB_CLIENT_SECRET"},
		{id: "facebook", client: "LOREHUB_IDP_FACEBOOK_CLIENT_ID", secret: "LOREHUB_IDP_FACEBOOK_CLIENT_SECRET"},
		{id: "x", client: "LOREHUB_IDP_X_CLIENT_ID", secret: "LOREHUB_IDP_X_CLIENT_SECRET"},
	}
}

func authModeFromEnvironment(
	requested string,
	issuer string,
	audience string,
	clientID string,
	clientSecret string,
	redirectURL string,
) string {
	if requested != "" {
		return strings.ToLower(strings.TrimSpace(requested))
	}
	if clientID != "" || clientSecret != "" || redirectURL != "" {
		return AuthModeInteractive
	}
	if issuer != "" || audience != "" {
		return AuthModeBearer
	}
	return AuthModeDisabled
}

func validate(config Config, command string) error {
	if config.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	if config.Environment != "development" && config.Environment != "local" &&
		(strings.TrimSpace(config.DevLoreIdentity) != "" || config.DevLoreIdentityFallback) {
		return errors.New("development-only Lore identity fallback is disabled outside development and local")
	}
	if config.Environment != "development" && config.Environment != "local" && config.DevActionsContextFallback {
		return errors.New("development-only Actions context fallback is disabled outside development and local")
	}
	if config.Environment != "development" && config.Environment != "local" && config.DevActionsJobTokenFallback {
		return errors.New("development-only Actions job token fallback is disabled outside development and local")
	}
	if config.DevLoreIdentityFallback && strings.TrimSpace(config.DevLoreIdentity) == "" {
		return errors.New("LOREHUB_DEV_LORE_IDENTITY is required when the development fallback is enabled")
	}
	if config.DevActionsJobTokenFallback && strings.TrimSpace(config.DevActionsJobToken) == "" {
		return errors.New("LOREHUB_DEV_ACTIONS_JOB_TOKEN is required when the development fallback is enabled")
	}
	if err := validateURL("LOREHUB_PUBLIC_ORIGIN", config.PublicOrigin, config.Environment, true); err != nil {
		return err
	}
	for name, value := range map[string]string{
		"LOREHUB_PUBLIC_API_URL":     config.PublicAPIURL,
		"LOREHUB_PUBLIC_GRAPHQL_URL": config.PublicGraphQLURL,
	} {
		if err := validateURL(name, value, config.Environment, false); err != nil {
			return err
		}
	}
	if err := validateURL("LOREHUB_ACTION_SOURCE_URL", config.ActionSourceURL, config.Environment, true); err != nil {
		return err
	}
	parsedPublicOrigin, _ := url.Parse(config.PublicOrigin)
	for name, value := range map[string]string{
		"LOREHUB_PUBLIC_API_URL":     config.PublicAPIURL,
		"LOREHUB_PUBLIC_GRAPHQL_URL": config.PublicGraphQLURL,
	} {
		candidate, _ := url.Parse(value)
		if !sameOrigin(parsedPublicOrigin, candidate) {
			return fmt.Errorf("%s must use the LOREHUB_PUBLIC_ORIGIN origin", name)
		}
	}
	if err := validatePlatformImages(config.RunnerPlatformImages); err != nil {
		return err
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`).MatchString(config.ActionsSecretKeyID) {
		return errors.New("LOREHUB_ACTIONS_SECRET_KEY_ID is invalid")
	}
	if strings.ContainsAny(config.ActionsSecretKey, "\r\n") {
		return errors.New("LOREHUB_ACTIONS_SECRET_KEY must be unbroken base64 for exactly 32 bytes")
	}
	actionsSecretKey, err := base64.StdEncoding.Strict().DecodeString(config.ActionsSecretKey)
	if err != nil || len(actionsSecretKey) != 32 {
		clear(actionsSecretKey)
		return errors.New("LOREHUB_ACTIONS_SECRET_KEY must be base64 for exactly 32 bytes")
	}
	clear(actionsSecretKey)
	if err := validateURL(
		"LOREHUB_ACTIONS_JOB_TOKEN_AUDIENCE",
		config.ActionsJobTokenAudience,
		config.Environment,
		false,
	); err != nil {
		return err
	}
	actionsAudience, _ := url.Parse(config.ActionsJobTokenAudience)
	if parsedPublicOrigin != nil && !sameOrigin(parsedPublicOrigin, actionsAudience) {
		return errors.New("LOREHUB_ACTIONS_JOB_TOKEN_AUDIENCE must use the public LoreHub origin")
	}
	if config.AuthMode != AuthModeDisabled && config.AuthMode != AuthModeBearer &&
		config.AuthMode != AuthModeInteractive {
		return fmt.Errorf("LOREHUB_AUTH_MODE must be %q, %q, or %q", AuthModeDisabled, AuthModeBearer,
			AuthModeInteractive)
	}
	if config.SessionTTL <= 0 || config.SessionTTL > maxSessionLifetime {
		return fmt.Errorf("LOREHUB_SESSION_TTL must be greater than zero and no longer than %s", maxSessionLifetime)
	}
	if config.LoginTransactionTTL <= 0 || config.LoginTransactionTTL > maxTransactionLife {
		return fmt.Errorf("LOREHUB_LOGIN_TRANSACTION_TTL must be greater than zero and no longer than %s",
			maxTransactionLife)
	}
	if err := validateCookie(config); err != nil {
		return err
	}
	if config.AuthMode == AuthModeBearer || config.AuthMode == AuthModeInteractive {
		if config.OIDCIssuer == "" || config.OIDCAudience == "" {
			return errors.New("OIDC issuer and audience are required for bearer or interactive authentication")
		}
		if err := validateURL("LOREHUB_OIDC_ISSUER", config.OIDCIssuer, config.Environment, false); err != nil {
			return err
		}
	}
	if command == "serve" && config.Environment == "production" && config.AuthMode == AuthModeDisabled {
		return errors.New("LOREHUB_AUTH_MODE=disabled is limited to an isolated local profile")
	}
	if config.Environment != "development" && config.Environment != "test" &&
		config.Environment != "local-insecure" && config.LoreAllowDevelopmentFallback {
		return errors.New("LOREHUB_LORE_ALLOW_DEVELOPMENT_FALLBACK is limited to isolated local profiles")
	}
	if config.Environment == "production" && strings.TrimSpace(config.LoreIdentity) != "" {
		return errors.New("LOREHUB_LORE_IDENTITY is forbidden in production")
	}
	if strings.TrimSpace(config.LoreIdentity) != "" && !config.AllowLegacyLoreIdentity {
		return errors.New("LOREHUB_LORE_IDENTITY requires the explicit local-insecure profile")
	}
	if config.AllowLegacyLoreIdentity && (config.Environment != "local-insecure" ||
		config.AuthMode != AuthModeDisabled || strings.TrimSpace(config.LoreIdentity) == "") {
		return errors.New("legacy Lore identity requires LOREHUB_ENV=local-insecure and disabled API auth")
	}
	if config.Environment != "development" && config.Environment != "test" &&
		len(config.LoreCredentials) != 0 {
		return errors.New("LOREHUB_LORE_CREDENTIALS is limited to development and test")
	}
	if config.Environment != "development" && config.Environment != "test" &&
		strings.TrimSpace(config.LoreAuthAuthority) == "" {
		return errors.New("LOREHUB_LORE_AUTHORITY is required outside local development")
	}
	if config.LoreAuthAuthority != "" {
		if err := loreclient.ValidateAuthAuthority(config.LoreAuthAuthority); err != nil {
			return fmt.Errorf("invalid Lore auth authority: %w", err)
		}
	}
	if config.Environment != "development" && config.Environment != "test" {
		for name, subject := range map[string]string{
			"LOREHUB_LORE_PUBLIC_READER_SUBJECT":           config.LorePublicReaderSubject,
			"LOREHUB_LORE_ACTIONS_RUNNER_SUBJECT":          config.LoreActionsRunnerSubject,
			"LOREHUB_OBSERVER_SERVICE_PRINCIPAL":           config.LoreObserverSubject,
			"LOREHUB_LORE_REPOSITORY_REGISTRATION_SUBJECT": config.LoreRepositoryRegistrationSubject,
		} {
			if err := loreclient.ValidateServiceSubject(subject); err != nil {
				return fmt.Errorf("%s is required and must be an exact subject: %w", name, err)
			}
		}
	}
	if config.LoreAuthIssuer == "" || config.LoreAuthAudience == "" || config.LoreRootDomain == "" ||
		config.LoreAuthJWKSURL == "" || config.AuthSigningKeyKID == "" {
		return errors.New("Lore auth issuer, audience, root domain, JWKS URL, and signing key kid are required")
	}
	if err := validateRootDomain(config.LoreRootDomain, config.Environment); err != nil {
		return err
	}
	if !validManagedDomain(config.LoreAuthIssuer, config.LoreRootDomain) {
		return errors.New("LOREHUB_LORE_AUTH_ISSUER must use the managed Lore root domain")
	}
	if err := validateURL("LOREHUB_LORE_AUTH_LOGIN_URL", config.LoreAuthLoginURL, config.Environment, false); err != nil {
		return err
	}
	loginURL, _ := url.Parse(config.LoreAuthLoginURL)
	if loginURL.Path != "/auth/lore/confirm" || !hostWithinRoot(loginURL.Hostname(), config.LoreRootDomain) {
		return errors.New("LOREHUB_LORE_AUTH_LOGIN_URL must use the managed Lore root domain")
	}
	if err := validateURL("LOREHUB_LORE_AUTH_JWKS_URL", config.LoreAuthJWKSURL, config.Environment, false); err != nil {
		return err
	}
	jwksURL, _ := url.Parse(config.LoreAuthJWKSURL)
	if jwksURL.Path != "/.well-known/jwks.json" || !hostWithinRoot(jwksURL.Hostname(), config.LoreRootDomain) {
		return errors.New("LOREHUB_LORE_AUTH_JWKS_URL must use the managed Lore root domain")
	}
	for name, endpoint := range map[string]string{
		"LOREHUB_LORE_POLICY_ENDPOINT":      config.LorePolicyEndpoint,
		"LOREHUB_LORE_OBSERVATION_ENDPOINT": config.LoreObservationEndpoint,
	} {
		if err := validateURL(name, endpoint, config.Environment, false); err != nil {
			return err
		}
		parsedEndpoint, _ := url.Parse(endpoint)
		expectedPath := "/internal/lore/policy"
		if name == "LOREHUB_LORE_OBSERVATION_ENDPOINT" {
			expectedPath = "/internal/lore/observation"
		}
		if parsedEndpoint.Path != expectedPath || !hostWithinRoot(parsedEndpoint.Hostname(), config.LoreRootDomain) {
			return fmt.Errorf("%s must use the managed Lore root domain", name)
		}
	}
	if strings.Contains(config.LoreAuthAudience, ",") {
		return errors.New("LOREHUB_LORE_AUTH_AUDIENCE must contain one managed root domain")
	}
	for _, audience := range strings.Split(config.LoreAuthAudience, ",") {
		audience = strings.TrimSpace(audience)
		if audience == "" || !hostWithinRoot(audience, config.LoreRootDomain) ||
			audience == "lore" || audience == "api" {
			return errors.New("LOREHUB_LORE_AUTH_AUDIENCE must contain managed root domains")
		}
	}
	authURL, authURLErr := url.Parse(config.LoreAuthURL)
	if authURLErr != nil || authURL.Scheme != "ucs-auth" || authURL.Host == "" ||
		authURL.User != nil || authURL.RawQuery != "" || authURL.Fragment != "" ||
		authURL.Path != "" || authURL.RawPath != "" || authURL.ForceQuery ||
		strings.ContainsAny(config.LoreAuthURL, "\r\n") {
		return errors.New("LOREHUB_LORE_AUTH_URL must be a fixed ucs-auth:// endpoint")
	}
	if err := loreclient.ValidateAuthAuthority(authURL.Host); err != nil {
		return errors.New("LOREHUB_LORE_AUTH_URL has an invalid authority")
	}
	if !hostWithinRoot(authURL.Hostname(), config.LoreRootDomain) {
		return errors.New("LOREHUB_LORE_AUTH_URL must use the managed Lore root domain")
	}
	publicURL, publicURLErr := url.Parse(config.LorePublicURL)
	if publicURLErr != nil || publicURL.Scheme != "lores" || publicURL.Host == "" ||
		publicURL.User != nil || publicURL.Path != "" || publicURL.RawQuery != "" || publicURL.Fragment != "" ||
		!hostWithinRoot(publicURL.Hostname(), config.LoreRootDomain) {
		return errors.New("LOREHUB_LORE_PUBLIC_URL must be a fixed managed lores:// endpoint")
	}
	internalURL, internalURLErr := url.Parse(config.LoreInternalURL)
	if internalURLErr != nil || internalURL.Scheme != "lores" || internalURL.Host == "" ||
		internalURL.User != nil || internalURL.Path != "" || internalURL.RawQuery != "" ||
		internalURL.Fragment != "" || !hostWithinRoot(internalURL.Hostname(), config.LoreRootDomain) {
		return errors.New("LOREHUB_LORE_INTERNAL_URL must be a fixed managed lores:// endpoint")
	}
	if config.LoreAuthTokenTTL < 5*time.Minute || config.LoreAuthTokenTTL > 10*time.Minute {
		return errors.New("LOREHUB_LORE_AUTH_TOKEN_TTL must be between 5m and 10m")
	}
	if config.LoreAuthSessionTTL <= 0 || config.LoreAuthSessionTTL > 10*time.Minute {
		return errors.New("LOREHUB_LORE_AUTH_SESSION_TTL must be between one second and 10m")
	}
	if config.Environment == "production" {
		if config.AuthSigningKeyPEM == "" && config.AuthSigningKeyPath == "" {
			return errors.New("a production Lore signing key must be configured")
		}
		for name, value := range map[string]string{
			"LOREHUB_LORE_AUTH_TLS_CERT":   config.LoreAuthTLSCert,
			"LOREHUB_LORE_AUTH_TLS_KEY":    config.LoreAuthTLSKey,
			"LOREHUB_POLICY_TLS_CERT":      config.PolicyTLSCert,
			"LOREHUB_POLICY_TLS_KEY":       config.PolicyTLSKey,
			"LOREHUB_POLICY_TLS_CLIENT_CA": config.PolicyTLSClientCA,
		} {
			if value == "" {
				return fmt.Errorf("%s is required in production", name)
			}
		}
	}
	if command == "serve" && config.AuthMode != AuthModeDisabled {
		if config.AuthSecret == "" {
			return errors.New("LOREHUB_AUTH_SECRET is required when authentication is enabled")
		}
		if len(config.AuthSecret) < 32 {
			return errors.New("LOREHUB_AUTH_SECRET must contain at least 32 characters")
		}
	}
	if command != "serve" || config.AuthMode != AuthModeInteractive {
		return validateRunner(config)
	}
	for name, value := range map[string]string{
		"LOREHUB_OIDC_CLIENT_ID":     config.OIDCClientID,
		"LOREHUB_OIDC_CLIENT_SECRET": config.OIDCClientSecret,
		"LOREHUB_OIDC_REDIRECT_URL":  config.OIDCRedirectURL,
		"LOREHUB_PUBLIC_ORIGIN":      config.PublicOrigin,
	} {
		if value == "" {
			return fmt.Errorf("%s is required when interactive authentication is enabled", name)
		}
	}
	if err := validateURL("LOREHUB_OIDC_REDIRECT_URL", config.OIDCRedirectURL, config.Environment, false); err != nil {
		return err
	}
	redirectURL, _ := url.Parse(config.OIDCRedirectURL)
	publicOrigin, _ := url.Parse(config.PublicOrigin)
	if !sameOrigin(redirectURL, publicOrigin) {
		return errors.New("LOREHUB_OIDC_REDIRECT_URL must use the LOREHUB_PUBLIC_ORIGIN origin")
	}
	return validateRunner(config)
}

func validateRunner(config Config) error {
	if config.RunnerPollPeriod <= 0 || config.BranchPollPeriod <= 0 || config.RunnerJobTimeout <= 0 ||
		config.RunnerJobTimeout > maxRunnerJobTimeout ||
		config.RunnerLeaseDuration <= 0 {
		return errors.New("runner polling, lease, and job timeout durations are outside their bounds")
	}
	if config.RunnerLeaseDuration >= config.RunnerJobTimeout {
		return errors.New("LOREHUB_RUNNER_LEASE_DURATION must be shorter than LOREHUB_RUNNER_JOB_TIMEOUT")
	}
	if config.RunnerLogMaxBytes <= 0 || config.RunnerLogMaxLineBytes <= 0 ||
		config.RunnerLogMaxLineBytes > config.RunnerLogMaxBytes ||
		config.RunnerArtifactMaxCount <= 0 || config.RunnerArtifactMaxFile <= 0 ||
		config.RunnerArtifactMaxTotal <= 0 || config.RunnerArtifactMaxFile > config.RunnerArtifactMaxTotal {
		return errors.New("runner log and artifact quotas are invalid")
	}
	return nil
}

func parseLoreCredentials(value string) (map[string]loreclient.CredentialMaterial, error) {
	if strings.TrimSpace(value) == "" {
		return map[string]loreclient.CredentialMaterial{}, nil
	}
	var result map[string]loreclient.CredentialMaterial
	if err := json.Unmarshal([]byte(value), &result); err != nil || result == nil {
		return nil, errors.New("LOREHUB_LORE_CREDENTIALS must be a JSON object keyed by partition")
	}
	for partition, material := range result {
		if strings.TrimSpace(partition) == "" {
			return nil, errors.New("LOREHUB_LORE_CREDENTIALS contains an empty partition")
		}
		result[partition] = loreclient.CredentialMaterial{
			Identity: strings.TrimSpace(material.Identity),
			Token:    strings.TrimSpace(material.Token),
			AuthURL:  strings.TrimSpace(material.AuthURL),
		}
	}
	return result, nil
}

func validateCookie(config Config) error {
	if invalidCookieName(config.SessionCookieName) {
		return errors.New("LOREHUB_SESSION_COOKIE_NAME is invalid")
	}
	if invalidCookieName(config.LoginBindingCookieName) {
		return errors.New("LOREHUB_LOGIN_BINDING_COOKIE_NAME is invalid")
	}
	if config.SessionCookiePath == "" || !strings.HasPrefix(config.SessionCookiePath, "/") ||
		strings.ContainsAny(config.SessionCookiePath, "\r\n") {
		return errors.New("LOREHUB_SESSION_COOKIE_PATH must be an absolute cookie path")
	}
	if config.SessionCookieDomain != "" && strings.ContainsAny(config.SessionCookieDomain, " /;\\\t\r\n") {
		return errors.New("LOREHUB_SESSION_COOKIE_DOMAIN is invalid")
	}
	return nil
}

func invalidCookieName(value string) bool {
	return value == "" || strings.ContainsAny(value, " ;,\t\r\n")
}

func boolSetting(name string, defaultValue bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return defaultValue, nil
	}
	switch strings.ToLower(value) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be true or false", name)
	}
}

func validateURL(name string, value string, environment string, originOnly bool) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil ||
		parsed.Fragment != "" || parsed.RawFragment != "" || parsed.Opaque != "" ||
		strings.ContainsAny(value, "\x00\r\n\t") {
		return fmt.Errorf("%s must be an absolute HTTP or HTTPS URL", name)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use HTTP or HTTPS", name)
	}
	if originOnly && (parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "") {
		return fmt.Errorf("%s must contain only an origin", name)
	}
	if environment != "development" && environment != "local" && environment != "local-insecure" &&
		parsed.Scheme != "https" {
		return fmt.Errorf("%s must use HTTPS outside the isolated local profiles", name)
	}
	hostname := parsed.Hostname()
	localHost := (environment == "development" || environment == "local" || environment == "local-insecure") &&
		hostname == "localhost"
	if hostname == "" || (!validDNSHost(hostname) && !localHost) {
		return fmt.Errorf("%s must use a valid DNS host", name)
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		return fmt.Errorf("%s must not contain a query", name)
	}
	return nil
}

func validateRootDomain(value string, environment string) error {
	value = strings.TrimSpace(strings.ToLower(value))
	if !validDNSHost(value) {
		return errors.New("LOREHUB_LORE_ROOT_DOMAIN must be a DNS root domain")
	}
	if environment == "production" && strings.HasSuffix(value, ".localhost") {
		return errors.New("LOREHUB_LORE_ROOT_DOMAIN cannot use localhost in production")
	}
	return nil
}

func hostWithinRoot(host string, root string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	root = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(root)), ".")
	return validDNSHost(host) && validDNSHost(root) && (host == root || strings.HasSuffix(host, "."+root))
}

func validManagedDomain(value string, root string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return validDNSHost(value) && hostWithinRoot(value, root)
}

func validDNSHost(value string) bool {
	value = strings.TrimSuffix(strings.TrimSpace(strings.ToLower(value)), ".")
	if value == "" || len(value) > 253 || strings.ContainsAny(value, "/:?#@ \t\r\n") {
		return false
	}
	labels := strings.Split(value, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character >= 'a' && character <= 'z') ||
				(character >= '0' && character <= '9') || character == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func sameOrigin(left *url.URL, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func validatePlatformImages(images map[string]string) error {
	labelPattern := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	for label, image := range images {
		if !labelPattern.MatchString(label) {
			return fmt.Errorf("LOREHUB_RUNNER_PLATFORM_IMAGES label %q is invalid", label)
		}
		if image == "" || strings.TrimSpace(image) != image || strings.ContainsAny(image, "\r\n\t ;'\"$`()") ||
			strings.Contains(image, "@") || !strings.Contains(image, ":") {
			return fmt.Errorf("LOREHUB_RUNNER_PLATFORM_IMAGES image for %q is invalid", label)
		}
	}
	return nil
}

func parseJSONMapSetting(name string) (map[string]string, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil, nil
	}
	var values map[string]string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, fmt.Errorf("%s must be a JSON object of strings: %w", name, err)
	}
	return values, nil
}

func parseDevActionsContext(raw string) (DevActionsContext, error) {
	if strings.TrimSpace(raw) == "" {
		return DevActionsContext{}, nil
	}
	var value DevActionsContext
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return DevActionsContext{}, fmt.Errorf("LOREHUB_DEV_ACTIONS_CONTEXT_JSON must be valid JSON: %w", err)
	}
	return value, nil
}

func envOrDefault(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func defaultIfEmpty(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func durationOrDefault(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err == nil {
		return duration
	}
	seconds, err := strconv.Atoi(value)
	if err == nil {
		return time.Duration(seconds) * time.Second
	}
	return fallback
}

func durationSetting(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err == nil {
		return duration, nil
	}
	seconds, secondsErr := strconv.Atoi(value)
	if secondsErr == nil {
		return time.Duration(seconds) * time.Second, nil
	}
	return 0, fmt.Errorf("%s must be a duration such as 10m or a number of seconds", key)
}

func byteSetting(key string, fallback int64) (int64, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive byte count", key)
	}
	return parsed, nil
}

func intSetting(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return parsed, nil
}

func cookieSecureSetting(environment string) (bool, error) {
	value := os.Getenv("LOREHUB_SESSION_COOKIE_SECURE")
	if value == "" {
		return environment != "development" && environment != "local", nil
	}
	secure, err := strconv.ParseBool(value)
	if err != nil {
		return false, errors.New("LOREHUB_SESSION_COOKIE_SECURE must be true or false")
	}
	return secure, nil
}
