# ADR 0002: Self-hosted runners and self-hosted Lore Servers

[English](0002-self-hosted-runners-and-lore-servers.md) | [日本語](0002-self-hosted-runners-and-lore-servers.ja.md)

- Status: Accepted
- Date: 2026-08-13

## Context

A LoreHub installation currently assumes one managed Lore Server and one operator-run CI worker fleet.

- `LOREHUB_LORE_PUBLIC_URL` and `LOREHUB_LORE_INTERNAL_URL` must point at a single managed `lores://` endpoint
  under `LOREHUB_LORE_ROOT_DOMAIN`, and configuration validation rejects anything else. Every repository receives
  its `lore_url` from that global value at provisioning time, and the SDK client rewrites every repository URL
  authority back to the single configured origin. The repository import API is the one exception: it stores a
  caller-supplied `loreUrl` after format validation.
- CI runs on the `lorehub runner` command. The worker is a trusted in-process component: it reads the job queue
  from PostgreSQL directly, decrypts execution-context secrets with the Actions secret key, signs job tokens with
  the Lore signing key, and writes logs and artifacts to the same file store as the API. There is no runner
  identity and no registration. The execution unit is the whole workflow: enqueue creates one `ci_jobs` row per
  workflow run, `runs-on` must be a single scalar label, and the worker hands the entire workflow to `act`.
  `ci_jobs.runner_labels` exists in the schema but nothing writes or reads it.
- Machine credentials follow one pattern: personal access tokens store an HMAC-SHA-256 digest (keyed through
  `SecretCodec`), with expiry, last-used, and revocation columns.
- The Lore policy and observation hooks are HTTPS/JSON endpoints behind mTLS. Verification checks the CA and a
  single shared client CN (`lore-policy-hook`); it does not identify an individual Lore Server.
- Repositories are organization-owned. There is no personal repository namespace today.

This forces the LoreHub operator to size a central Lore Server and a central runner fleet for all tenants. The
operator wants the opposite default: tenants bring their own compute and storage, and capacity operated by the
installation is a paid offering.

## Decision

Repository storage and CI execution become bring-your-own by default. Organizations register their own Lore
Servers and their own runners through a GitHub-Actions-style registration flow. Capacity operated by the
installation (the managed Lore Server and the managed runner fleet) is gated behind entitlements that an instance
administrator grants explicitly. Organizations that exist before the upgrade are grandfathered so nothing breaks
in place.

### Instance administrators and entitlements

- Instance administrators are bootstrapped from configuration: `LOREHUB_INSTANCE_ADMIN_USERNAMES` names users who
  may call the admin API. Grants and revocations are recorded in the audit trail.
- New table `entitlements`: `organization_id` and `user_id` as separate nullable foreign-key columns with a CHECK
  that exactly one is set, `feature` (CHECK: `hosted_lore_server`, `hosted_runners`), `granted_by` (nullable),
  `grant_source` (CHECK: `admin`, `migration`), `created_at`, `revoked_at` (CHECK `revoked_at >= created_at`),
  and a partial unique index over active rows per subject and feature.
- The migration that introduces the table backfills both entitlements with `grant_source = 'migration'` for every
  organization that exists at upgrade time. Existing installations keep working unchanged; only organizations
  created after the upgrade start with no entitlements.
- Entitlement checks use an admission snapshot: they run when work is admitted (repository creation, run
  enqueue). Revocation affects future admissions only; queued and in-progress work completes. Immediate
  cancellation on revocation is explicitly out of scope for the first iteration.
- Billing and payment collection are out of scope; the entitlement row is the only fact the platform checks.

### Self-hosted runners

Runner identity and registration:

- New table `ci_runners`: id, separate scope columns (`repository_id`, `organization_id`) with the composite
  foreign-key pattern from `actions_execution_context_entries` so a repository-scoped runner cannot cross an
  organization boundary, name, labels (jsonb, normalized to lowercase, constrained in count, length, and type),
  credential digest, credential expiry and last-used timestamps, `revoked_at`, runner version, and
  `last_seen_at`. Displayed status (`offline`, `idle`, `busy`) is derived from the heartbeat and active leases,
  never self-reported.
- New table `runner_registration_tokens`: scope columns, token digest, `expires_at`, `consumed_at`, `created_by`.
  A settings page (repository or organization) issues a short-lived registration token with prefix `lhrr_`.
  Exchange consumes the row with an atomic `UPDATE ... WHERE consumed_at IS NULL AND expires_at > now()`.
