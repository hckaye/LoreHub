package config

import (
	"reflect"
	"strings"
	"testing"
)

func TestLoadDefaultsToDeterministicDisabledMode(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_ENV", "development")
	t.Setenv("LOREHUB_AUTH_MODE", "")
	t.Setenv("LOREHUB_OIDC_ISSUER", "")
	t.Setenv("LOREHUB_OIDC_AUDIENCE", "")
	t.Setenv("LOREHUB_OIDC_CLIENT_ID", "")
	t.Setenv("LOREHUB_OIDC_CLIENT_SECRET", "")
	t.Setenv("LOREHUB_OIDC_REDIRECT_URL", "")

	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.AuthMode != AuthModeDisabled || settings.SessionCookieSecure {
		t.Fatalf("unexpected development defaults: mode=%q secure=%t", settings.AuthMode, settings.SessionCookieSecure)
	}
}

func TestLoadRejectsIncompleteInteractiveAuthentication(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_AUTH_MODE", AuthModeInteractive)
	t.Setenv("LOREHUB_OIDC_ISSUER", "https://keycloak.example/realms/lorehub")
	t.Setenv("LOREHUB_OIDC_AUDIENCE", "lorehub-api")
	t.Setenv("LOREHUB_OIDC_CLIENT_ID", "lorehub-web")
	t.Setenv("LOREHUB_OIDC_CLIENT_SECRET", "")
	t.Setenv("LOREHUB_OIDC_REDIRECT_URL", "https://app.example/auth/callback")
	t.Setenv("LOREHUB_PUBLIC_ORIGIN", "https://app.example")
	t.Setenv("LOREHUB_AUTH_SECRET", strings.Repeat("a", 32))

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LOREHUB_OIDC_CLIENT_SECRET") {
		t.Fatalf("expected a clear missing client secret error, got %v", err)
	}
}

func TestLoadRejectsInteractiveAuthenticationWithoutAudience(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_AUTH_MODE", AuthModeInteractive)
	t.Setenv("LOREHUB_OIDC_ISSUER", "https://keycloak.example/realms/lorehub")
	t.Setenv("LOREHUB_OIDC_CLIENT_ID", "lorehub-web")
	t.Setenv("LOREHUB_OIDC_CLIENT_SECRET", "client-secret")
	t.Setenv("LOREHUB_OIDC_REDIRECT_URL", "https://app.example/auth/callback")
	t.Setenv("LOREHUB_PUBLIC_ORIGIN", "https://app.example")
	t.Setenv("LOREHUB_AUTH_SECRET", strings.Repeat("a", 32))

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "OIDC issuer and audience") {
		t.Fatalf("expected a clear missing audience error, got %v", err)
	}
}

func TestLoadInteractiveAuthenticationUsesSecureProductionCookie(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_ENV", "production")
	t.Setenv("LOREHUB_AUTH_MODE", AuthModeInteractive)
	t.Setenv("LOREHUB_OIDC_ISSUER", "https://keycloak.example/realms/lorehub")
	t.Setenv("LOREHUB_OIDC_AUDIENCE", "lorehub-api")
	t.Setenv("LOREHUB_OIDC_CLIENT_ID", "lorehub-web")
	t.Setenv("LOREHUB_OIDC_CLIENT_SECRET", "client-secret")
	t.Setenv("LOREHUB_OIDC_REDIRECT_URL", "https://app.example/auth/callback")
	t.Setenv("LOREHUB_PUBLIC_ORIGIN", "https://app.example")
	t.Setenv("LOREHUB_AUTH_SECRET", strings.Repeat("a", 32))
	t.Setenv("LOREHUB_SESSION_TTL", "24h")
	t.Setenv("LOREHUB_LOGIN_TRANSACTION_TTL", "10m")

	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.AuthMode != AuthModeInteractive || !settings.SessionCookieSecure ||
		settings.OIDCClientID != "lorehub-web" || settings.OIDCAudience != "lorehub-api" ||
		settings.LoginBindingCookieName != "lorehub_login_binding" {
		t.Fatalf("unexpected interactive production settings: %#v", settings)
	}
}

