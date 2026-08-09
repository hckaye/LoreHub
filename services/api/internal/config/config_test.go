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
	t.Setenv("LOREHUB_LORE_CREDENTIALS",
		`{"lore-partition":{"identity":"service-identity","token":"short-lived-token",`+
			`"authUrl":"https://auth.example/login"}}`)

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

func TestLoadRejectsProductionIdentityFallback(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_ENV", "production")
	t.Setenv("LOREHUB_LORE_IDENTITY", "legacy-shared-identity")
	t.Setenv("LOREHUB_LORE_CREDENTIALS",
		`{"lore-partition":{"identity":"service-identity","token":"short-lived-token",`+
			`"authUrl":"https://auth.example/login"}}`)

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "LOREHUB_LORE_IDENTITY") {
		t.Fatalf("expected production identity fallback error, got %v", err)
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
	t.Setenv("LOREHUB_LORE_IDENTITY", "")
	t.Setenv("LOREHUB_LORE_CREDENTIALS", "")
	t.Setenv("LOREHUB_SESSION_COOKIE_SECURE", "")
	t.Setenv("LOREHUB_SESSION_COOKIE_NAME", "")
	t.Setenv("LOREHUB_LOGIN_BINDING_COOKIE_NAME", "")
	t.Setenv("LOREHUB_SESSION_COOKIE_PATH", "")
	t.Setenv("LOREHUB_SESSION_COOKIE_DOMAIN", "")
}