- `lorehub-runner configure --url <lorehub>` reads the registration token from stdin (a `--token` flag exists but
  the daemon warns that it leaks into process lists), exchanges it for a long-lived runner credential with prefix
  `lhr_`, and stores local configuration. Credential digests use a dedicated HMAC key (`LOREHUB_RUNNER_TOKEN_KEY`)
  with a key-id column for rotation, so rotating API token keys does not strand every agent. `lorehub-runner run`
  starts the daemon; an install script provides a systemd unit the way `svc.sh` does for GitHub's runner.

Runner control-plane API (new):

- External runners never touch PostgreSQL, the Actions secret key, or the Lore signing key. Those stay inside the
  API process. The daemon speaks an HTTPS protocol authenticated by the runner credential: claim, heartbeat,
  cancellation poll, execution-context fetch, job-token issuance, log upload, artifact upload, and completion.
- Claim is one transaction that resolves the credential to a runner row, verifies it is active and not revoked,
  verifies the job's repository is inside the runner's scope, verifies the job's labels are a subset of the
  runner's labels, verifies the job's `execution_target` is `self_hosted`, and writes the runner id into the
  lease. Heartbeat, context fetch, token issuance, uploads, and completion all verify that the presented
  credential belongs to the leaseholder runner. Uploads are bound to job, attempt, and lease, with size limits
  and idempotent retry semantics.
- The managed fleet keeps using the in-process worker path; it claims only jobs with `execution_target =
'managed'`.

Routing:

- Enqueue stores the workflow's normalized `runs-on` labels into `ci_jobs.runner_labels` and sets
  `execution_target`: `self_hosted` when the labels include `self-hosted`, `managed` for platform labels such as
  `ubuntu-latest`. A self-hosted runner can never claim a managed job, even if its labels would match, and vice
  versa.
- Because the execution unit is the whole workflow, the first iteration requires every job in a workflow to use
  the same normalized `runs-on`; workflow validation rejects mixed specifications with an actionable error.
  Per-job routing requires a scheduler redesign (job dependencies, outputs, artifacts) and is deferred.
- A run that resolves to managed labels without the `hosted_runners` entitlement is recorded as a completed,
  failed run with a machine-readable `failure_reason` (new column) instead of an enqueue error. Propagating an
  error from enqueue would roll back the workflow catalog and branch observation in the same transaction and make
  the poller retry the same revision forever.

Execution environment:

- The first iteration supports Linux/amd64 container runners, matching the current worker. The install package
  configures the Docker engine mTLS endpoint, the egress proxy, and the runtime isolation expectations documented
  in the runner trust-boundary guidance of `docs/runner-actions.md`; the tenant owns isolation on their own
  hardware.
- The settings UI warns that an organization-scoped runner receives source, secrets, and job tokens for every
  repository in its scope.

### Self-hosted Lore Servers

Registration and identity:

- New table `lore_servers`: id, scope (`instance` or `organization`, as separate columns with CHECKs), name,
  public `lores://` URL, status, credential digest, Lore build version, `last_seen_at`, health metadata. The
  instance server is one `instance`-scoped row created from configuration at startup.
- New table `lore_server_registration_tokens` with the same shape and atomic consume as runner registration.
  Organization settings issue a token with prefix `lhsr_`; on the Lore Server host, `lorehub-lores-agent
configure` exchanges it (stdin-first, like the runner) for a server credential with prefix `lhss_`, records the
  advertised `lores://` URL, and `lorehub-lores-agent run` starts a daemon that reports health, build version,
  and hook module status to LoreHub over the normal HTTP API.
- Registration validates the Lore build and hook module version against a supported range, because the policy
  integration assumes a patched Lore build as documented in `docs/operations/control-plane-authorization.md`.
- User-scoped servers and runners are defined in the schema shape (a nullable `user_id` scope column) but stay
  dormant until a personal repository namespace exists. The first iteration ships `instance` and `organization`
  scopes only.

Per-server trust:

- The shared `lore-policy-hook` client certificate does not distinguish servers, so it is never distributed to
  tenants. LoreHub issues a per-server mTLS client certificate whose identity encodes the `lore_server_id`,
  delivered and renewed through the agent, with revocation on server removal. The policy and observation
  endpoints resolve the certificate identity to a server row and verify that the repository in the request is
  assigned to that server. A compromised tenant server can then only speak for its own repositories.