func TestLoadAdvertisesOnlyCompleteProvisionedProviderAliases(t *testing.T) {
	setRequiredEnvironment(t)
	providerSettings := identityProviderSettings()
	for _, provider := range providerSettings {
		t.Setenv(provider.client, "")
		t.Setenv(provider.secret, "")
	}
	t.Setenv("LOREHUB_IDP_GOOGLE_CLIENT_ID", "google-client")
	t.Setenv("LOREHUB_IDP_GOOGLE_CLIENT_SECRET", "google-secret")
	t.Setenv("LOREHUB_IDP_GITHUB_CLIENT_ID", "github-client")
	t.Setenv("LOREHUB_IDP_FACEBOOK_CLIENT_SECRET", "facebook-secret")
	t.Setenv("LOREHUB_IDP_X_CLIENT_ID", "x-client")
	t.Setenv("LOREHUB_IDP_X_CLIENT_SECRET", "x-secret")

	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"google", "x"}; !reflect.DeepEqual(settings.IdentityProviders, want) {
		t.Fatalf("configured provider aliases = %v, want %v", settings.IdentityProviders, want)
	}
}

func TestLoadAdvertisesAllFourProviderAliasesWhenFullyConfigured(t *testing.T) {
	setRequiredEnvironment(t)
	for _, provider := range identityProviderSettings() {
		t.Setenv(provider.client, provider.id+"-client")
		t.Setenv(provider.secret, provider.id+"-secret")
	}

	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"google", "github", "facebook", "x"}; !reflect.DeepEqual(settings.IdentityProviders, want) {
		t.Fatalf("configured provider aliases = %v, want %v", settings.IdentityProviders, want)
	}
}

func TestLoadRejectsProductionStaticLoreCredentials(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_ENV", "production")
	t.Setenv("LOREHUB_LORE_CREDENTIALS", `{"partition":{"identity":"shared","token":"token",`+
		`"authUrl":"ucs-auth://auth.example.com"}}`)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "only allowed in development or test") {
		t.Fatalf("expected static production credential rejection, got %v", err)
	}
}

func TestLoadRejectsProductionMissingServiceSubject(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_ENV", "production")
	t.Setenv("LOREHUB_PUBLIC_ORIGIN", "https://actions.example")
	t.Setenv("LOREHUB_LORE_PUBLIC_READER_SUBJECT", "public-reader-subject")
	t.Setenv("LOREHUB_LORE_ACTIONS_RUNNER_SUBJECT", "actions-runner-subject")
	t.Setenv("LOREHUB_LORE_REPOSITORY_REGISTRATION_SUBJECT", "")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LOREHUB_LORE_REPOSITORY_REGISTRATION_SUBJECT") {
		t.Fatalf("expected missing production service subject rejection, got %v", err)
	}
}

func TestLoadRejectsProductionInvalidServiceSubject(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_ENV", "production")
	t.Setenv("LOREHUB_LORE_PUBLIC_READER_SUBJECT", "public reader")
	t.Setenv("LOREHUB_LORE_ACTIONS_RUNNER_SUBJECT", "actions-runner-subject")
	t.Setenv("LOREHUB_LORE_REPOSITORY_REGISTRATION_SUBJECT", "registration-subject")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LOREHUB_LORE_PUBLIC_READER_SUBJECT") {
		t.Fatalf("expected invalid production service subject rejection, got %v", err)
	}
}

func TestLoadRejectsInvalidLoginBindingCookieName(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_LOGIN_BINDING_COOKIE_NAME", "invalid;name")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LOREHUB_LOGIN_BINDING_COOKIE_NAME") {
		t.Fatalf("expected invalid binding cookie name error, got %v", err)
	}
}

func TestLoadRejectsProductionIdentityFallback(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_ENV", "production")
	t.Setenv("LOREHUB_LORE_IDENTITY", "legacy-shared-identity")
	t.Setenv("LOREHUB_LORE_CREDENTIALS",
		`{"lore-partition":{"identity":"service-identity","token":"short-lived-token",`+
			`"authUrl":"https://auth.example/login"}}`)

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "development-only Lore identity fallback") {
		t.Fatalf("expected production identity fallback error, got %v", err)
	}
}

func TestLoadRejectsDevelopmentLoreIdentityOutsideDevelopment(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_ENV", "production")
	t.Setenv("LOREHUB_DEV_ALLOW_LORE_IDENTITY_FALLBACK", "true")
	t.Setenv("LOREHUB_DEV_LORE_IDENTITY", "development-only")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "development-only Lore identity fallback") {
		t.Fatalf("expected development-only Lore identity to fail closed, got %v", err)
	}
}

