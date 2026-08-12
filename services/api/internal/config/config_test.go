package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
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

func TestLocalLoreDefaultsFollowTheConfiguredRootDomain(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_ENV", "development")
	t.Setenv("LOREHUB_LORE_ROOT_DOMAIN", "control.local")

	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.LoreAuthIssuer != "auth.control.local" ||
		settings.LoreAuthURL != "ucs-auth://auth.control.local:8443" ||
		settings.LoreAuthJWKSURL != "http://control.local:8080/.well-known/jwks.json" ||
		settings.LorePublicURL != "lores://control.local:41337" ||
		settings.LoreInternalURL != "lores://control.local:41337" {
		t.Fatalf("local Lore defaults did not follow the configured root: %#v", settings)
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

func TestLoadRejectsBearerAuthenticationWithoutCredentialSecret(t *testing.T) {
	setProductionEnvironment(t)
	t.Setenv("LOREHUB_AUTH_MODE", AuthModeBearer)
	t.Setenv("LOREHUB_AUTH_SECRET", "")

	_, err := LoadFor("serve")
	if err == nil || !strings.Contains(err.Error(), "LOREHUB_AUTH_SECRET") {
		t.Fatalf("expected a clear missing credential secret error, got %v", err)
	}
}

func TestLoadInteractiveAuthenticationUsesSecureProductionCookie(t *testing.T) {
	setProductionEnvironment(t)

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

func TestLoadForProductionRunnerDoesNotRequireWebAuthentication(t *testing.T) {
	setProductionEnvironment(t)
	t.Setenv("LOREHUB_AUTH_MODE", AuthModeDisabled)
	t.Setenv("LOREHUB_OIDC_ISSUER", "")
	t.Setenv("LOREHUB_OIDC_AUDIENCE", "")
	t.Setenv("LOREHUB_OIDC_CLIENT_ID", "")
	t.Setenv("LOREHUB_OIDC_CLIENT_SECRET", "")
	t.Setenv("LOREHUB_OIDC_REDIRECT_URL", "")
	t.Setenv("LOREHUB_AUTH_SECRET", "")
	t.Setenv("LOREHUB_WEBHOOK_SECRET_KEY_ID", "")
	t.Setenv("LOREHUB_WEBHOOK_SECRET_KEY", "")
	t.Setenv("LOREHUB_WEBHOOK_ALLOW_PRIVATE_TARGETS", "invalid")
	t.Setenv("LOREHUB_WEBHOOK_REQUEST_TIMEOUT", "invalid")

	settings, err := LoadFor("runner")
	if err != nil {
		t.Fatal(err)
	}
	if settings.AuthMode != AuthModeDisabled {
		t.Fatalf("runner authentication mode = %q, want disabled", settings.AuthMode)
	}
}

func TestLoadRequiresDedicatedWebhookEncryptionKey(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_WEBHOOK_SECRET_KEY", "not-base64")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LOREHUB_WEBHOOK_SECRET_KEY") {
		t.Fatalf("expected an invalid webhook encryption key to fail, got %v", err)
	}
}

func TestLoadRejectsLineWrappedWebhookEncryptionKey(t *testing.T) {
	setRequiredEnvironment(t)
	key := "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	t.Setenv("LOREHUB_WEBHOOK_SECRET_KEY", key[:8]+"\n"+key[8:])

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LOREHUB_WEBHOOK_SECRET_KEY") {
		t.Fatalf("expected a line-wrapped webhook key to fail, got %v", err)
	}
}

func TestLoadRejectsPrivateWebhookTargetsOutsideLocalProfiles(t *testing.T) {
	setProductionEnvironment(t)
	t.Setenv("LOREHUB_WEBHOOK_ALLOW_PRIVATE_TARGETS", "true")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LOREHUB_WEBHOOK_ALLOW_PRIVATE_TARGETS") {
		t.Fatalf("expected private webhook targets to fail in production, got %v", err)
	}
}

func TestLoadRejectsInvalidWebhookDurations(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_WEBHOOK_REQUEST_TIMEOUT", "not-a-duration")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LOREHUB_WEBHOOK_REQUEST_TIMEOUT") {
		t.Fatalf("expected an invalid webhook timeout to fail, got %v", err)
	}
}

