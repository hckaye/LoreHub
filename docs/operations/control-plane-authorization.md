# Repository authentication and authorization

[English](control-plane-authorization.md) | [日本語](control-plane-authorization.ja.md)

Production deployments use the following permission, token, key, TLS, and recovery boundaries.

## Repository boundaries

LoreHub PostgreSQL stores users, Keycloak associations, organizations, teams, roles, repository policies, audit events,
and provisioning state. Lore Server stores revisions, branches, files, and locks.

`repositories.lore_repository_id` maps a LoreHub repository to a Lore partition. It is a unique lowercase 32-character
hexadecimal value. Authorization resources use the same value with the `urc-` prefix. Changing this value changes the
repository access boundary.

A Lore partition has repository-level permissions rather than path ACLs. Content that needs a separate access
boundary uses a separate Lore repository and partition. Lore QUIC and gRPC require current permission for the requested
partition.

## Permission evaluation

Private repository access requires an active user, active organization membership, a direct or team repository role,
the requested token scope, repository policy, branch policy, and permission for the operation. An active organization
owner can administer repositories without a direct or team role. Organization maintainers manage organization, team,
and membership settings but receive no repository permission automatically.

| LoreHub role | Lore permissions   |
| ------------ | ------------------ |
| read         | read               |
| triage       | read               |
| write        | read, write        |
| maintain     | read, write        |
| admin        | read, write, admin |

`obliterate` is a separate high-risk permission. It requires both an enabled repository policy and an explicit user
grant. A broader organization role cannot widen the scope requested by a token.

Anonymous reads of public repositories use an `anonymous_reader` service principal with read permission for the exact
resource. A suspended user or a user removed from the organization is denied instead of being retried as anonymous.
Unauthorized private repository requests return `404`.

Role, team, collaborator, policy, Link, obliterate, and provisioning changes write their audit and outbox records in the
same PostgreSQL transaction.

## UCS authentication and tokens

LoreHub implements `epic_urc.UrcAuthApi` from Lore 0.8.6. Protocol code is generated from the official v0.8.6 source;
the source and regeneration steps are recorded in `services/api/internal/loreauth/proto/README.md`.

After browser authentication, `GetAuthSession` returns a short-lived base authentication token for an active user. Its
`resources` claim is empty. The base token can be exchanged for a resource token and cannot access repository data.
`expires_at` uses Unix milliseconds as required by Lore.

Resource exchange reads current direct roles, team roles, organization membership, and account state from PostgreSQL.
Only an exact `urc-{32-character-id}` resource can be requested. User tokens do not support resource wildcards. Issued
resource tokens expire after 5 to 10 minutes.

External-token exchange and API-key exchange return the protocol `Unimplemented` result. Token values, session codes,
and private authentication URLs are excluded from logs, errors, audit details, metrics, and traces.

To test the boundary against Lore, supply two partition URLs and read, base, expired, wrong-issuer, wrong-audience, and
wrong-key tokens through environment variables. Set `DATABASE_URL`, then run:

```bash
./scripts/test-lore-auth-boundary.sh
```

The script tests QUIC and gRPC with the stock Lore SDK. It also checks team grants and revocation, external
collaborators, protected branch rejection, and one-time merge authorization in PostgreSQL. Set
`LOREHUB_SMOKE_LORE_ONLY=1` only when testing a separately managed Lore Server without the PostgreSQL policy checks.

## URLs, audience, and signing keys

Issuer, audience, Auth URL, JWKS URL, and public Lore URL use names under the same managed root domain. A production
deployment may use:

```text
issuer:   auth.lorehub.example
audience: lorehub.example
AuthURL:  ucs-auth://auth.lorehub.example:8443
JWKS:     https://lorehub.example/.well-known/jwks.json
Lore:     lores://lorehub.example:41337
```

Local development uses the `lorehub.localhost` root and permits HTTP for authentication, JWKS, and confirmation pages.
The JWT issuer is the hostname `auth.lorehub.localhost`, matching Lore client domain validation. The UCS gRPC endpoint
is `ucs-auth://auth.lorehub.localhost:8443`; Lore 0.8.6 converts it to HTTPS.

Public URLs and token audiences use resolvable public names. `LOREHUB_LORE_INTERNAL_AUTH_URL` supplies the API URL used
by Lore Server. The connection validates the HTTPS CA and SAN. `LOREHUB_LORE_INTERNAL_DOMAIN` supplies the host used by
policy, JWKS, and authentication API calls. Its local default is `lorehub.internal`; production DNS resolves it to the
API.

The CI runner connects through `LOREHUB_LORE_INTERNAL_URL` while retaining the partition from the public Lore URL. On
`runner-data`, `lore.<root-domain>` resolves to Lore Server. The API listens for UCS authentication on the advertised
port through `LOREHUB_LORE_AUTH_COMPAT_ADDRESS`.

