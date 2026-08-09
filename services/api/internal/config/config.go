package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	AuthModeDisabled    = "disabled"
	AuthModeBearer      = "bearer"
	AuthModeInteractive = "interactive"
	maxSessionLifetime  = 30 * 24 * time.Hour
	maxTransactionLife  = 15 * time.Minute
)

type Config struct {
	Environment              string
	HTTPAddress              string
	DatabaseURL              string
	DatabaseTimeout          time.Duration
	ShutdownTimeout          time.Duration
	AuthMode                 string
	OIDCIssuer               string
	OIDCAudience             string
	OIDCClientID             string
	OIDCClientSecret         string
	OIDCRedirectURL          string
	PublicOrigin             string
	AuthSecret               string
	SessionCookieName        string
	LoginBindingCookieName   string
	SessionCookiePath        string
	SessionCookieDomain      string
	SessionCookieSecure      bool
	SessionTTL               time.Duration
	LoginTransactionTTL      time.Duration
	LoreCacheDir             string
	LoreIdentity             string
	AllowLegacyLoreIdentity  bool
	LoreAuthIssuer           string
	LoreAuthAudience         string
	LoreRootDomain           string
	LoreAuthJWKSURL          string
	LoreAuthEnvironment      string
	LoreAuthIDP              string
	LoreAuthTokenTTL         time.Duration
	LoreAuthSessionTTL       time.Duration
	LoreAuthLoginURL         string
	LoreAuthURL              string
	LorePublicURL            string
	LoreAuthAddress          string
	LoreAuthTLSCert          string
	LoreAuthTLSKey           string
	AuthSigningKeyPath       string
	AuthSigningKeyPEM        string
	AuthSigningKeyKID        string
	AuthPreviousKeys         string
	PolicyAddress            string
	PolicyTLSCert            string
	PolicyTLSKey             string
	PolicyTLSClientCA        string
	LoreBinary               string
	ActBinary                string
	RunnerPollPeriod         time.Duration
	BranchPollPeriod         time.Duration
	RunnerJobTimeout         time.Duration
	RunnerLogDir             string
	RunnerArtifactDir        string
	RunnerWorkDir            string
	RunnerServicePrincipal   string
	ObserverServicePrincipal string
}

