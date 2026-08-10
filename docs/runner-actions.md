# Actions runner operations

LoreHub Actions keeps `act` as the workflow engine. The runner clones the exact Lore revision, discovers only
`.github/workflows/*.yml` and `.yaml` at that revision, validates the supported trigger and runtime definitions, and
invokes `act` with exactly one workflow file and the stored event JSON. `actions/checkout` remains in the workflow;
the prepared Lore workspace is copied into the remote job by the runner adapter, without Git.

## Trust boundary

The default Compose profile does not start a runner and never mounts `/var/run/docker.sock`. The `runner` profile uses
the exact `docker:29.4.0-dind-rootless` image. The engine boundary is the only privileged part of this Compose setup;
job containers are created with `--privileged=false`, one CPU, 1 GiB memory, 256 PIDs, dropped capabilities, and
`no-new-privileges`. These are runtime limits, not a claim that arbitrary job code is harmless.

The engine exposes only Docker mTLS on 2376. The runner receives a read-only copy of the client certificate directory
and uses `DOCKER_HOST=tcp://runner-engine:2376`, `DOCKER_TLS_VERIFY=1`, and
`DOCKER_CERT_PATH=/etc/lorehub/docker-client` for its engine client and `act`. Job containers receive none of those
variables or files. A job cannot call the Docker API without a client certificate, and job options cannot add daemon
credentials, host mounts, devices, capabilities, or host namespaces.

Networks are deliberately separate:

- `runner-data` is internal and contains the runner, PostgreSQL, Lore, and API.
- `runner-control` is internal and contains only the runner and engine.
- `runner-egress` is internal and contains only the engine and the forward proxy.
- `runner-action` is internal and connects the runner to the same forward proxy for action downloads.
- `runner-uplink` is the proxy's only uplink network.

API and Web do not join `runner-control`. The engine does not join `runner-data`, and the runner has no uplink network.
The engine pulls images through the proxy. Each job gets a disposable internal network. A small HAProxy gateway sidecar
is attached to that internal network and the disposable engine uplink, and forwards only to the outer Squid IP. The
sidecar is a fixed HAProxy transit container, not a raw TCP forwarder;
the job itself is never attached to the bridge and has no direct outside route. Internal service names are not injected.
Job HTTP and HTTPS therefore pass through Squid.
The disposable gateway uses the fixed `haproxy:3.2.4-alpine` image and does not provide a general raw TCP forwarder.

Squid uses the canonical `ubuntu/squid:7.2-26.04_edge` tag. It permits safe HTTP/HTTPS ports and CONNECT to 443 only.
Its destination ACL rejects loopback, RFC1918, link-local, CGNAT, documentation and test ranges, multicast/reserved
ranges, and IPv6 private/link-local ranges. The destination ACL is applied after Squid resolves hostnames, so a public
hostname that resolves to a private address is rejected. This is a Docker/OCI network boundary, not a claim of a
stronger sandbox.
The only local exception is the runner-action artifact endpoint at 172.28.244.2:34567, allowed only from the
runner networks for act artifact upload/download; private and local user HTTP/HTTPS destinations remain denied.

Docker Desktop can run without cgroup enforcement. In that case Compose CPU, memory, and PID values limit the outer
engine container as a whole; they are not per-job security limits. Production requires each trust domain to use a
dedicated, disposable runner node or pod separate from LoreHub API/Web, and gVisor, Kata Containers, or an equivalent
verified workload isolation layer is a required production condition. This repository's Compose smoke does not claim
to verify that stronger layer.

## Lore credentials

The runner and poller request a short-lived Lore credential with a dedicated service principal, the exact repository
partition, and the `read` scope. The Control Plane resolves the active PostgreSQL grant and signs a resource-scoped JWT.
The Lore SDK client uses that credential for branch observation and exact-revision checkout. Repository URLs and their
partition IDs are validated together before a token is issued or used. Production has no file or shared-identity
fallback. The only static identity adapter is an explicit development/local test fallback and is rejected in
production.

The runner also resolves an `actions:execute` context using the service subject, repository and organization partitions,
and the literal job environment. Repository, organization, and environment variables/secrets are merged with
environment precedence. Variables are passed to act as `--var` entries only; secrets are passed through a temporary
0600 `--secret-file`, masked in logs, and removed on every exit. Variables do not become environment variables. A
resolver error fails the job before act starts. The PostgreSQL resolver verifies the active `ci_runner` grant, reads
organization, repository, and environment entries in one repeatable-read transaction, and decrypts AES-256-GCM secret
values only in runner memory. Management APIs return variable values and secret metadata, never secret plaintext.

