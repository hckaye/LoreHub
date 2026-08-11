# ADR 0001: LoreHub platform architecture

[English](0001-platform-architecture.md) | [日本語](0001-platform-architecture.ja.md)

- Status: Accepted
- Date: 2026-08-09

## Context

LoreHub provides repository browsing, issues, reviews, access management, audit logs, and CI for Lore repositories.
Lore is still below version 1.0, so its API and storage format may change. Integration uses the public C API and the
official Go SDK. GitHub, GitLab, and Gitea inform the collaboration workflows and interface layout.

## Decision

### Runtime components

- Next.js implements the web application in `src/app`.
- Go implements the API, authorization, PostgreSQL updates, asynchronous work, and CI coordination.
- Lore Server stores revisions, branches, files, and locks.
- PostgreSQL stores users, organizations, permissions, issues, reviews, CI state, and audit events.
- Object storage holds CI logs and artifacts. PostgreSQL stores their location and state.

The first deployment uses one Go binary with `serve`, `migrate`, and `runner` commands. Each command runs as a separate
process. Feature packages keep service boundaries explicit so a high-load or failure-prone component can move to a
separate service later.

### Lore integration

The Go backend accesses Lore through the official [Lore Go SDK](https://github.com/EpicGames/lore-go). SDK-specific
types stay inside `internal/lore`, so web and collaboration features do not depend on SDK types.

Read operations use a shared bare-clone cache and fetch only the required revision trees. CI clones an exact revision
into a temporary workspace before executing it.

Browser sessions and Lore credentials have separate lifetimes and scopes. The API checks the user session, permission,
and CSRF token before resolving credentials for a repository partition and operation. Production credentials are
short-lived tokens based on current PostgreSQL permissions. They include the subject, partition, scope, and Lore Auth
URL. Signing keys come from a secret manager, KMS, or a restricted file. Missing production authentication settings
stop startup or deny the operation. Credential fallbacks are limited to explicit development and test fixtures.

Browser sign-in uses OIDC Authorization Code Flow with PKCE. The API verifies the issuer and audience, stores the
OIDC tokens in a time-limited server-side session, and sends only the session cookie to the browser. Bearer API clients
remain supported.

### Data updates

Operations that update multiple tables run in a PostgreSQL transaction. External notifications are written to an outbox
in the same transaction and retried by a worker. External event IDs or business uniqueness constraints prevent duplicate
delivery.

A pull request records the target branch revision when it is created and when it is last checked. Lore is queried again
before merge. A changed target revision triggers another review and CI evaluation. The pull request is marked merged
only after the Lore branch merge succeeds.

### GitHub Actions-compatible CI

Workflow files are read from `.github/workflows/*.yml` and `.yaml`.
[nektos/act](https://github.com/nektos/act) executes supported jobs. LoreHub performs these steps:

1. Receive a Lore branch push event and enqueue it once.
2. Clone the requested Lore revision into an isolated workspace.
3. Create the GitHub Actions-compatible event payload, environment variables, and short-lived job token.
4. Run `act` in an unprivileged isolated environment and save status, logs, and artifacts.

The latest revision reported by the Lore hook and the revision used to read the workflow catalog are stored separately.
Workflow discovery and push-run registration still occur when the hook event arrives before polling observes the branch.

Actions variables and secrets support organization, repository, and environment scopes. PostgreSQL stores variables and
AES-256-GCM encrypted secrets. A runner decrypts secrets only after checking its active CI service principal and
repository grant. `GITHUB_TOKEN` is a short-lived RS256 JWT scoped to the job, run, attempt, repository, and active
lease. Internal APIs such as SARIF upload repeat those checks.

`act` differs from GitHub-hosted runners. Windows and macOS runners, GitHub-only APIs, and unsupported workflow syntax
are documented as unavailable. Compatibility tests run public GitHub workflow examples to detect changes.

Production runners use dedicated, short-lived infrastructure for each trust domain and do not share a host with the API.
The Compose runner profile connects to `docker:29.4.0-dind-rootless` over mTLS on port 2376. Internal `runner-data`,
`runner-control`, and `runner-egress` networks separate the API, runner, engine, job containers, and outbound proxy. Job
containers use a disposable proxy gateway and have no direct outbound route.

Engine permissions and job CPU, memory, PID, capability, and namespace limits are configured separately. Docker Desktop
cannot enforce every per-job cgroup limit, so production uses dedicated disposable nodes or pods with a verified
isolation layer such as gVisor or Kata Containers. Production Lore reads require a short-lived credential for the
service subject, repository partition, and read scope.

### Interface and localization

The locale is the first URL segment. Components read visible text from the locale dictionary. English and Japanese
dictionaries contain the same keys. GitHub and Gitea inform information layout, while Lore terms such as `revision`,
`branch latest`, and `working tree` remain unchanged.

Components are split by responsibility. Formatting and lint checks enforce a maximum of 120 characters per line and
fewer than 1,000 lines per file.

## Rejected alternatives

### Connect to Lore from Next.js only

This would place native libraries, long-running operations, connection caches, and the CI worker in the web process.

### Store Lore file and revision data in PostgreSQL

This would bypass Lore features for large files, deduplication, and partial retrieval and would create two independently
mutable copies of the same repository data.

## Operational consequences

The Lore Go SDK and matching native library must be released together. Lore upgrades run the adapter integration tests
before deployment. Lore Server storage and the LoreHub PostgreSQL database have separate backup and recovery procedures.
