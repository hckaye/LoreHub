# Keycloak operations

[English](keycloak.md) | [日本語](keycloak.ja.md)

Keycloak handles email and password sign-in, social identity providers, and account management.

## Services

- Keycloak uses the dedicated `keycloak-postgres` service. Its credentials, schema, and backups are separate from the
  LoreHub application database.
- Keycloak starts with the production `start` command. Local Compose explicitly enables HTTP and fixes the hostname so
  the token issuer remains stable.
- The `lorehub` realm and its clients are imported from `infra/keycloak/realm-lorehub.json` on first startup.
  `infra/keycloak/bootstrap.sh` configures each social provider when its credentials are present.

## Initial setup

Generate persistent local secrets before starting Compose:

```bash
scripts/setup-keycloak-secrets.sh
docker compose -f infra/compose.yaml up --build
```

Local endpoints:

- Administration console: <http://keycloak.localhost:8280/admin/master/console>
  The username is `admin`; the password is `KEYCLOAK_ADMIN_PASSWORD` in `.env`.
- LoreHub realm: <http://keycloak.localhost:8280/realms/lorehub>
- OIDC discovery: <http://keycloak.localhost:8280/realms/lorehub/.well-known/openid-configuration>

`scripts/setup-keycloak-secrets.sh` generates `POSTGRES_PASSWORD`, `KEYCLOAK_ADMIN_PASSWORD`,
`KEYCLOAK_DB_PASSWORD`, `LOREHUB_OIDC_CLIENT_SECRET`, `LOREHUB_AUTH_SECRET`,
`LOREHUB_ACTIONS_SECRET_KEY`, and `LOREHUB_WEBHOOK_SECRET_KEY`. It fills empty values and preserves existing values.
Use `--force` only when rotating them. The generated `.env` has mode `0600`, is excluded from version control, and is
not printed by the script.

## OIDC clients

The realm defines two clients:

| Client        | Type         | Purpose                                                        |
| ------------- | ------------ | -------------------------------------------------------------- |
| `lorehub-web` | confidential | Next.js web application using Authorization Code Flow and PKCE |
| `lorehub-api` | bearer-only  | Go API resource server with audience validation                |

The `lorehub-web` access token includes `lorehub-api` in its audience. The Go API validates it with
`LOREHUB_OIDC_AUDIENCE=lorehub-api`.

### Local accounts and password policy

Self-registration and email-address login are enabled. Passwords require at least 12 characters, uppercase and
lowercase letters, a number, and a symbol. They cannot match the username or email address, and the previous three
passwords cannot be reused. Brute-force protection starts delaying login after five failures and increases the delay up
to 900 seconds.

Keycloak stores password hashes and account email addresses. A new LoreHub installation does not create sample accounts
or initial user passwords.

### Redirect and logout URLs

The realm import contains these local URLs. `keycloak-bootstrap` updates them from environment variables on each start.

- Root URL: `http://localhost:3000`
- Redirect URI: `http://localhost:3000/auth/callback`
- Post-logout redirect URI: `http://localhost:3000/`
- Web origin: `http://localhost:3000`

For production, set `LOREHUB_PUBLIC_ORIGIN`, `LOREHUB_OIDC_REDIRECT_URL`, and
`LOREHUB_OIDC_LOGOUT_REDIRECT_URL` to the same public HTTPS site. Set `KEYCLOAK_HOSTNAME` and
`LOREHUB_OIDC_ISSUER` to the public HTTPS Keycloak URL.

## Go API and the OIDC issuer

The Go API reads OIDC discovery from `LOREHUB_OIDC_ISSUER` and verifies the token issuer, audience, signature, and
expiry. Compose uses `LOREHUB_AUTH_MODE=interactive`, issuer
`http://keycloak.localhost:8280/realms/lorehub`, audience `lorehub-api`, and client ID `lorehub-web`. The API waits for
the Keycloak health check and bootstrap completion before starting.

Use `LOREHUB_AUTH_MODE=bearer` with an issuer and audience for bearer-only API clients. Use
`LOREHUB_AUTH_MODE=disabled` only for local development without authentication. To run the API without Keycloak in
Compose, remove its service dependencies explicitly:

```bash
LOREHUB_AUTH_MODE=disabled docker compose -f infra/compose.yaml run --rm --no-deps api
```

### OIDC discovery from local Docker

Local Compose uses `http://keycloak.localhost:8280/realms/lorehub` as the issuer. Browsers resolve
`keycloak.localhost` to the host loopback address. The API container reaches the same published port through the
`keycloak.localhost:host-gateway` entry. The browser and API therefore read the same issuer, discovery document, and
JWKS URL.

Other deployments must meet these conditions:

1. The issuer is reachable from both browsers and the API.
2. The discovery document issuer exactly matches `LOREHUB_OIDC_ISSUER`.
3. A reverse proxy forwards the external TLS, Host, and `Forwarded` values correctly.

Do not publish an issuer that browsers can resolve only through container DNS.

## Social identity providers

A provider is enabled only when both `LOREHUB_IDP_<PROVIDER>_CLIENT_ID` and
`LOREHUB_IDP_<PROVIDER>_CLIENT_SECRET` are set. Removing either value disables the provider on the next bootstrap run.
Restoring both values updates and enables it again.