Production startup requires HTTPS authentication, JWKS, and confirmation endpoints, plus a signing key, key ID, TLS
configuration, and Lore JWT verification settings. Local HTTP is limited to development profiles.

JWTs use RSA asymmetric signing. JWKS publishes the current and previous public key during rotation. Publish the new
public key first, switch the signing key and key ID, wait for the previous token lifetime of up to 10 minutes, then
remove the old public key. Production signing keys come from a secret manager, KMS, or restricted file.

## TLS and Lore Server

Lore Server requires `[server.auth]`, issuer, audience, and `[server.auth.jwk]`. QUIC and gRPC validate the same issuer,
audience, public key, key ID, expiry, resource, and permissions.

Local Compose `tls-init` creates a CA, Lore and API server certificates, and a hook client certificate. SANs include
`lorehub.localhost`, `auth.lorehub.localhost`, `api.lorehub.localhost`, `lore.lorehub.localhost`, and
`api.lorehub.internal`. Add `infra/.local-tls/lorehub-local-ca.crt` to the host TLS trust store before using the host
Lore CLI. These certificates are for local development.

Lore hooks call `https://<policy-host>:8444/internal/lore/policy` under the managed root domain. The connection uses
mTLS and a timeout from 100 milliseconds to 5 seconds, with a default of 1 second. The observation endpoint uses another
fixed path under the same root. The hook certificate requires the `lore-policy-hook` identity and client-auth usage.
Connection, certificate, SAN, payload, and policy failures deny the Lore operation.

Production requires the hook endpoint, managed root, JWKS, Auth URL, CA, client certificate, and client key. The hook
client certificate is separate from service certificates.

## Protected branches and merge

The Lore image builds the official v0.8.6 source and registers the LoreHub module in the official hook registry. Two
small patches add branch-name metadata to the `BranchCreate` hook context and separate the advertised Auth URL from the
server-side Auth URL. Each Lore upgrade checks `HookContext`, JWT verification, the UCS client, environment
advertisement, and the hook registry before retaining those patches.

The hook context contains repository, user, branch ID, branch name, proposed revision, and client IP metadata. The
current revision is resolved by branch ID from PostgreSQL observation state. Push and delete operations are denied when
that state is absent, older than two minutes, or incomplete. `BranchCreate` checks its name against branch rules. A
successful `BranchPush` updates observation state, and `BranchDelete` removes it. An observer service principal polls
Lore branches to repair state after a missed post-hook event.

Direct pushes to protected branches are denied. After the merge worker creates the exact proposed revision, it requests
a one-time authorization from an internal mTLS endpoint. PostgreSQL stores the user, repository, target branch ID and
name, expected current revision, proposed revision, source revision, expiry, and consumed state. The hook atomically
matches and consumes the user, repository, branch, current revision, and proposed revision tuple. A different, expired,
or previously consumed tuple is denied.

## Repository provisioning and Links

Repository creation generates a 32-character ID and stores the pending repository, policy, counter, audit event, outbox
event, and provisioning state in one transaction. The actor receives a short-lived admin resource token for that exact
ID, which is passed to stock Lore `RepositoryCreate`. Idempotency checks and retries use a separate
`lorehub-provisioner` token scoped to the same ID. Success changes the repository to active. Failure records the reason;
retry and reconciliation retain the pending ID. Public URLs use `lores://`, and internal authority substitution occurs
only while connecting.

Importing an existing Lore repository uses a separate endpoint. It requires the current user's exact admin resource
token and verifies the repository information and Lore repository ID.

A Link declaration requires administrator permission and an enabled policy on both source and target repositories. It
stays `declared` until Lore 0.8.6 reports that the Link is active. A declaration does not grant target repository access
or add a path ACL.

## Service principals, runners, and recovery

Web requests, merges, CI checkout, public reads, branch observation, and provisioning each issue a short-lived token for
the exact service principal, partition, and permissions. Service principals have `is_service_account=true` and appear in
audit records. Anonymous reader, CI runner, observer, and provisioner principals are separate. Legacy
`LOREHUB_LORE_IDENTITY` is accepted only in the `local-insecure` profile with API authentication disabled.

The runner inherits `LOREHUB_ENV`. It can use `LOREHUB_RUNNER_AUTH_MODE=disabled` because it does not authenticate
browser or external API requests. Production runner startup still requires the managed root domain, Lore signing key,
Auth URL, TLS, Actions encryption key, CI service principal, and PostgreSQL. CI checkout fails unless the runner can
issue its scoped service-principal token.

Recovery restores Lore storage, LoreHub PostgreSQL, Keycloak PostgreSQL, signing keys, and TLS keys separately. Before
opening the API, verify each Lore repository ID against `repositories.lore_repository_id`, provisioning state, audit
events, and outbox events. A lost signing key is replaced under a new key ID so old tokens expire quickly. Replacing a
lost CA requires a coordinated update of Lore, API, hook, and user trust stores.
