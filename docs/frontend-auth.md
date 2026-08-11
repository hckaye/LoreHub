# Frontend authentication contract

[English](frontend-auth.md) | [日本語](frontend-auth.ja.md)

The Go API owns authentication state. OAuth access tokens stay in the server-side session and are not exposed to
browser JavaScript.

## Session

The server-rendered locale layout calls `GET /api/v1/auth/session` and forwards the incoming session cookie to the API.
The response is expected to be JSON with one of these shapes:

```json
{
  "authenticated": true,
  "user": {
    "id": "user-id",
    "username": "name",
    "displayName": "Display name",
    "email": "name@example.test",
    "locale": "en"
  },
  "session": {
    "id": "session-id",
    "createdAt": "2026-08-09T00:00:00Z",
    "expiresAt": "2026-09-08T00:00:00Z",
    "lastSeenAt": "2026-08-09T00:00:00Z"
  },
  "csrfToken": "session-bound-token"
}
```

An anonymous browser must receive a stable successful response with `authenticated: false`. A `401` response is treated
as an expired session, while other failures are shown as an unavailable authentication service. Unknown fields are
discarded by the frontend normalizer, including any accidental token-shaped fields. The CSRF value is bound to the
server-side session and remains valid for that session; it is not an OAuth access token.

## Same-origin routes

The App Router handlers for `/api/*` and `/auth/*` forward requests to `LOREHUB_API_URL` at runtime. Browser code uses
same-origin paths. The upstream URL is supplied at runtime, so the same Web image can run in different environments.
The browser sends `credentials: "include"` for mutations and includes the session CSRF token in `X-CSRF-Token`.

The logout request is `POST /auth/logout`. Issue, pull request, organization, and repository forms use the existing API
mutation endpoints and show the actual unauthorized, forbidden, invalid, conflict, and unavailable states returned by
the server.

## Login and registration

Sign-in starts at `/auth/login?return_to=<relative-path>`. The return path is checked before it is encoded;
absolute URLs, protocol-relative URLs, and backslash-based paths fall back to `/`.

The sign-up link uses `/auth/login?return_to=<relative-path>&prompt=create`. `prompt=create` is the documented Keycloak
OIDC query used to open the provider registration screen. If the deployed identity provider does not enable
registration, the provider error is shown on the sign-up page.

## Local proxy configuration

Set `LOREHUB_API_URL` to the API origin reachable by the Next.js server, for example
`http://lorehub.localhost:8080` in Compose or
`http://127.0.0.1:8080` for host development. This is a server-only variable and must not use the `NEXT_PUBLIC_*`
prefix.