func Load() (Config, error) {
	environment := envOrDefault("LOREHUB_ENV", "development")
	loreAuthIssuer := os.Getenv("LOREHUB_LORE_AUTH_ISSUER")
	loreAuthLoginURL := os.Getenv("LOREHUB_LORE_AUTH_LOGIN_URL")
	loreAuthURL := os.Getenv("LOREHUB_LORE_AUTH_URL")
	loreRootDomain := os.Getenv("LOREHUB_LORE_ROOT_DOMAIN")
	loreAuthJWKSURL := os.Getenv("LOREHUB_LORE_AUTH_JWKS_URL")
	lorePublicURL := os.Getenv("LOREHUB_LORE_PUBLIC_URL")
	signingKeyPath := os.Getenv("LOREHUB_AUTH_SIGNING_KEY_PATH")
	loreAuthTLSCert := os.Getenv("LOREHUB_LORE_AUTH_TLS_CERT")
	loreAuthTLSKey := os.Getenv("LOREHUB_LORE_AUTH_TLS_KEY")
	policyTLSCert := os.Getenv("LOREHUB_POLICY_TLS_CERT")
	policyTLSKey := os.Getenv("LOREHUB_POLICY_TLS_KEY")
	policyTLSCA := os.Getenv("LOREHUB_POLICY_TLS_CLIENT_CA")
	signingKeyKID := os.Getenv("LOREHUB_AUTH_SIGNING_KEY_KID")
	allowLegacyLoreIdentity, err := boolSetting("LOREHUB_ALLOW_LEGACY_LORE_IDENTITY", false)
	if err != nil {
		return Config{}, err
	}
	if environment != "production" {
		loreRootDomain = defaultIfEmpty(loreRootDomain, "lorehub.localhost")
		loreAuthIssuer = defaultIfEmpty(loreAuthIssuer, "auth.lorehub.localhost")
		loreAuthLoginURL = defaultIfEmpty(loreAuthLoginURL,
			"http://lorehub.localhost:8080/auth/lore/confirm")
		loreAuthURL = defaultIfEmpty(loreAuthURL, "ucs-auth://auth.lorehub.localhost:8443")
		loreAuthJWKSURL = defaultIfEmpty(loreAuthJWKSURL,
			"http://api.lorehub.localhost:8080/.well-known/jwks.json")
		lorePublicURL = defaultIfEmpty(lorePublicURL, "lores://lore.lorehub.localhost:41337")
		signingKeyPath = defaultIfEmpty(signingKeyPath, "/var/lib/lorehub/keys/lore-auth.pem")
		loreAuthTLSCert = defaultIfEmpty(loreAuthTLSCert, "/var/lib/lorehub/tls/server.crt")
		loreAuthTLSKey = defaultIfEmpty(loreAuthTLSKey, "/var/lib/lorehub/tls/server.key")
		policyTLSCert = defaultIfEmpty(policyTLSCert, "/var/lib/lorehub/tls/server.crt")
		policyTLSKey = defaultIfEmpty(policyTLSKey, "/var/lib/lorehub/tls/server.key")
		policyTLSCA = defaultIfEmpty(policyTLSCA, "/var/lib/lorehub/tls/ca.crt")
		signingKeyKID = defaultIfEmpty(signingKeyKID, "local-current")
	}
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
	config := Config{
		Environment:             environment,
		HTTPAddress:             envOrDefault("LOREHUB_HTTP_ADDRESS", ":8080"),
		DatabaseURL:             os.Getenv("DATABASE_URL"),
		DatabaseTimeout:         durationOrDefault("LOREHUB_DATABASE_TIMEOUT", 10*time.Second),
		ShutdownTimeout:         durationOrDefault("LOREHUB_SHUTDOWN_TIMEOUT", 15*time.Second),
		AuthMode:                authMode,
		OIDCIssuer:              oidcIssuer,
		OIDCAudience:            oidcAudience,
		OIDCClientID:            oidcClientID,
		OIDCClientSecret:        os.Getenv("LOREHUB_OIDC_CLIENT_SECRET"),
		OIDCRedirectURL:         os.Getenv("LOREHUB_OIDC_REDIRECT_URL"),
		PublicOrigin:            strings.TrimRight(os.Getenv("LOREHUB_PUBLIC_ORIGIN"), "/"),
		AuthSecret:              os.Getenv("LOREHUB_AUTH_SECRET"),
		SessionCookieName:       envOrDefault("LOREHUB_SESSION_COOKIE_NAME", "lorehub_session"),
		LoginBindingCookieName:  envOrDefault("LOREHUB_LOGIN_BINDING_COOKIE_NAME", "lorehub_login_binding"),
		SessionCookiePath:       envOrDefault("LOREHUB_SESSION_COOKIE_PATH", "/"),
		SessionCookieDomain:     os.Getenv("LOREHUB_SESSION_COOKIE_DOMAIN"),
		SessionCookieSecure:     cookieSecure,
		SessionTTL:              sessionTTL,
		LoginTransactionTTL:     transactionTTL,
		LoreCacheDir:            envOrDefault("LOREHUB_LORE_CACHE_DIR", ".cache/lorehub/repositories"),
		LoreIdentity:            os.Getenv("LOREHUB_LORE_IDENTITY"),
		AllowLegacyLoreIdentity: allowLegacyLoreIdentity,
		LoreAuthIssuer:          loreAuthIssuer,
		LoreAuthAudience:        envOrDefault("LOREHUB_LORE_AUTH_AUDIENCE", loreRootDomain),
		LoreRootDomain:          loreRootDomain,
		LoreAuthJWKSURL:         loreAuthJWKSURL,
		LoreAuthEnvironment:     envOrDefault("LOREHUB_LORE_AUTH_ENVIRONMENT", environment),
		LoreAuthIDP:             envOrDefault("LOREHUB_LORE_AUTH_IDP", "keycloak"),
		LoreAuthTokenTTL:        durationOrDefault("LOREHUB_LORE_AUTH_TOKEN_TTL", 5*time.Minute),
		LoreAuthSessionTTL:      durationOrDefault("LOREHUB_LORE_AUTH_SESSION_TTL", 5*time.Minute),
		LoreAuthLoginURL:        loreAuthLoginURL,
		LoreAuthURL:             loreAuthURL,
		LorePublicURL:           lorePublicURL,
		LoreAuthAddress:         envOrDefault("LOREHUB_LORE_AUTH_ADDRESS", ":8443"),
		LoreAuthTLSCert:         loreAuthTLSCert,
		LoreAuthTLSKey:          loreAuthTLSKey,
		AuthSigningKeyPath:      signingKeyPath,
		AuthSigningKeyPEM:       os.Getenv("LOREHUB_AUTH_SIGNING_KEY"),
		AuthSigningKeyKID:       signingKeyKID,
		AuthPreviousKeys:        os.Getenv("LOREHUB_AUTH_PREVIOUS_KEYS"),
		PolicyAddress:           envOrDefault("LOREHUB_POLICY_ADDRESS", ":8444"),
		PolicyTLSCert:           policyTLSCert,
		PolicyTLSKey:            policyTLSKey,
		PolicyTLSClientCA:       policyTLSCA,
		LoreBinary:              envOrDefault("LOREHUB_LORE_BINARY", "lore"),
		ActBinary:               envOrDefault("LOREHUB_ACT_BINARY", "act"),
		RunnerPollPeriod:        durationOrDefault("LOREHUB_RUNNER_POLL_PERIOD", 2*time.Second),
		BranchPollPeriod:        durationOrDefault("LOREHUB_BRANCH_POLL_PERIOD", 15*time.Second),
		RunnerJobTimeout:        durationOrDefault("LOREHUB_RUNNER_JOB_TIMEOUT", 60*time.Minute),
		RunnerLogDir:            envOrDefault("LOREHUB_RUNNER_LOG_DIR", ".cache/lorehub/runner-logs"),
		RunnerArtifactDir:       envOrDefault("LOREHUB_RUNNER_ARTIFACT_DIR", ".cache/lorehub/runner-artifacts"),
		RunnerWorkDir:           envOrDefault("LOREHUB_RUNNER_WORK_DIR", ".cache/lorehub/runner-work"),
		RunnerServicePrincipal: strings.TrimSpace(envOrDefault(
			"LOREHUB_RUNNER_SERVICE_PRINCIPAL", "lorehub-ci-runner")),
		ObserverServicePrincipal: strings.TrimSpace(envOrDefault(
			"LOREHUB_OBSERVER_SERVICE_PRINCIPAL", "lorehub-observer")),
	}

	if err := validate(config); err != nil {
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

func validate(config Config) error {
	if config.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required")
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
	if config.Environment == "production" && config.AuthMode == AuthModeDisabled {
		return errors.New("LOREHUB_AUTH_MODE=disabled is limited to an isolated local profile")
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
	if !hostWithinRoot(loginURL.Hostname(), config.LoreRootDomain) {
		return errors.New("LOREHUB_LORE_AUTH_LOGIN_URL must use the managed Lore root domain")
	}
	if err := validateURL("LOREHUB_LORE_AUTH_JWKS_URL", config.LoreAuthJWKSURL, config.Environment, false); err != nil {
		return err
	}
	jwksURL, _ := url.Parse(config.LoreAuthJWKSURL)
	if !hostWithinRoot(jwksURL.Hostname(), config.LoreRootDomain) {
		return errors.New("LOREHUB_LORE_AUTH_JWKS_URL must use the managed Lore root domain")
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
		strings.ContainsAny(config.LoreAuthURL, "\r\n") {
		return errors.New("LOREHUB_LORE_AUTH_URL must be a fixed ucs-auth:// endpoint")
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
	if config.AuthMode != AuthModeInteractive {
		return nil
	}
	for name, value := range map[string]string{
		"LOREHUB_OIDC_CLIENT_ID":     config.OIDCClientID,
		"LOREHUB_OIDC_CLIENT_SECRET": config.OIDCClientSecret,
		"LOREHUB_OIDC_REDIRECT_URL":  config.OIDCRedirectURL,
		"LOREHUB_PUBLIC_ORIGIN":      config.PublicOrigin,
		"LOREHUB_AUTH_SECRET":        config.AuthSecret,
	} {
		if value == "" {
			return fmt.Errorf("%s is required when interactive authentication is enabled", name)
		}
	}
	if len(config.AuthSecret) < 32 {
		return errors.New("LOREHUB_AUTH_SECRET must contain at least 32 characters")
	}
	if err := validateURL("LOREHUB_OIDC_REDIRECT_URL", config.OIDCRedirectURL, config.Environment, false); err != nil {
		return err
	}
	if err := validateURL("LOREHUB_PUBLIC_ORIGIN", config.PublicOrigin, config.Environment, true); err != nil {
		return err
	}
	redirectURL, _ := url.Parse(config.OIDCRedirectURL)
	publicOrigin, _ := url.Parse(config.PublicOrigin)
	if !sameOrigin(redirectURL, publicOrigin) {
		return errors.New("LOREHUB_OIDC_REDIRECT_URL must use the LOREHUB_PUBLIC_ORIGIN origin")
	}
	return nil
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
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("%s must be an absolute HTTP or HTTPS URL", name)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use HTTP or HTTPS", name)
	}
	if originOnly && (parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "") {
		return fmt.Errorf("%s must contain only an origin", name)
	}
	if environment != "development" && environment != "local-insecure" && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use HTTPS outside the isolated local profiles", name)
	}
	return nil
}

func validateRootDomain(value string, environment string) error {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || strings.ContainsAny(value, "/:?#@ \t\r\n") || !strings.Contains(value, ".") {
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
	return host == root || strings.HasSuffix(host, "."+root)
}

func validManagedDomain(value string, root string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || strings.ContainsAny(value, "/:?#@ \t\r\n") || !strings.Contains(value, ".") {
		return false
	}
	return hostWithinRoot(value, root)
}

func sameOrigin(left *url.URL, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
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