func TestLoadRejectsDevelopmentActionsContextOutsideDevelopment(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_ENV", "production")
	t.Setenv("LOREHUB_DEV_ALLOW_ACTIONS_CONTEXT_FALLBACK", "true")
	t.Setenv("LOREHUB_PUBLIC_ORIGIN", "https://actions.example")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "development-only Actions context fallback") {
		t.Fatalf("expected development-only Actions context to fail closed, got %v", err)
	}
}

func TestLoadRejectsDevelopmentActionsJobTokenOutsideDevelopment(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_ENV", "production")
	t.Setenv("LOREHUB_DEV_ALLOW_ACTIONS_JOB_TOKEN_FALLBACK", "true")
	t.Setenv("LOREHUB_PUBLIC_ORIGIN", "https://actions.example")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "development-only Actions job token fallback") {
		t.Fatalf("expected development-only Actions job token to fail closed, got %v", err)
	}
}

func TestLoadRequiresDevelopmentActionsJobTokenWhenEnabled(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_DEV_ALLOW_ACTIONS_JOB_TOKEN_FALLBACK", "true")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LOREHUB_DEV_ACTIONS_JOB_TOKEN") {
		t.Fatalf("expected an explicit development Actions job token, got %v", err)
	}
}

func TestLoadParsesRunnerMappingAndPublicActionsURLs(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_PUBLIC_ORIGIN", "http://localhost:3000")
	t.Setenv("LOREHUB_PUBLIC_API_URL", "http://localhost:3000/api/v1")
	t.Setenv("LOREHUB_PUBLIC_GRAPHQL_URL", "http://localhost:3000/api/graphql")
	t.Setenv("LOREHUB_RUNNER_PLATFORM_IMAGES", `{"self-hosted":"ghcr.io/example/runner:1.2.3"}`)

	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.PublicAPIURL != "http://localhost:3000/api/v1" ||
		settings.PublicGraphQLURL != "http://localhost:3000/api/graphql" ||
		settings.RunnerPlatformImages["self-hosted"] != "ghcr.io/example/runner:1.2.3" {
		t.Fatalf("public Actions configuration was not preserved: %#v", settings)
	}
}

func TestLoadRejectsHTTPActionsOriginInProduction(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_ENV", "production")
	t.Setenv("LOREHUB_PUBLIC_ORIGIN", "http://localhost:3000")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LOREHUB_PUBLIC_ORIGIN must use HTTPS") {
		t.Fatalf("expected production Actions origin to require HTTPS, got %v", err)
	}
}

func TestLoadRequiresExplicitDevelopmentLoreIdentity(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_DEV_ALLOW_LORE_IDENTITY_FALLBACK", "true")
	t.Setenv("LOREHUB_DEV_LORE_IDENTITY", "")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LOREHUB_DEV_LORE_IDENTITY") {
		t.Fatalf("expected an explicit development Lore identity, got %v", err)
	}
}

func TestLoadRejectsUnboundedRunnerTimeout(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_RUNNER_JOB_TIMEOUT", "25h")
	t.Setenv("LOREHUB_RUNNER_LEASE_DURATION", "2h")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "job timeout") {
		t.Fatalf("expected an upper bound for runner jobs, got %v", err)
	}
}

func TestLoadRejectsProductionStaticLoreCredentialsWithHTTPSAuthURL(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_ENV", "production")
	t.Setenv("LOREHUB_LORE_CREDENTIALS", `{"partition":{"identity":"shared","token":"token",`+
		`"authUrl":"https://auth.example/login"}}`)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "only allowed in development or test") {
		t.Fatalf("expected static production credential rejection, got %v", err)
	}
}

func TestLoadRequiresProductionLoreServiceSubjects(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_ENV", "production")
	t.Setenv("LOREHUB_PUBLIC_ORIGIN", "https://actions.example")
	t.Setenv("LOREHUB_PUBLIC_API_URL", "https://actions.example/api/v1")
	t.Setenv("LOREHUB_PUBLIC_GRAPHQL_URL", "https://actions.example/api/graphql")
	t.Setenv("LOREHUB_LORE_PUBLIC_READER_SUBJECT", "")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LOREHUB_LORE_PUBLIC_READER_SUBJECT") {
		t.Fatalf("expected missing production service subject rejection, got %v", err)
	}
}

func setRequiredEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://localhost/lorehub")
	t.Setenv("LOREHUB_ENV", "development")
	t.Setenv("LOREHUB_RUNNER_SUBJECT", "")
	t.Setenv("LOREHUB_DEV_ALLOW_LORE_IDENTITY_FALLBACK", "")
	t.Setenv("LOREHUB_DEV_LORE_IDENTITY", "")
	t.Setenv("LOREHUB_AUTH_MODE", "")
	t.Setenv("LOREHUB_OIDC_ISSUER", "")
	t.Setenv("LOREHUB_OIDC_AUDIENCE", "")
	t.Setenv("LOREHUB_OIDC_CLIENT_ID", "")
	t.Setenv("LOREHUB_OIDC_CLIENT_SECRET", "")
	t.Setenv("LOREHUB_OIDC_REDIRECT_URL", "")
	t.Setenv("LOREHUB_PUBLIC_ORIGIN", "")
	t.Setenv("LOREHUB_AUTH_SECRET", "")
	t.Setenv("LOREHUB_SESSION_TTL", "")
	t.Setenv("LOREHUB_LOGIN_TRANSACTION_TTL", "")
	t.Setenv("LOREHUB_LORE_IDENTITY", "")
	t.Setenv("LOREHUB_LORE_CREDENTIALS", "")
	t.Setenv("LOREHUB_LORE_AUTHORITY", "")
	t.Setenv("LOREHUB_LORE_PUBLIC_READER_SUBJECT", "")
	t.Setenv("LOREHUB_LORE_ACTIONS_RUNNER_SUBJECT", "")
	t.Setenv("LOREHUB_LORE_REPOSITORY_REGISTRATION_SUBJECT", "")
	t.Setenv("LOREHUB_LORE_ALLOW_DEVELOPMENT_FALLBACK", "")
	t.Setenv("LOREHUB_LORE_PUBLIC_READER_SUBJECT", "public-reader-subject")
	t.Setenv("LOREHUB_LORE_ACTIONS_RUNNER_SUBJECT", "actions-runner-subject")
	t.Setenv("LOREHUB_LORE_REPOSITORY_REGISTRATION_SUBJECT", "registration-subject")
	t.Setenv("LOREHUB_OIDC_ISSUER", "")
	t.Setenv("LOREHUB_OIDC_AUDIENCE", "")
	t.Setenv("LOREHUB_OIDC_CLIENT_ID", "")
	t.Setenv("LOREHUB_OIDC_CLIENT_SECRET", "")
	t.Setenv("LOREHUB_OIDC_REDIRECT_URL", "")
	t.Setenv("LOREHUB_PUBLIC_ORIGIN", "")
	t.Setenv("LOREHUB_PUBLIC_API_URL", "")
	t.Setenv("LOREHUB_PUBLIC_GRAPHQL_URL", "")
	t.Setenv("LOREHUB_RUNNER_PLATFORM_IMAGES", "")
	t.Setenv("LOREHUB_DEV_ALLOW_ACTIONS_CONTEXT_FALLBACK", "")
	t.Setenv("LOREHUB_DEV_ACTIONS_CONTEXT_JSON", "")
	t.Setenv("LOREHUB_DEV_ALLOW_ACTIONS_JOB_TOKEN_FALLBACK", "")
	t.Setenv("LOREHUB_DEV_ACTIONS_JOB_TOKEN", "")
	t.Setenv("LOREHUB_AUTH_SECRET", "")
	t.Setenv("LOREHUB_SESSION_TTL", "")
	t.Setenv("LOREHUB_LOGIN_TRANSACTION_TTL", "")
	t.Setenv("LOREHUB_SESSION_COOKIE_SECURE", "")
	t.Setenv("LOREHUB_SESSION_COOKIE_NAME", "")
	t.Setenv("LOREHUB_LOGIN_BINDING_COOKIE_NAME", "")
	t.Setenv("LOREHUB_SESSION_COOKIE_PATH", "")
	t.Setenv("LOREHUB_SESSION_COOKIE_DOMAIN", "")
	for _, provider := range identityProviderSettings() {
		t.Setenv(provider.client, "")
		t.Setenv(provider.secret, "")
	}
}