func TestLoadForMigrateRequiresOnlyDatabaseConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/lorehub")
	t.Setenv("LOREHUB_DATABASE_TIMEOUT", "17s")
	t.Setenv("LOREHUB_ENV", "production")
	t.Setenv("LOREHUB_ACTIONS_SECRET_KEY_ID", "")
	t.Setenv("LOREHUB_ACTIONS_SECRET_KEY", "")
	t.Setenv("LOREHUB_AUTH_MODE", "invalid")
	t.Setenv("LOREHUB_RUNNER_PLATFORM_IMAGES", "invalid-json")

	settings, err := LoadFor("migrate")
	if err != nil {
		t.Fatal(err)
	}
	if settings.DatabaseURL != "postgres://localhost/lorehub" || settings.DatabaseTimeout != 17*time.Second {
		t.Fatalf("unexpected migration settings: %#v", settings)
	}
}

func TestLoadForMigrateValidatesDatabaseConfiguration(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		url     string
		timeout string
	}{
		{name: "missing URL", timeout: "10s"},
		{name: "invalid timeout", url: "postgres://localhost/lorehub", timeout: "later"},
		{name: "zero timeout", url: "postgres://localhost/lorehub", timeout: "0s"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", testCase.url)
			t.Setenv("LOREHUB_DATABASE_TIMEOUT", testCase.timeout)
			if _, err := LoadFor("migrate"); err == nil {
				t.Fatal("invalid migration database configuration was accepted")
			}
		})
	}
}

func TestLoadRejectsUnmanagedAndInsecureLoreEndpoints(t *testing.T) {
	setProductionEnvironment(t)
	t.Setenv("LOREHUB_LORE_AUTH_JWKS_URL", "https://lorehub.example.evil/.well-known/jwks.json")
	if _, err := Load(); err == nil {
		t.Fatal("an evil suffix must not be accepted as a managed Lore endpoint")
	}

	setProductionEnvironment(t)
	t.Setenv("LOREHUB_LORE_POLICY_ENDPOINT", "http://lorehub.example/internal/lore/policy")
	if _, err := Load(); err == nil {
		t.Fatal("an HTTP production policy endpoint must be rejected")
	}

	setProductionEnvironment(t)
	t.Setenv("LOREHUB_LORE_AUTH_AUDIENCE", "api")
	if _, err := Load(); err == nil {
		t.Fatal("the stock Lore audience shorthand must be rejected")
	}
}

func TestLoadAdvertisesOnlyCompleteProvisionedProviderAliases(t *testing.T) {
	setRequiredEnvironment(t)
	for _, provider := range identityProviderSettings() {
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

func TestLoadRejectsMissingProductionServicePrincipal(t *testing.T) {
	setProductionEnvironment(t)
	t.Setenv("LOREHUB_OBSERVER_SERVICE_PRINCIPAL", "")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(),
		"LOREHUB_OBSERVER_SERVICE_PRINCIPAL") {
		t.Fatalf("expected a missing observer service principal error, got %v", err)
	}
}

func TestLoadRejectsProductionStaticLoreCredentials(t *testing.T) {
	setProductionEnvironment(t)
	t.Setenv("LOREHUB_LORE_CREDENTIALS", `{"partition":{"identity":"shared","token":"token",`+
		`"authUrl":"ucs-auth://auth.example.com"}}`)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "limited to development and test") {
		t.Fatalf("expected static production credential rejection, got %v", err)
	}
}

func TestLoadRejectsProductionMissingServiceSubject(t *testing.T) {
	setProductionEnvironment(t)
	t.Setenv("LOREHUB_LORE_REPOSITORY_REGISTRATION_SUBJECT", "")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LOREHUB_LORE_REPOSITORY_REGISTRATION_SUBJECT") {
		t.Fatalf("expected missing production service subject rejection, got %v", err)
	}
}

func TestLoadRejectsProductionInvalidServiceSubject(t *testing.T) {
	setProductionEnvironment(t)
	t.Setenv("LOREHUB_LORE_PUBLIC_READER_SUBJECT", "public reader")
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

func TestLoadRejectsLegacyLoreIdentityOutsideExplicitProfile(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_LORE_IDENTITY", "shared-local-identity")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "explicit local-insecure profile") {
		t.Fatalf("expected legacy identity rejection, got %v", err)
	}
}

func TestLoadAllowsExplicitLocalInsecureLegacyProfile(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_ENV", "local-insecure")
	t.Setenv("LOREHUB_LORE_IDENTITY", "isolated-local-identity")
	t.Setenv("LOREHUB_ALLOW_LEGACY_LORE_IDENTITY", "true")

	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !settings.AllowLegacyLoreIdentity || settings.LoreIdentity != "isolated-local-identity" {
		t.Fatalf("unexpected local legacy settings: %#v", settings)
	}
}

func TestLoadRejectsDevelopmentLoreIdentityOutsideDevelopment(t *testing.T) {
	setProductionEnvironment(t)
	t.Setenv("LOREHUB_DEV_ALLOW_LORE_IDENTITY_FALLBACK", "true")
	t.Setenv("LOREHUB_DEV_LORE_IDENTITY", "development-only")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "development-only Lore identity fallback") {
		t.Fatalf("expected development-only Lore identity to fail closed, got %v", err)
	}
}

