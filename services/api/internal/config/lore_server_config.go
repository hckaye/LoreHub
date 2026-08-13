package config

import (
	"errors"
	"os"
	"regexp"
	"strings"
)

var loreServerTokenKeyIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func loadLoreServerConfig(environment string, command string) (string, string, bool, error) {
	tokenKey := os.Getenv("LOREHUB_LORES_TOKEN_KEY")
	tokenKeyID := os.Getenv("LOREHUB_LORES_TOKEN_KEY_ID")
	if environment != "production" {
		tokenKey = defaultIfEmpty(tokenKey, "local-development-lores-token-key")
		tokenKeyID = defaultIfEmpty(tokenKeyID, "local-lores-v1")
	}
	allowPrivateServers := false
	if command == "serve" {
		var err error
		allowPrivateServers, err = boolSetting("LOREHUB_LORE_ALLOW_PRIVATE_SERVERS", false)
		if err != nil {
			return "", "", false, err
		}
	}
	return tokenKey, tokenKeyID, allowPrivateServers, nil
}

func validateLoreServerConfig(settings Config, command string) error {
	if command != "serve" {
		return nil
	}
	if len(settings.LoresTokenKey) < 32 || strings.ContainsAny(settings.LoresTokenKey, "\r\n") {
		return errors.New("LOREHUB_LORES_TOKEN_KEY must contain at least 32 unbroken characters")
	}
	if !loreServerTokenKeyIDPattern.MatchString(settings.LoresTokenKeyID) {
		return errors.New("LOREHUB_LORES_TOKEN_KEY_ID is invalid")
	}
	return nil
}