The production `JobTokenIssuer` receives the exact job, run, attempt, repository, actor, service subject, REST/GraphQL
scopes, and requested expiry. It issues a `kid`-identified RS256 GITHUB_TOKEN valid for no more than 15 minutes.
Issuance and verification both recheck the active organization/repository, job lease, cancellation state, `ci_runner`
principal,
and repository grant. The token is reserved, passed only through the secret file, and exposed by act as `github.token`
and `secrets.GITHUB_TOKEN`. A GitHub-compatible SARIF endpoint accepts this token, validates the same job boundary, and
stores bounded SARIF 2.1.0 documents and alerts in PostgreSQL. Static token/context adapters are development/test-only.

`LOREHUB_RUNNER_PLATFORM_IMAGES` may add validated operator-owned runner-label mappings. The deployed default is
`ubuntu-latest=ghcr.io/catthehacker/ubuntu:act-24.04`; unmapped literal labels fail closed. Workflow files cannot
provide or override these act platform mappings.

Remote action references other than `actions/checkout` are downloaded from the operator-configured
`LOREHUB_ACTION_SOURCE_URL` through the runner proxy, extracted into a temporary local repository mapping, and removed
with the workspace. The workflow cannot change this source. The public GitHub context remains the configured LoreHub
URL.

`workflow_dispatch.inputs` keeps its description, required flag, default, type, and choice options. The API and UI
expose that definition, and the server validates submitted values before storing the exact resolved strings in the event
payload.
The runtime preserves `github.event.inputs` and the `inputs.*` context for act. The configured LoreHub public origin,
API URL, and GraphQL URL populate the GitHub context and `GITHUB_*` environment values; GitHub.com is never substituted.

The official `actions/checkout@v4` line remains unchanged. Lore supplies the already-cloned workspace without Git. The
adapter does not support `ref`, `repository`, `path`, `filter`, `sparse-checkout`, `ssh-key`, `lfs: true`, or
submodules;
those inputs disable the workflow with an explicit error.

## Workflow catalog and branches

The default branch is the canonical Actions catalog. Its initial observation records the branch and synchronizes
workflow records without inventing a push. Every later default-branch revision synchronizes the catalog and queues one
run per matching supported workflow. Missing workflows become disabled; invalid or unsupported workflows are retained
with an error state and are never treated as successful.

Feature branches never update, remove, or disable the catalog. Their exact revision workflow definitions are stored in a
revision table and can enqueue push runs for that revision only. They cannot become a dispatch target until their
workflow exists in the default-branch catalog.

## Resource and output limits

The runner has a hard 24-hour job timeout bound and a renewable lease. It polls cancellation while `act` runs, sends a
graceful termination signal, and force-stops after the grace period. A lost lease prevents completion publication.
Workspaces, partial artifact trees, disposable job networks, proxy gateways, and `act --rm` containers are cleaned up.

Logs are capped by `LOREHUB_RUNNER_LOG_MAX_BYTES` (10 MiB by default). Artifacts are limited to 100 files, 100 MiB per
file, and 500 MiB per job by default, with hard upper bounds. Persistence rejects symlinks, special files, and paths
outside the staging tree.
The complete staged tree is renamed into place only after all files pass validation. A persistence failure cannot claim
artifact success.

## API visibility policy

Public repositories expose workflow/run metadata and bounded job logs anonymously. Internal repositories require an
active user with active organization membership. Private repositories require an active user with an active direct
repository membership, an active organization team membership plus repository team permission, or an owner exception.
Public repository artifacts are public and may be downloaded anonymously, like public logs. Internal and private logs
and artifacts require active read permission. Dispatch, cancellation, and rerun require repository write permission. A
rerun receives a new run number and stores `runAttempt` plus `rerunOf` so each execution remains independently
addressable.
Browser session mutations require the finalized cookie CSRF check; bearer authentication remains compatible.
Unauthorized private/internal repository access returns 404 so repository existence is not disclosed. A public
repository's bounded artifacts are also intentionally public and may be downloaded anonymously; internal and private
artifacts require active read permission.

## Compose smoke test

Use a unique project name so unrelated Docker projects are untouched. The adversarial smoke uses both `runner` and
`runner-smoke` profiles and starts a temporary canary on a separate network with a host-published port. It must verify
from an actual `act` job that:

- the exact contents of a Lore file outside `.github` are present after `actions/checkout`;
- resolved PostgreSQL and Lore service IPs are unreachable;
- the outer `runner-egress` gateway, `host.docker.internal`, the canary, and existing host-published ports are
  unreachable;
- private/local destinations are rejected through the proxy, public HTTPS succeeds through the proxy, and raw public TCP
  fails;
- `docker info` reports `rootless`, the unauthenticated 2375 endpoint fails, and 2376 requires the client certificate;
- the job cannot see the Docker client certificate directory or variables; and
- no `act` containers or disposable job networks remain after completion.

Clean only the named smoke project and its volumes. Do not run a global Docker cleanup command.
