package config

import (
	"errors"
	"regexp"
	"strings"
)

var runnerTokenKeyIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func validateRunnerTokenConfig(settings Config, command string) error {
	if command != "serve" {
		return nil
	}
	if !runnerTokenKeyIDPattern.MatchString(settings.RunnerTokenKeyID) {
		return errors.New("LOREHUB_RUNNER_TOKEN_KEY_ID is invalid")
	}
	if len(settings.RunnerTokenKey) < 32 || strings.TrimSpace(settings.RunnerTokenKey) != settings.RunnerTokenKey ||
		strings.ContainsAny(settings.RunnerTokenKey, "\r\n") {
		return errors.New("LOREHUB_RUNNER_TOKEN_KEY must contain at least 32 unbroken characters")
	}
	return nil
}
