package config

import (
	"strings"
	"testing"
)

func setInteractiveEnvironment(t *testing.T) {
	t.Helper()
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_AUTH_SECRET", strings.Repeat("s", 32))
	t.Setenv("LOREHUB_PUBLIC_ORIGIN", "http://localhost:3000")
}

func TestPasswordAuthenticationIsDefaultForInteractiveWithoutOIDC(t *testing.T) {
	setInteractiveEnvironment(t)
	t.Setenv("LOREHUB_AUTH_MODE", "interactive")

	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.AuthMode != AuthModeInteractive || !settings.PasswordAuthEnabled ||
		!settings.PasswordRegistrationEnabled {
		t.Fatalf("unexpected password defaults: %+v", settings)
	}
}

func TestPasswordAuthenticationImpliesInteractiveMode(t *testing.T) {
	setInteractiveEnvironment(t)
	t.Setenv("LOREHUB_AUTH_PASSWORD", "enabled")

	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.AuthMode != AuthModeInteractive || !settings.PasswordAuthEnabled {
		t.Fatalf("LOREHUB_AUTH_PASSWORD=enabled did not select interactive mode: %+v", settings)
	}
}

func TestPasswordAuthenticationDefaultsOffWhenOIDCIsConfigured(t *testing.T) {
	setInteractiveEnvironment(t)
	t.Setenv("LOREHUB_OIDC_ISSUER", "http://identity.localhost/realms/lorehub")
	t.Setenv("LOREHUB_OIDC_AUDIENCE", "lorehub-api")
	t.Setenv("LOREHUB_OIDC_CLIENT_ID", "lorehub-web")
	t.Setenv("LOREHUB_OIDC_CLIENT_SECRET", "secret")
	t.Setenv("LOREHUB_OIDC_REDIRECT_URL", "http://localhost:3000/auth/callback")

	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.AuthMode != AuthModeInteractive || settings.PasswordAuthEnabled ||
		settings.PasswordRegistrationEnabled {
		t.Fatalf("OIDC configuration did not keep password authentication off: %+v", settings)
	}

	t.Setenv("LOREHUB_AUTH_PASSWORD", "enabled")
	settings, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if !settings.PasswordAuthEnabled {
		t.Fatal("explicit LOREHUB_AUTH_PASSWORD=enabled was ignored alongside OIDC")
	}
}

func TestPasswordRegistrationCanBeDisabledIndependently(t *testing.T) {
	setInteractiveEnvironment(t)
	t.Setenv("LOREHUB_AUTH_MODE", "interactive")
	t.Setenv("LOREHUB_AUTH_PASSWORD_REGISTRATION", "disabled")

	settings, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !settings.PasswordAuthEnabled || settings.PasswordRegistrationEnabled {
		t.Fatalf("registration was not disabled independently: %+v", settings)
	}
}

func TestInteractiveModeRequiresPasswordOrOIDC(t *testing.T) {
	setInteractiveEnvironment(t)
	t.Setenv("LOREHUB_AUTH_MODE", "interactive")
	t.Setenv("LOREHUB_AUTH_PASSWORD", "disabled")

	if _, err := Load(); err == nil ||
		!strings.Contains(err.Error(), "LOREHUB_AUTH_PASSWORD") {
		t.Fatalf("interactive mode without any provider was accepted: %v", err)
	}
}

func TestPasswordAuthenticationRejectsInvalidValues(t *testing.T) {
	setInteractiveEnvironment(t)
	t.Setenv("LOREHUB_AUTH_MODE", "interactive")
	t.Setenv("LOREHUB_AUTH_PASSWORD", "yes")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "LOREHUB_AUTH_PASSWORD") {
		t.Fatalf("invalid LOREHUB_AUTH_PASSWORD value was accepted: %v", err)
	}
}
