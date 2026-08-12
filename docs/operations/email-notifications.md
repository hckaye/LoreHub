# Notification email

[English](email-notifications.md) | [日本語](email-notifications.ja.md)

Users can enable email delivery under **Settings**, **Notification preferences**. Team and repository event switches
apply to both the notification inbox and email. Disabling email delivery cancels messages that have not started
sending.

## Local delivery

Docker Compose sends notification email to Mailpit. Start the stack and open <http://localhost:8025> to inspect the
captured messages.

```bash
scripts/setup-keycloak-secrets.sh
docker compose -f infra/compose.yaml up --build
```

Mailpit is for local testing. Its web port binds to `127.0.0.1`. Change `MAILPIT_HOST_PORT` if port `8025` is already
in use.

## Production SMTP

Set these values before starting the API:

| Variable                                    | Value                                                 |
| ------------------------------------------- | ----------------------------------------------------- |
| `LOREHUB_NOTIFICATION_EMAIL_ENABLED`        | `true`                                                |
| `LOREHUB_SMTP_HOST`                         | SMTP server hostname                                  |
| `LOREHUB_SMTP_PORT`                         | SMTP server port                                      |
| `LOREHUB_SMTP_FROM_ADDRESS`                 | Sender email address                                  |
| `LOREHUB_SMTP_FROM_NAME`                    | Sender display name                                   |
| `LOREHUB_SMTP_TLS_MODE`                     | `starttls` for explicit TLS or `tls` for implicit TLS |
| `LOREHUB_SMTP_USERNAME`                     | SMTP username when authentication is required         |
| `LOREHUB_SMTP_PASSWORD`                     | SMTP password when authentication is required         |
| `LOREHUB_NOTIFICATION_EMAIL_POLL_PERIOD`    | Time between delivery checks, default `2s`            |
| `LOREHUB_NOTIFICATION_EMAIL_SEND_TIMEOUT`   | Timeout for one SMTP delivery, default `10s`          |
| `LOREHUB_NOTIFICATION_EMAIL_LEASE_DURATION` | Delivery ownership period, default `30s`              |
| `LOREHUB_NOTIFICATION_EMAIL_MAX_ATTEMPTS`   | Maximum automatic attempts, default `8`               |

The API rejects incomplete settings at startup. Production also rejects `LOREHUB_SMTP_TLS_MODE=none`. Set the SMTP
username and password together; omit both when the server does not require authentication.

`LOREHUB_PUBLIC_ORIGIN` supplies the host in message links. It must be the public HTTPS origin in production.

## Retries

LoreHub retries a failed delivery after 30 seconds and doubles the delay up to one hour. Another API instance can
resume a delivery after its ownership period expires. A delivery stops after the configured attempt limit. The API
logs the delivery ID and attempt number without logging the message body or SMTP password.

Account verification and password reset email use Keycloak's SMTP settings. Configure them separately as described in
the [Keycloak operations guide](keycloak.md#email-verification-password-reset-and-smtp).
