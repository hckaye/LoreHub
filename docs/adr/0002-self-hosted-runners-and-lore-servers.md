# ADR 0002: Self-hosted runners and self-hosted Lore Servers

[English](0002-self-hosted-runners-and-lore-servers.md) | [日本語](0002-self-hosted-runners-and-lore-servers.ja.md)

- Status: Proposed
- Date: 2026-08-13

## Context

A LoreHub installation currently assumes one managed Lore Server and one operator-run CI worker fleet.

- `LOREHUB_LORE_PUBLIC_URL` and `LOREHUB_LORE_INTERNAL_URL` must point at a single managed `lores://` endpoint
  under `LOREHUB_LORE_ROOT_DOMAIN`, and configuration validation rejects anything else. Every repository receives
  its `lore_url` from that global value at provisioning time, and the SDK client rewrites every repository URL
  authority back to the single configured origin.
- CI runs on the `lorehub runner` command. The worker is a trusted process started by the operator. It claims any
  queued job with `FOR UPDATE SKIP LOCKED` and a lease. There is no runner identity, no registration, and no label
  routing: `ci_jobs.runner_labels` is written but never read.

This forces the LoreHub operator to size a central Lore Server and a central runner fleet for all tenants. Both
are expensive. The operator wants the opposite default: tenants bring their own compute and storage, and capacity
operated by the installation is a paid offering.

## Decision

Repository storage and CI execution become bring-your-own by default. Organizations and users register their own
Lore Servers and their own runners through a GitHub-Actions-style registration flow. Capacity operated by the
installation (the managed Lore Server and the managed runner fleet) is gated behind entitlements that an instance
administrator grants explicitly.

### Entitlements

- New table `entitlements`: subject (`organization` or `user` with the matching id), `feature`, `granted_by`,
  `created_at`, `revoked_at`. Features start with `hosted_lore_server` and `hosted_runners`.
- Instance administrators grant and revoke entitlements through an admin API. Billing and payment collection are
  out of scope for this ADR; the entitlement row is the only fact the platform checks.
- Nothing is granted by default. A fresh organization or user can only use self-hosted capacity.

### Self-hosted runners

- New table `ci_runners`: id, scope (`repository`, `organization`, or `user`), owner reference, name, labels
  (jsonb), status (`offline`, `idle`, `busy`), credential digest, `last_seen_at`, runner version. The composite
  foreign keys follow the pattern used by `actions_execution_context_entries` so a repository-scoped runner cannot
  cross an organization boundary.
- Registration mirrors the GitHub Actions runner flow. A settings page (repository, organization, or user) issues
  a short-lived registration token with prefix `lhrr_`. The runner host runs `lorehub-runner configure --url
<lorehub> --token <lhrr_...>`, which exchanges the registration token for a long-lived runner credential with
  prefix `lhr_`, stored as a SHA-256 digest exactly like personal access tokens. `lorehub-runner run` starts the
  daemon; an install script provides a systemd unit the way `svc.sh` does for GitHub's runner.
- The daemon reuses the existing worker execution path (checkout, `act`, Docker over mTLS, log upload, job
  tokens). The claim query gains two filters: the job's repository must be inside the runner's scope, and the
  job's `runner_labels` must be a subset of the runner's labels. Job tokens stay scoped to a single job lease.
- `runs-on` routing: the label `self-hosted`, alone or with custom labels, routes to registered runners. Platform
  labels such as `ubuntu-latest` route to the managed fleet and require the `hosted_runners` entitlement on the
  repository owner. A run that resolves to managed labels without the entitlement fails at enqueue time with an
  explanatory annotation instead of waiting forever.
- The managed fleet keeps running through the existing `lorehub runner` command, which now claims only jobs that
  passed the entitlement check and carry platform labels.

### Self-hosted Lore Servers

- New table `lore_servers`: id, scope (`instance`, `organization`, or `user`), owner reference, name, public
  `lores://` URL, status, credential digest, `last_seen_at`, health metadata. The instance server is represented
  as one `instance`-scoped row created from configuration at startup.
- Registration follows the same shape as runners. Organization or user settings issue a registration token with
  prefix `lhsr_`. On the host that runs the Lore Server, `lorehub-lores-agent configure` exchanges the token for
  a server credential, records the advertised `lores://` URL, and `lorehub-lores-agent run` starts a daemon that
  reports health and version to LoreHub over the normal HTTP API.
- Authentication stays centralized. Registered servers keep delegating authentication and policy to LoreHub's
  existing Lore Auth and policy gRPC endpoints, which the agent configures on the server. LoreHub remains the
  single identity authority regardless of where repository data lives.
- Server selection at repository creation: an explicit per-repository choice wins, then the owner default (an
  organization-level or user-level setting naming one registered server), then the instance server. Using the
  instance server requires the `hosted_lore_server` entitlement; without it, creation fails with guidance to
  register a server first.
- The resolved server writes `repositories.lore_url` exactly as today, so the data plane keeps working from the
  stored URL. The SDK client stops rewriting every URL to one global origin and instead resolves transport
  authority per server row; its credential cache is already keyed by endpoint. Configuration validation keeps the
  root-domain restriction for the instance server only.
- Moving an existing repository between servers is out of scope. A future ADR covers migration tooling.

### Enforcement summary

| Action                                      | Without entitlement          | With entitlement                      |
| ------------------------------------------- | ---------------------------- | ------------------------------------- |
| Create repository, no server registered     | Rejected with guidance       | Provisions on instance server         |
| Create repository, owner default server set | Provisions on that server    | Same, instance server also selectable |
| Run with `runs-on: self-hosted`             | Routes to registered runners | Same                                  |
| Run with `runs-on: ubuntu-latest`           | Fails at enqueue             | Routes to managed fleet               |

## Rejected alternatives

### Keep one shared Lore Server and shard it internally

Partition-level sharding keeps all storage cost on the operator and does nothing for tenants with data residency
or scale requirements. It also keeps the single root-domain assumption baked into configuration.

### Free-form server URL per repository

Letting repository settings accept an arbitrary `lores://` URL without registration gives no health reporting, no
credential trust, and no way to revoke a server. Typos would strand repositories at creation time.

### Inbound-only managed connections to tenant networks

Requiring LoreHub to reach into tenant networks through operator-managed tunnels adds infrastructure the agent
model avoids. The agent registers outbound; the server itself must still be reachable at its advertised
`lores://` endpoint by clients and by LoreHub for provisioning, which matches how Lore clients already connect.

## Operational consequences

- Registered Lore Servers must expose their advertised `lores://` endpoint to both Lore clients and the LoreHub
  API host. Provisioning fails fast with a health error when the endpoint is unreachable.
- Self-hosted runner hosts follow the trust-boundary guidance in ADR 0001: one trust domain per tenant, and the
  tenant owns isolation on their own hardware.
- The `runner` compose profile and the managed Lore Server stay available for development and for installations
  that grant themselves entitlements.
- New surfaces to document: runner registration, server registration, entitlement administration.
