package config

import (
	"os"
	"strings"
)

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
