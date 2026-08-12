package config

import (
	"strings"
	"testing"
	"time"
)

func TestNotificationEmailDefaultsToDisabled(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_NOTIFICATION_EMAIL_ENABLED", "")

	settings, err := LoadFor("serve")
	if err != nil {
		t.Fatal(err)
	}
	if settings.NotificationEmailEnabled {
		t.Fatal("notification email must be disabled until SMTP is configured")
	}
}

func TestNotificationEmailAcceptsLocalMailServer(t *testing.T) {
	setRequiredEnvironment(t)
	setNotificationEmailEnvironment(t)

	settings, err := LoadFor("serve")
	if err != nil {
		t.Fatal(err)
	}
	if !settings.NotificationEmailEnabled || settings.SMTPHost != "mailpit" || settings.SMTPPort != 1025 ||
		settings.SMTPTLSMode != smtpTLSModeNone || settings.NotificationEmailMaxAttempts != 6 ||
		settings.NotificationEmailPollPeriod != time.Second {
		t.Fatalf("unexpected notification email settings: %#v", settings)
	}
}

func TestNotificationEmailRejectsIncompleteSMTP(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_NOTIFICATION_EMAIL_ENABLED", "true")
	t.Setenv("LOREHUB_SMTP_HOST", "")
	t.Setenv("LOREHUB_SMTP_FROM_ADDRESS", "notifications@example.com")

	_, err := LoadFor("serve")
	if err == nil || !strings.Contains(err.Error(), "LOREHUB_SMTP_HOST") {
		t.Fatalf("expected missing SMTP host error, got %v", err)
	}
}

func TestNotificationEmailRejectsPlaintextProductionSMTP(t *testing.T) {
	setProductionEnvironment(t)
	setNotificationEmailEnvironment(t)

	_, err := LoadFor("serve")
	if err == nil || !strings.Contains(err.Error(), "requires SMTP TLS") {
		t.Fatalf("expected production SMTP TLS error, got %v", err)
	}
}

func TestRunnerIgnoresNotificationEmailSettings(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("LOREHUB_NOTIFICATION_EMAIL_ENABLED", "invalid")
	t.Setenv("LOREHUB_SMTP_PORT", "invalid")

	settings, err := LoadFor("runner")
	if err != nil {
		t.Fatal(err)
	}
	if settings.NotificationEmailEnabled {
		t.Fatal("runner must not start a notification email worker")
	}
}

func setNotificationEmailEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("LOREHUB_NOTIFICATION_EMAIL_ENABLED", "true")
	t.Setenv("LOREHUB_NOTIFICATION_EMAIL_POLL_PERIOD", "1s")
	t.Setenv("LOREHUB_NOTIFICATION_EMAIL_SEND_TIMEOUT", "5s")
	t.Setenv("LOREHUB_NOTIFICATION_EMAIL_LEASE_DURATION", "15s")
	t.Setenv("LOREHUB_NOTIFICATION_EMAIL_MAX_ATTEMPTS", "6")
	t.Setenv("LOREHUB_SMTP_HOST", "mailpit")
	t.Setenv("LOREHUB_SMTP_PORT", "1025")
	t.Setenv("LOREHUB_SMTP_USERNAME", "")
	t.Setenv("LOREHUB_SMTP_PASSWORD", "")
	t.Setenv("LOREHUB_SMTP_FROM_ADDRESS", "notifications@example.com")
	t.Setenv("LOREHUB_SMTP_FROM_NAME", "LoreHub")
	t.Setenv("LOREHUB_SMTP_TLS_MODE", "none")
}
