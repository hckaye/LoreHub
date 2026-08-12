package config

import (
	"errors"
	"fmt"
	"net/mail"
	"os"
	"strings"
	"time"
)

const (
	smtpTLSModeStartTLS = "starttls"
	smtpTLSModeTLS      = "tls"
	smtpTLSModeNone     = "none"
)

func applyNotificationEmailConfig(config *Config, command string) error {
	config.NotificationEmailPollPeriod = 2 * time.Second
	config.NotificationEmailSendTimeout = 10 * time.Second
	config.NotificationEmailLeaseDuration = 30 * time.Second
	config.NotificationEmailMaxAttempts = 8
	config.SMTPPort = 587
	config.SMTPFromName = "LoreHub"
	config.SMTPTLSMode = smtpTLSModeStartTLS
	if command != "serve" {
		return nil
	}
	var err error
	config.NotificationEmailEnabled, err = boolSetting("LOREHUB_NOTIFICATION_EMAIL_ENABLED", false)
	if err != nil {
		return err
	}
	config.NotificationEmailPollPeriod, err = durationSetting(
		"LOREHUB_NOTIFICATION_EMAIL_POLL_PERIOD",
		config.NotificationEmailPollPeriod,
	)
	if err != nil {
		return err
	}
	config.NotificationEmailSendTimeout, err = durationSetting(
		"LOREHUB_NOTIFICATION_EMAIL_SEND_TIMEOUT",
		config.NotificationEmailSendTimeout,
	)
	if err != nil {
		return err
	}
	config.NotificationEmailLeaseDuration, err = durationSetting(
		"LOREHUB_NOTIFICATION_EMAIL_LEASE_DURATION",
		config.NotificationEmailLeaseDuration,
	)
	if err != nil {
		return err
	}
	config.NotificationEmailMaxAttempts, err = intSetting(
		"LOREHUB_NOTIFICATION_EMAIL_MAX_ATTEMPTS",
		config.NotificationEmailMaxAttempts,
	)
	if err != nil {
		return err
	}
	config.SMTPPort, err = intSetting("LOREHUB_SMTP_PORT", config.SMTPPort)
	if err != nil {
		return err
	}
	config.SMTPHost = strings.TrimSpace(os.Getenv("LOREHUB_SMTP_HOST"))
	config.SMTPUsername = os.Getenv("LOREHUB_SMTP_USERNAME")
	config.SMTPPassword = os.Getenv("LOREHUB_SMTP_PASSWORD")
	config.SMTPFromAddress = strings.TrimSpace(os.Getenv("LOREHUB_SMTP_FROM_ADDRESS"))
	config.SMTPFromName = envOrDefault("LOREHUB_SMTP_FROM_NAME", config.SMTPFromName)
	config.SMTPTLSMode = strings.ToLower(strings.TrimSpace(
		envOrDefault("LOREHUB_SMTP_TLS_MODE", config.SMTPTLSMode),
	))
	return validateNotificationEmailConfig(*config)
}

func validateNotificationEmailConfig(config Config) error {
	if !config.NotificationEmailEnabled {
		return nil
	}
	if config.NotificationEmailPollPeriod <= 0 || config.NotificationEmailPollPeriod > time.Minute {
		return errors.New("LOREHUB_NOTIFICATION_EMAIL_POLL_PERIOD must be no longer than one minute")
	}
	if config.NotificationEmailSendTimeout <= 0 || config.NotificationEmailSendTimeout > time.Minute {
		return errors.New("LOREHUB_NOTIFICATION_EMAIL_SEND_TIMEOUT must be no longer than one minute")
	}
	if config.NotificationEmailLeaseDuration < config.NotificationEmailSendTimeout+5*time.Second ||
		config.NotificationEmailLeaseDuration > 5*time.Minute {
		return errors.New("LOREHUB_NOTIFICATION_EMAIL_LEASE_DURATION must cover the send timeout")
	}
	if config.NotificationEmailMaxAttempts < 1 || config.NotificationEmailMaxAttempts > 20 {
		return errors.New("LOREHUB_NOTIFICATION_EMAIL_MAX_ATTEMPTS must be between 1 and 20")
	}
	if config.SMTPHost == "" || strings.ContainsAny(config.SMTPHost, "\r\n\t ") {
		return errors.New("LOREHUB_SMTP_HOST is required when notification email is enabled")
	}
	if config.SMTPPort < 1 || config.SMTPPort > 65535 {
		return errors.New("LOREHUB_SMTP_PORT is invalid")
	}
	fromAddress, err := mail.ParseAddress(config.SMTPFromAddress)
	if err != nil || fromAddress.Address != config.SMTPFromAddress {
		return errors.New("LOREHUB_SMTP_FROM_ADDRESS is invalid")
	}
	if strings.ContainsAny(config.SMTPFromName, "\r\n") {
		return errors.New("LOREHUB_SMTP_FROM_NAME is invalid")
	}
	if (config.SMTPUsername == "") != (config.SMTPPassword == "") {
		return errors.New("LOREHUB_SMTP_USERNAME and LOREHUB_SMTP_PASSWORD must be set together")
	}
	if config.SMTPTLSMode != smtpTLSModeStartTLS && config.SMTPTLSMode != smtpTLSModeTLS &&
		config.SMTPTLSMode != smtpTLSModeNone {
		return fmt.Errorf("LOREHUB_SMTP_TLS_MODE must be %q, %q, or %q",
			smtpTLSModeStartTLS, smtpTLSModeTLS, smtpTLSModeNone)
	}
	if config.SMTPUsername != "" && config.SMTPTLSMode == smtpTLSModeNone {
		return errors.New("SMTP authentication requires TLS")
	}
	if config.Environment == "production" && config.SMTPTLSMode == smtpTLSModeNone {
		return errors.New("production notification email requires SMTP TLS")
	}
	return nil
}