func TestLoadRejectsDevelopmentActionsContextOutsideDevelopment(t *testing.T) {
	setProductionEnvironment(t)
	t.Setenv("LOREHUB_DEV_ALLOW_ACTIONS_CONTEXT_FALLBACK", "true")
	t.Setenv("LOREHUB_PUBLIC_ORIGIN", "https://actions.example")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "development-only Actions context fallback") {
		t.Fatalf("expected development-only Actions context to fail closed, got %v", err)
	}
}

func TestLoadRejectsDevelopmentActionsJobTokenOutsideDevelopment(t *testing.T) {
	setProductionEnvironment(t)
	t.Setenv("LOREHUB_DEV_ALLOW_ACTIONS_JOB_TOKEN_FALLBACK", "true")
	t.Setenv("LOREHUB_PUBLIC_ORIGIN", "https://actions.example")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "development-only Actions job token fallback") {
		t.Fatalf("expected development-only Actions job token to fail closed, got %v", err)
	}
}

func TestLoadRequiresDedicatedActionsEncryptionKey(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_ACTIONS_SECRET_KEY", "not-base64")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "exactly 32 bytes") {
		t.Fatalf("expected an invalid Actions encryption key to fail, got %v", err)
	}
}

func TestLoadRejectsLineWrappedActionsEncryptionKey(t *testing.T) {
	setRequiredEnvironment(t)
	key := "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	t.Setenv("LOREHUB_ACTIONS_SECRET_KEY", key[:8]+"\n"+key[8:])

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "unbroken base64") {
		t.Fatalf("expected a line-wrapped Actions encryption key to fail, got %v", err)
	}
}

func TestLoadRejectsCrossOriginActionsTokenAudience(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_ACTIONS_JOB_TOKEN_AUDIENCE", "http://attacker.example/api/v3")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "public LoreHub origin") {
		t.Fatalf("expected a cross-origin Actions token audience to fail, got %v", err)
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
	setProductionEnvironment(t)
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
	setProductionEnvironment(t)
	t.Setenv("LOREHUB_LORE_CREDENTIALS", `{"partition":{"identity":"shared","token":"token",`+
		`"authUrl":"https://auth.example/login"}}`)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "limited to development and test") {
		t.Fatalf("expected static production credential rejection, got %v", err)
	}
}

func TestLoadRequiresProductionLoreServiceSubjects(t *testing.T) {
	setProductionEnvironment(t)
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
	t.Setenv("LOREHUB_ACTIONS_SECRET_KEY_ID", "test-actions-v1")
	t.Setenv(
		"LOREHUB_ACTIONS_SECRET_KEY",
		"MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
	)
	t.Setenv("LOREHUB_ACTIONS_JOB_TOKEN_AUDIENCE", "")
	t.Setenv("LOREHUB_WEBHOOK_SECRET_KEY_ID", "test-webhooks-v1")
	t.Setenv(
		"LOREHUB_WEBHOOK_SECRET_KEY",
		"MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
	)
	t.Setenv("LOREHUB_WEBHOOK_ALLOW_PRIVATE_TARGETS", "")
	t.Setenv("LOREHUB_WEBHOOK_POLL_PERIOD", "")
	t.Setenv("LOREHUB_WEBHOOK_REQUEST_TIMEOUT", "")
	t.Setenv("LOREHUB_WEBHOOK_LEASE_DURATION", "")
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
	t.Setenv("LOREHUB_LORE_ALLOW_DEVELOPMENT_FALLBACK", "")
	t.Setenv("LOREHUB_PUBLIC_API_URL", "")
	t.Setenv("LOREHUB_PUBLIC_GRAPHQL_URL", "")
	t.Setenv("LOREHUB_RUNNER_PLATFORM_IMAGES", "")
	t.Setenv("LOREHUB_DEV_ALLOW_ACTIONS_CONTEXT_FALLBACK", "")
	t.Setenv("LOREHUB_DEV_ACTIONS_CONTEXT_JSON", "")
	t.Setenv("LOREHUB_DEV_ALLOW_ACTIONS_JOB_TOKEN_FALLBACK", "")
	t.Setenv("LOREHUB_DEV_ACTIONS_JOB_TOKEN", "")
	t.Setenv("LOREHUB_SESSION_COOKIE_SECURE", "")
	t.Setenv("LOREHUB_SESSION_COOKIE_NAME", "")
	t.Setenv("LOREHUB_LOGIN_BINDING_COOKIE_NAME", "")
	t.Setenv("LOREHUB_SESSION_COOKIE_PATH", "")
	t.Setenv("LOREHUB_SESSION_COOKIE_DOMAIN", "")
	t.Setenv("LOREHUB_ALLOW_LEGACY_LORE_IDENTITY", "")
	for _, name := range []string{
		"LOREHUB_LORE_AUTH_ISSUER", "LOREHUB_LORE_AUTH_AUDIENCE", "LOREHUB_LORE_ROOT_DOMAIN",
		"LOREHUB_LORE_AUTH_JWKS_URL", "LOREHUB_LORE_AUTH_URL", "LOREHUB_LORE_AUTH_LOGIN_URL",
		"LOREHUB_LORE_PUBLIC_URL", "LOREHUB_LORE_POLICY_ENDPOINT", "LOREHUB_LORE_OBSERVATION_ENDPOINT",
		"LOREHUB_LORE_PUBLIC_READER_SUBJECT", "LOREHUB_LORE_ACTIONS_RUNNER_SUBJECT",
		"LOREHUB_OBSERVER_SERVICE_PRINCIPAL", "LOREHUB_LORE_REPOSITORY_REGISTRATION_SUBJECT",
		"LOREHUB_LORE_REPOSITORY_LIFECYCLE_SUBJECT",
		"LOREHUB_AUTH_SIGNING_KEY_PATH", "LOREHUB_AUTH_SIGNING_KEY", "LOREHUB_AUTH_SIGNING_KEY_KID",
		"LOREHUB_AUTH_PREVIOUS_KEYS", "LOREHUB_LORE_AUTH_TLS_CERT", "LOREHUB_LORE_AUTH_TLS_KEY",
		"LOREHUB_POLICY_TLS_CERT", "LOREHUB_POLICY_TLS_KEY", "LOREHUB_POLICY_TLS_CLIENT_CA",
	} {
		t.Setenv(name, "")
	}
}

