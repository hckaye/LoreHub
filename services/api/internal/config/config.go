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
	Environment            string
	HTTPAddress            string
	DatabaseURL            string
	DatabaseTimeout        time.Duration
	ShutdownTimeout        time.Duration
	AuthMode               string
	OIDCIssuer             string
	OIDCAudience           string
	OIDCClientID           string
	OIDCClientSecret       string
	OIDCRedirectURL        string
	PublicOrigin           string
	AuthSecret             string
	SessionCookieName      string
	LoginBindingCookieName string
	SessionCookiePath      string
	SessionCookieDomain    string
	SessionCookieSecure    bool
	SessionTTL             time.Duration
	LoginTransactionTTL    time.Duration
	LoreCacheDir           string
	LoreIdentity           string
	LoreBinary             string
	ActBinary              string
	RunnerPollPeriod       time.Duration
	BranchPollPeriod       time.Duration
	RunnerJobTimeout       time.Duration
	RunnerLogDir           string
	RunnerArtifactDir      string
	RunnerWorkDir          string
}

func Load() (Config, error) {
	environment := envOrDefault("LOREHUB_ENV", "development")
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
		Environment:            environment,
		HTTPAddress:            envOrDefault("LOREHUB_HTTP_ADDRESS", ":8080"),
		DatabaseURL:            os.Getenv("DATABASE_URL"),
		DatabaseTimeout:        durationOrDefault("LOREHUB_DATABASE_TIMEOUT", 10*time.Second),
		ShutdownTimeout:        durationOrDefault("LOREHUB_SHUTDOWN_TIMEOUT", 15*time.Second),
		AuthMode:               authMode,
		OIDCIssuer:             oidcIssuer,
		OIDCAudience:           oidcAudience,
		OIDCClientID:           oidcClientID,
		OIDCClientSecret:       os.Getenv("LOREHUB_OIDC_CLIENT_SECRET"),
		OIDCRedirectURL:        os.Getenv("LOREHUB_OIDC_REDIRECT_URL"),
		PublicOrigin:           strings.TrimRight(os.Getenv("LOREHUB_PUBLIC_ORIGIN"), "/"),
		AuthSecret:             os.Getenv("LOREHUB_AUTH_SECRET"),
		SessionCookieName:      envOrDefault("LOREHUB_SESSION_COOKIE_NAME", "lorehub_session"),
		LoginBindingCookieName: envOrDefault("LOREHUB_LOGIN_BINDING_COOKIE_NAME", "lorehub_login_binding"),
		SessionCookiePath:      envOrDefault("LOREHUB_SESSION_COOKIE_PATH", "/"),
		SessionCookieDomain:    os.Getenv("LOREHUB_SESSION_COOKIE_DOMAIN"),
		SessionCookieSecure:    cookieSecure,
		SessionTTL:             sessionTTL,
		LoginTransactionTTL:    transactionTTL,
		LoreCacheDir:           envOrDefault("LOREHUB_LORE_CACHE_DIR", ".cache/lorehub/repositories"),
		LoreIdentity:           os.Getenv("LOREHUB_LORE_IDENTITY"),
		LoreBinary:             envOrDefault("LOREHUB_LORE_BINARY", "lore"),
		ActBinary:              envOrDefault("LOREHUB_ACT_BINARY", "act"),
		RunnerPollPeriod:       durationOrDefault("LOREHUB_RUNNER_POLL_PERIOD", 2*time.Second),
		BranchPollPeriod:       durationOrDefault("LOREHUB_BRANCH_POLL_PERIOD", 15*time.Second),
		RunnerJobTimeout:       durationOrDefault("LOREHUB_RUNNER_JOB_TIMEOUT", 60*time.Minute),
		RunnerLogDir:           envOrDefault("LOREHUB_RUNNER_LOG_DIR", ".cache/lorehub/runner-logs"),
		RunnerArtifactDir:      envOrDefault("LOREHUB_RUNNER_ARTIFACT_DIR", ".cache/lorehub/runner-artifacts"),
		RunnerWorkDir:          envOrDefault("LOREHUB_RUNNER_WORK_DIR", ".cache/lorehub/runner-work"),
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
	if environment == "production" && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use HTTPS in production", name)
	}
	return nil
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