- Authentication stays centralized: registered servers keep delegating authentication and policy to LoreHub's
  Lore Auth and policy endpoints, which remain under the managed root domain. The root-domain restriction in
  configuration is relaxed only for data-plane repository URLs.

Server selection and data plane:

- Repositories gain a `lore_server_id` foreign key next to the existing `lore_url`. Selection at creation:
  explicit per-repository choice, then the organization default (a setting naming one registered server), then
  the instance server. Using the instance server requires the `hosted_lore_server` entitlement; without it,
  creation fails inside the provisioning transaction with guidance to register a server first.
- The assignment happens in the same transaction as `BeginRepositoryProvisioning`, which already locks the
  organization row. The migration backfills `lore_server_id` for all existing repositories to the instance server
  row.
- The repository import API requires a `loreServerId` and verifies the imported URL's authority matches that
  server's registered URL. Free-form import URLs are removed.
- The SDK client replaces its single rewrite origin with a resolver from `lore_server_id` to public and transport
  authorities, and keys its credential cache by server id and normalized transport authority.

Reachability and health:

| Connection                  | Direction                 | Purpose                                      |
| --------------------------- | ------------------------- | -------------------------------------------- |
| LoreHub API to Lore Server  | outbound from LoreHub     | provisioning, branch reads, merges, deletion |
| Lore clients to Lore Server | from user machines        | clone, sync, push                            |
| Lore Server to LoreHub      | outbound from server      | Lore Auth, JWKS, policy, observation         |
| Agent to LoreHub            | outbound from server host | registration, heartbeat, health              |

- The agent heartbeat proves the host is up, not that the data plane is reachable. Server health combines the
  heartbeat with an active TLS probe from the API host, and provisioning fails fast with a health error when the
  endpoint is unreachable. `last_seen_at` alone is never treated as proof of reachability.
- Because tenants supply the URLs LoreHub connects to, the resolver rejects private and reserved addresses unless
  the operator explicitly allows private networks (`LOREHUB_LORE_ALLOW_PRIVATE_SERVERS`), and applies connection
  timeouts.
- Moving an existing repository between servers is out of scope. A future ADR covers migration tooling.

### Enforcement summary

| Action                            | Without entitlement                      | With entitlement              |
| --------------------------------- | ---------------------------------------- | ----------------------------- |
| Create repo, no server registered | Rejected in provisioning, with guidance  | Provisions on instance server |
| Create repo, owner default set    | Provisions on that server                | Instance server also allowed  |
| Import repository                 | Requires a registered server id          | Instance server also allowed  |
| Run with `runs-on: self-hosted`   | Routes to registered runners             | Same                          |
| Run with `runs-on: ubuntu-latest` | Recorded as failed with `failure_reason` | Routes to managed fleet       |

Grandfathered organizations hold both entitlements via migration grants, so their behavior does not change at
upgrade time.

## Rejected alternatives

### Keep one shared Lore Server and shard it internally

Partition-level sharding keeps all storage cost on the operator and does nothing for tenants with data residency
or scale requirements. It also keeps the single root-domain assumption baked into configuration.

### Free-form server URL per repository

Letting repository settings or the import API accept an arbitrary `lores://` URL without registration gives no
health reporting, no credential trust, no per-server policy identity, and no way to revoke a server. The existing
import behavior is tightened to registered servers for the same reason.

### Reuse the in-process worker as the self-hosted runner

Shipping the current worker binary to tenants would hand them database credentials, the secrets encryption key,
and the token signing key. A control-plane API with a narrow, lease-scoped surface is required for any external
execution, which is why it is part of this ADR rather than an optimization.

### Inbound-only managed connections to tenant networks

Requiring LoreHub to reach into tenant networks through operator-managed tunnels adds infrastructure the agent
model avoids. The agent registers outbound; the server itself must still be reachable at its advertised
`lores://` endpoint by clients and by LoreHub, which matches how Lore clients already connect.

## Operational consequences

- Registered Lore Servers must expose their advertised `lores://` endpoint to Lore clients and to the LoreHub API
  host, and must be able to reach LoreHub's auth, policy, and API endpoints outbound.
- Self-hosted runner hosts follow the trust-boundary guidance in ADR 0001 and `docs/runner-actions.md`: one trust
  domain per tenant, container isolation configured by the install package, and the tenant owns isolation on
  their own hardware.
- The `runner` compose profile and the managed Lore Server stay available for development and for installations
  that grant themselves entitlements.
- New surfaces to document: runner registration, server registration, entitlement administration, the runner
  control-plane API, and the per-server certificate lifecycle.
