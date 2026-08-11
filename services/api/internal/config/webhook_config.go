package config

import (
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
	"time"
)

var webhookKeyIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func validateWebhookConfig(settings Config, command string) error {
	if command != "serve" {
		return nil
	}
	if !webhookKeyIDPattern.MatchString(settings.WebhookSecretKeyID) {
		return errors.New("LOREHUB_WEBHOOK_SECRET_KEY_ID is invalid")
	}
	if settings.WebhookSecretKey == "" || strings.TrimSpace(settings.WebhookSecretKey) != settings.WebhookSecretKey ||
		strings.ContainsAny(settings.WebhookSecretKey, "\r\n") {
		return errors.New("LOREHUB_WEBHOOK_SECRET_KEY must be unbroken base64 for exactly 32 bytes")
	}
	key, err := base64.StdEncoding.Strict().DecodeString(settings.WebhookSecretKey)
	if err != nil || len(key) != 32 {
		clear(key)
		return errors.New("LOREHUB_WEBHOOK_SECRET_KEY must be base64 for exactly 32 bytes")
	}
	clear(key)
	if settings.WebhookPollPeriod <= 0 || settings.WebhookPollPeriod > time.Minute {
		return errors.New("LOREHUB_WEBHOOK_POLL_PERIOD must be between one nanosecond and one minute")
	}
	if settings.WebhookRequestTimeout <= 0 || settings.WebhookRequestTimeout > 30*time.Second {
		return errors.New("LOREHUB_WEBHOOK_REQUEST_TIMEOUT must be no longer than 30 seconds")
	}
	if settings.WebhookLeaseDuration < settings.WebhookRequestTimeout ||
		settings.WebhookLeaseDuration > 5*time.Minute {
		return errors.New("LOREHUB_WEBHOOK_LEASE_DURATION must cover the request timeout and be at most five minutes")
	}
	local := settings.Environment == "development" || settings.Environment == "test" ||
		settings.Environment == "local" || settings.Environment == "local-insecure"
	if settings.WebhookAllowPrivateTargets && !local {
		return errors.New("LOREHUB_WEBHOOK_ALLOW_PRIVATE_TARGETS is limited to isolated local profiles")
	}
	return nil
}
