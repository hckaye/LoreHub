# Authentication and identity boundaries

[English](identity.md) | [日本語](identity.ja.md)

## Responsibilities

Keycloak handles email and password accounts and brokers social identity providers. The LoreHub Go API and Next.js
application use Keycloak as OIDC clients and receive user information through token claims.

```text
Browser
  └─ Next.js web (lorehub-web, confidential, Authorization Code + PKCE)
       └─ Keycloak (lorehub realm)
            ├─ Email and password accounts
            ├─ Google, GitHub, Facebook, and X identity providers
            └─ Dedicated PostgreSQL database for authentication data
  └─ Go API (lorehub-api, bearer-only)
       └─ Validates token issuer, audience, signature, and expiry
```

## Data separation

Keycloak uses a dedicated PostgreSQL service named `keycloak-postgres`. Authentication data and LoreHub application
data have separate databases and backup schedules. Keycloak has no access to the LoreHub application database.

## OIDC clients

- `lorehub-web` is a confidential client that uses Authorization Code Flow with PKCE S256. Password grants are
  disabled. Its access token audience includes `lorehub-api`.
- `lorehub-api` is a bearer-only resource server. It validates access tokens against
  `LOREHUB_OIDC_AUDIENCE=lorehub-api` and does not start login flows.

## Token and session lifetimes

The realm configuration in `infra/keycloak/realm-lorehub.json` sets these limits:

- Access token: 300 seconds
- SSO session: 1,800 seconds idle and 28,800 seconds maximum
- Refresh token revocation: enabled with no reuse
- Offline session idle timeout: 30 days

## Password and brute-force protection

- Passwords must contain at least 12 characters, uppercase and lowercase letters, a number, and a symbol. They cannot
  contain the username or email address, and the previous three passwords cannot be reused.
- Brute-force protection locks an account after five failed attempts and increases the wait time up to 900 seconds.
- Email addresses are used as login names. Self-registration is enabled. Production installations must configure SMTP
  and enable email verification.

## Social identity provider provisioning

Social provider credentials are supplied through environment variables. `infra/keycloak/bootstrap.sh` creates or
updates a provider only when both its client ID and client secret are present. Removing credentials disables the
provider; restoring them enables it again. Provisioning is idempotent. External access tokens are not stored, and
provider email claims are not trusted until verified.

X uses a generic OAuth v2 provider with the official X OAuth 2.0 endpoints. It does not use the Twitter broker that is
deprecated in Keycloak 26.7. See the [Keycloak operations guide](../operations/keycloak.md) for configuration details.

## Authentication modes

`LOREHUB_AUTH_MODE=interactive` is the default Compose mode. It enables browser sign-in and uses
`http://keycloak.localhost:8280/realms/lorehub` as the issuer. Use `LOREHUB_AUTH_MODE=bearer` for API clients that only
send bearer tokens. `LOREHUB_AUTH_MODE=disabled` is limited to local development without sign-in.

These modes apply to the LoreHub application. Lore Server still validates short-lived JWTs issued by the API and
scoped to `urc-{repository_id}`. The [repository authorization guide](../operations/control-plane-authorization.md)
covers Lore UCS authentication, JWKS, key rotation, TLS, and protected branch hooks.
