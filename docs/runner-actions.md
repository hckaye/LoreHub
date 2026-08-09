# Actions runner operations

LoreHub Actions keeps `act` as the workflow engine. The runner clones the exact Lore revision, discovers the workflow
files from that revision, validates `actions/checkout` for the Lore workspace adapter,
and invokes `act` with one workflow file and the stored event JSON. Act's local checkout path copies the already-cloned
Lore workspace into the remote job;
the runner deletes the temporary workspace after every job.

## Trust boundary

The default Compose profile does not start a runner and never mounts `/var/run/docker.sock`. The `runner` profile starts
an isolated Docker-in-Docker engine using the exact `docker:29.4.0-dind` image. The engine boundary is the only
privileged part of this setup; the runner itself is not privileged. Job containers receive fixed CPU, memory, PID,
capability, and `no-new-privileges` limits. Jobs do not receive host mounts, host networking, or the API/web Docker
daemon socket. The runner passes `--privileged=false` and `act --rm` so job containers are non-privileged and removed
after the workflow.

Compose uses three separate runner networks. `runner-data` contains the runner, PostgreSQL, Lore, and the trusted API;
`runner-control` contains only the runner and the Docker engine; and `runner-egress` contains only the engine. Web stays
on the application network. API and web do not join `runner-control`, and the engine does not join `runner-data`. Act
job containers use the engine's disposable bridge network, so they cannot route to PostgreSQL or Lore on `runner-data`.

The engine listens on `2376` with Docker mTLS. The runner receives only the read-only client certificate volume and
passes `DOCKER_HOST=tcp://runner-engine:2376`, `DOCKER_TLS_VERIFY=1`, and `DOCKER_CERT_PATH=/etc/lorehub/docker-client`
to act. Job containers receive none of these variables or certificates. Non-empty `jobs.<job>.container.options` and
`jobs.<job>.services.<name>.options` are unsupported and disable the workflow; other container/service fields that could
introduce host mounts, devices, capabilities, host namespaces, or credentials are rejected as well.

The rootful engine boundary is not a claim that arbitrary job code is harmless. The rootless 29.4.0 variant was tested
but its nested bridge could reach a host-connected service network in this environment, so it is not used here. Each
production trust domain must use dedicated, disposable runner infrastructure separate from the LoreHub API and web
services. Operators must apply the same network, resource, image, and host policy to that infrastructure and verify the
engine image before promotion. The Compose engine has a separate egress network for pulling approved workflow images;
it is not shared with the API or job data network. This is a Docker/OCI boundary, not a claim of a stronger sandbox.

Lore reads currently use the shared `LOREHUB_LORE_IDENTITY` setting. This is not yet a least-privilege per-job Lore
credential. The control-plane authentication unit will integrate the credential provider later; until then operators
must treat the configured identity as the trust-domain identity.

## Resource and output limits

The runner has a bounded job timeout and a short renewable lease. It polls cancellation while the workflow runs and
stops the `act` process when cancellation is requested or the lease is lost. A job cannot publish completion after its
lease is lost. Workspaces and uncommitted artifact directories are removed during cleanup.

Logs are capped by `LOREHUB_RUNNER_LOG_MAX_BYTES` (10 MiB by default). Artifacts are limited to 100 files, 100 MiB per
file, and 500 MiB per job by default. Artifact persistence rejects symlinks, special files, and paths outside the staged
artifact directory. Files are published only after the complete staged tree is renamed into place. A persistence failure
does not produce successful artifact records.

## API visibility policy

For public repositories, workflow and run metadata and bounded job logs are readable without a session. Private and
internal repositories require repository or organization read membership. Artifact downloads require the same repository
read check. Dispatch, cancellation, and rerun require repository write permission and a browser CSRF token when the
request uses a LoreHub session cookie; bearer authentication remains supported.

An initial branch observation records the branch revision and synchronizes workflow records without creating a push run.
Later revisions create one run per matching supported workflow. A rerun is a new run number with `runAttempt`
incremented and `rerunOf` pointing to the original run; it does not reuse the original job record.

## Compose smoke test

Use a unique Compose project name so unrelated Docker projects are untouched:

```sh
docker compose -p lorehub-actions-smoke -f infra/compose.yaml --profile runner config
docker compose -p lorehub-actions-smoke -f infra/compose.yaml --profile runner up -d --build --wait
docker compose -p lorehub-actions-smoke -f infra/compose.yaml --profile runner down --volumes
```

Before calling the smoke test successful, inspect the rendered configuration and confirm that no service has a
`/var/run/docker.sock` bind mount and that `DOCKER_HOST` uses `2376` with TLS. Run a Lore workflow through `act` that
reads a repository file outside `.github`, asserts its exact contents, checks the resolved PostgreSQL and Lore IPs are
unreachable, confirms the Docker client certificate directory is absent, and confirms an unauthenticated engine
connection cannot be made. Verify the job log contains `lore-checkout-ok`, `network-isolation-ok`,
`docker-client-certs-absent`,
and `docker-api-denied`. Also verify the engine has no remaining act containers. The cleanup command above removes only
the named smoke project and its volumes.
