# Authentication and identity boundaries

[English](identity.md) | [日本語](identity.ja.md)

## Responsibilities

The Go API owns interactive authentication. By default it verifies the email and password accounts stored in the
LoreHub database. An installation can instead, or additionally, delegate sign-in to an external OIDC provider; the
API then acts as an OIDC relying party. In both cases the browser holds only an opaque session cookie issued by the
API, never an access token.

```text
Browser
  └─ Next.js web (same-origin proxy, opaque session cookie)
       └─ Go API
            ├─ Built-in sign-in: users and user_passwords tables in PostgreSQL
            └─ External OIDC (optional): Authorization Code + PKCE against LOREHUB_OIDC_ISSUER
                 └─ Keycloak, ZITADEL, Okta, Entra ID, or another broker
                      └─ absorbs SAML, LDAP, and social identity providers
```

LoreHub itself speaks exactly one federation protocol: OIDC. It does not implement SAML, LDAP, or per-vendor social
login. A company whose identity provider only offers SAML or LDAP runs a broker such as Keycloak or ZITADEL and
points `LOREHUB_OIDC_ISSUER` at the broker; LoreHub does not know or care what sits behind it.

## Identity model

A person is a row in `users`. Sign-in methods link to it through `user_identities`, keyed by `(issuer, subject)`:

- Built-in password accounts use the fixed issuer `lorehub` and the user's ID as the subject.
- External OIDC accounts use the provider's issuer URL and its `sub` claim. LoreHub provisions the user on first
  sign-in.

Email addresses are profile attributes, not identity keys. Two identities are never linked automatically because they
share an email address.

## Built-in password sign-in

- Password hashes use argon2id and live in `user_passwords`, separate from profile data. Sign-in accepts the email
  address or the username.
- Passwords must contain at least 12 characters, uppercase and lowercase letters, a number, and a symbol, and cannot
  contain the username or email address.
- After five consecutive failures the account locks; the wait starts at 30 seconds, doubles per further failure, and
  caps at 15 minutes. A missing account costs the same verification time as a wrong password.
- Self-registration is enabled by default. Set `LOREHUB_AUTH_PASSWORD_REGISTRATION=disabled` to restrict an
  installation to existing accounts.
- Login and registration requests must be same-origin JSON posts; the API rejects cross-site form-shaped requests.
- Changing a password requires the current password and a valid session with its CSRF token, and revokes every other
  session of that user.
- The built-in store does not keep password history and does not verify email addresses. Installations that require
  either delegate sign-in to an external OIDC provider that enforces them.

## External OIDC provider

Set these values to delegate interactive sign-in:

```env
LOREHUB_OIDC_ISSUER=https://auth.example.com
LOREHUB_OIDC_AUDIENCE=lorehub-api
LOREHUB_OIDC_CLIENT_ID=lorehub-web
LOREHUB_OIDC_CLIENT_SECRET=...
LOREHUB_OIDC_REDIRECT_URL=https://lorehub.example.com/auth/callback
```

The API reads OIDC discovery from the issuer and runs Authorization Code Flow with PKCE S256. ID tokens are verified
against the client ID and bearer access tokens against `LOREHUB_OIDC_AUDIENCE`. Sessions stay server-side exactly as
with built-in sign-in.

When the issuer is a broker that fronts several providers, the sign-in page can deep-link a specific one. The query
parameter used for the hint defaults to Keycloak's `kc_idp_hint` and can be changed with
`LOREHUB_OIDC_IDP_HINT_PARAM`.

Configuring an OIDC provider turns built-in password sign-in off unless `LOREHUB_AUTH_PASSWORD=enabled` keeps both
active side by side. The bundled Keycloak Compose profile remains available for social sign-in and as a reference
broker; see the [Keycloak operations guide](../operations/keycloak.md).

## Authentication modes

`LOREHUB_AUTH_MODE=interactive` is the default Compose mode and enables browser sign-in with the built-in store, an
external OIDC provider, or both. Use `LOREHUB_AUTH_MODE=bearer` for API clients that only send bearer tokens; it
requires an OIDC issuer and audience. `LOREHUB_AUTH_MODE=disabled` is limited to local development without sign-in.

These modes apply to the LoreHub application. Lore Server still validates short-lived JWTs issued by the API and
scoped to `urc-{repository_id}`. The [repository authorization guide](../operations/control-plane-authorization.md)
covers Lore UCS authentication, JWKS, key rotation, TLS, and protected branch hooks.
