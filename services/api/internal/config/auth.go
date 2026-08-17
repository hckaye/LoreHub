package config

import (
	"fmt"
	"os"
	"strings"
)

func authModeFromEnvironment(
	requested string,
	issuer string,
	audience string,
	clientID string,
	redirectURL string,
	passwordAuth string,
) string {
	if requested != "" {
		return strings.ToLower(strings.TrimSpace(requested))
	}
	if clientID != "" || redirectURL != "" {
		return AuthModeInteractive
	}
	if strings.EqualFold(strings.TrimSpace(passwordAuth), "enabled") {
		return AuthModeInteractive
	}
	if issuer != "" || audience != "" {
		return AuthModeBearer
	}
	return AuthModeDisabled
}

// interactiveOIDCConfigured reports whether an external OIDC provider is part
// of the interactive configuration. A client secret alone does not count:
// setup-secrets.sh pre-generates one so the optional Keycloak profile can be
// enabled later, and it is meaningless without an issuer or client ID.
func interactiveOIDCConfigured(config Config) bool {
	return config.OIDCIssuer != "" || config.OIDCAudience != "" || config.OIDCClientID != "" ||
		config.OIDCRedirectURL != ""
}

func enabledSetting(name string, defaultValue bool) (bool, error) {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch value {
	case "":
		return defaultValue, nil
	case "enabled":
		return true, nil
	case "disabled":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be %q or %q", name, "enabled", "disabled")
	}
}
