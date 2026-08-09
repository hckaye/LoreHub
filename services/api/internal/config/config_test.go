package config

import (
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
	t.Setenv("LOREHUB_LORE_ROOT_DOMAIN", "lorehub.example")
	t.Setenv("LOREHUB_LORE_AUTH_ISSUER", "auth.lorehub.example")
	t.Setenv("LOREHUB_LORE_AUTH_AUDIENCE", "lorehub.example")
	t.Setenv("LOREHUB_LORE_AUTHORITY", "auth.lorehub.example:8443")
	t.Setenv("LOREHUB_LORE_AUTH_URL", "ucs-auth://auth.lorehub.example:8443")
	t.Setenv("LOREHUB_LORE_AUTH_LOGIN_URL", "https://lorehub.example/auth/lore/confirm")
	t.Setenv("LOREHUB_LORE_AUTH_JWKS_URL", "https://api.lorehub.example/.well-known/jwks.json")
	t.Setenv("LOREHUB_LORE_PUBLIC_URL", "lores://lore.lorehub.example:41337")
	t.Setenv("LOREHUB_AUTH_SIGNING_KEY_PATH", "/keys/current.pem")
	t.Setenv("LOREHUB_AUTH_SIGNING_KEY_KID", "current")
	t.Setenv("LOREHUB_LORE_AUTH_TLS_CERT", "/tls/server.crt")
	t.Setenv("LOREHUB_LORE_AUTH_TLS_KEY", "/tls/server.key")
	t.Setenv("LOREHUB_POLICY_TLS_CERT", "/tls/server.crt")
	t.Setenv("LOREHUB_POLICY_TLS_KEY", "/tls/server.key")
	t.Setenv("LOREHUB_POLICY_TLS_CLIENT_CA", "/tls/ca.crt")
	t.Setenv("LOREHUB_LORE_POLICY_ENDPOINT", "https://api.lorehub.example:8444/internal/lore/policy")
	t.Setenv("LOREHUB_LORE_OBSERVATION_ENDPOINT", "https://api.lorehub.example:8444/internal/lore/observation")
	t.Setenv("LOREHUB_LORE_PUBLIC_READER_SUBJECT", "00000000-0000-4000-8000-000000000001")
	t.Setenv("LOREHUB_LORE_ACTIONS_RUNNER_SUBJECT", "00000000-0000-4000-8000-000000000002")
	t.Setenv("LOREHUB_OBSERVER_SERVICE_PRINCIPAL", "00000000-0000-4000-8000-000000000003")
	t.Setenv("LOREHUB_LORE_REPOSITORY_REGISTRATION_SUBJECT", "00000000-0000-4000-8000-000000000004")

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

func setRequiredEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://localhost/lorehub")
	t.Setenv("LOREHUB_OIDC_ISSUER", "")
	t.Setenv("LOREHUB_OIDC_AUDIENCE", "")
	t.Setenv("LOREHUB_OIDC_CLIENT_ID", "")
	t.Setenv("LOREHUB_OIDC_CLIENT_SECRET", "")
	t.Setenv("LOREHUB_OIDC_REDIRECT_URL", "")
	t.Setenv("LOREHUB_PUBLIC_ORIGIN", "")
	t.Setenv("LOREHUB_AUTH_SECRET", "")
	t.Setenv("LOREHUB_SESSION_TTL", "")
	t.Setenv("LOREHUB_LOGIN_TRANSACTION_TTL", "")
	t.Setenv("LOREHUB_SESSION_COOKIE_SECURE", "")
	t.Setenv("LOREHUB_SESSION_COOKIE_NAME", "")
	t.Setenv("LOREHUB_LOGIN_BINDING_COOKIE_NAME", "")
	t.Setenv("LOREHUB_SESSION_COOKIE_PATH", "")
	t.Setenv("LOREHUB_SESSION_COOKIE_DOMAIN", "")
	t.Setenv("LOREHUB_LORE_IDENTITY", "")
	t.Setenv("LOREHUB_ALLOW_LEGACY_LORE_IDENTITY", "")
}