Create credentials in each provider's developer console. Use the Redirect URI displayed for that provider in the
Keycloak administration console. It follows this pattern:

```text
http://keycloak.localhost:8280/realms/lorehub/broker/<alias>/callback
```

Replace the local origin with the public HTTPS Keycloak URL in production.

### Google

- Console: Google Cloud Console, APIs & Services, Credentials, OAuth client ID
- Application type: Web application
- Authorized redirect URI:
  `http://keycloak.localhost:8280/realms/lorehub/broker/google/callback`
- Scope: `openid email profile`
- Optional hosted-domain restriction: `LOREHUB_IDP_GOOGLE_HOSTED_DOMAIN`

### GitHub

- Console: GitHub, Settings, Developer settings, OAuth Apps, New OAuth App
- Authorization callback URL:
  `http://keycloak.localhost:8280/realms/lorehub/broker/github/callback`
- Scope: `user:email`

### Facebook

- Console: Meta for Developers, Apps, Facebook Login, Settings
- Valid OAuth Redirect URI:
  `http://keycloak.localhost:8280/realms/lorehub/broker/facebook/callback`
- Scope: `email`

### X

Keycloak 26.7 deprecates its built-in Twitter broker, which uses OAuth 1.0a. LoreHub configures a generic OAuth v2
provider with the official X OAuth 2.0 endpoints.

- Console: X Developer Portal, App, User authentication settings, OAuth 2.0
- App type: Web App, Confidential Client
- Callback URL:
  `http://keycloak.localhost:8280/realms/lorehub/broker/x/callback`
- Scope: `users.read users.email`
- Authorization endpoint: `https://x.com/i/oauth2/authorize`
- Token endpoint: `https://api.x.com/2/oauth2/token`
- UserInfo endpoint: `https://api.x.com/2/users/me?user.fields=confirmed_email,profile_image_url`
- PKCE: S256

The UserInfo response nests values under `data`. The Keycloak claim mappings use `data.id`, `data.username`,
`data.name`, `data.confirmed_email`, and `data.profile_image_url`. Bootstrap maps each provider avatar field to the
OIDC `picture` claim so LoreHub can show it after sign-in.

## Account linking

Social providers use `trustEmail=false`. When the first broker login finds an existing account with the same email
address, the user must confirm the account link through the `firstBrokerLogin` flow. Keep the confirmation step enabled
in production. Adjust duplicate email and linking behavior through Identity Provider Mappers and First Broker Login Flow
in the realm settings.

## Email verification, password reset, and SMTP

Local development defaults to `LOREHUB_VERIFY_EMAIL=false`. Password reset requires SMTP even when email verification
is disabled.

Set all of these values in production:

- `LOREHUB_ENV=production`
- `LOREHUB_VERIFY_EMAIL=true`
- `KEYCLOAK_SMTP_HOST`, `KEYCLOAK_SMTP_PORT`, and `KEYCLOAK_SMTP_FROM`
- `KEYCLOAK_SMTP_AUTH`; when `true`, also set `KEYCLOAK_SMTP_USER` and `KEYCLOAK_SMTP_PASSWORD`
- Exactly one of `KEYCLOAK_SMTP_STARTTLS` or `KEYCLOAK_SMTP_SSL` to `true`
- Optional sender values: `KEYCLOAK_SMTP_FROM_DISPLAY_NAME`, `KEYCLOAK_SMTP_REPLY_TO`, and
  `KEYCLOAK_SMTP_REPLY_TO_DISPLAY_NAME`

`keycloak-bootstrap` rejects a production configuration with email verification disabled or with incomplete SMTP
settings. When verification is enabled, each bootstrap run updates the realm `verifyEmail` flag and SMTP settings. The
SMTP password is supplied through the environment and is not written to bootstrap logs.

## Backup and upgrades

Use the [backup and recovery procedure](backup-and-recovery.md). It creates a separate logical dump of the Keycloak
database and stops Keycloak while the backup is taken.

Create a database backup before a Keycloak upgrade and follow the Keycloak upgrade guide. Update the
`infra/keycloak/Dockerfile` base image and the `keycloak-bootstrap` image in `infra/compose.yaml` to the same release.

Realm import runs only when the realm does not exist. Apply later realm JSON changes with `kcadm`, or import them into a
temporary realm and verify them before applying the equivalent changes.

## Production TLS and reverse proxy

Place Keycloak behind a TLS-terminating reverse proxy such as nginx, Caddy, or Cloudflare.

- Set `KEYCLOAK_HOSTNAME` to the public HTTPS URL, for example `https://auth.lorehub.example`.
- Compose starts Keycloak with `--proxy-headers=forwarded`. Configure the proxy to send a valid standard `Forwarded`
  header. If another header format is used, change both Keycloak and proxy configuration consistently.
- Make the Keycloak application port reachable only from the reverse proxy network.
- Change `LOREHUB_PUBLIC_ORIGIN`, `LOREHUB_OIDC_REDIRECT_URL`, and `LOREHUB_OIDC_LOGOUT_REDIRECT_URL` to HTTPS and set
  `LOREHUB_SESSION_COOKIE_SECURE=true`.
- Realm setting `sslRequired=external` allows local loopback access and requires TLS for external access.
- Change the bootstrap administrator password after first startup and restrict administration console access by
  network policy or VPN.