func setProductionEnvironment(t *testing.T) {
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
	t.Setenv("LOREHUB_LORE_ROOT_DOMAIN", "lorehub.example")
	t.Setenv("LOREHUB_LORE_AUTH_ISSUER", "auth.lorehub.example")
	t.Setenv("LOREHUB_LORE_AUTH_AUDIENCE", "lorehub.example")
	t.Setenv("LOREHUB_LORE_AUTHORITY", "auth.lorehub.example:8443")
	t.Setenv("LOREHUB_LORE_AUTH_URL", "ucs-auth://auth.lorehub.example:8443")
	t.Setenv("LOREHUB_LORE_AUTH_LOGIN_URL", "https://lorehub.example/auth/lore/confirm")
	t.Setenv("LOREHUB_LORE_AUTH_JWKS_URL", "https://lorehub.example/.well-known/jwks.json")
	t.Setenv("LOREHUB_LORE_PUBLIC_URL", "lores://lorehub.example:41337")
	t.Setenv("LOREHUB_AUTH_SIGNING_KEY_PATH", "/keys/current.pem")
	t.Setenv("LOREHUB_AUTH_SIGNING_KEY_KID", "current")
	t.Setenv("LOREHUB_LORE_AUTH_TLS_CERT", "/tls/server.crt")
	t.Setenv("LOREHUB_LORE_AUTH_TLS_KEY", "/tls/server.key")
	t.Setenv("LOREHUB_POLICY_TLS_CERT", "/tls/server.crt")
	t.Setenv("LOREHUB_POLICY_TLS_KEY", "/tls/server.key")
	t.Setenv("LOREHUB_POLICY_TLS_CLIENT_CA", "/tls/ca.crt")
	t.Setenv("LOREHUB_LORE_POLICY_ENDPOINT", "https://lorehub.example:8444/internal/lore/policy")
	t.Setenv("LOREHUB_LORE_OBSERVATION_ENDPOINT", "https://lorehub.example:8444/internal/lore/observation")
	t.Setenv("LOREHUB_LORE_PUBLIC_READER_SUBJECT", "00000000-0000-4000-8000-000000000001")
	t.Setenv("LOREHUB_LORE_ACTIONS_RUNNER_SUBJECT", "00000000-0000-4000-8000-000000000002")
	t.Setenv("LOREHUB_OBSERVER_SERVICE_PRINCIPAL", "00000000-0000-4000-8000-000000000003")
	t.Setenv("LOREHUB_LORE_REPOSITORY_REGISTRATION_SUBJECT", "00000000-0000-4000-8000-000000000004")
	t.Setenv("LOREHUB_LORE_REPOSITORY_LIFECYCLE_SUBJECT", "00000000-0000-4000-8000-000000000005")
}
