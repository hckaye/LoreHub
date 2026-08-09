# Actions runner operations

LoreHub Actions keeps `act` as the workflow engine. The runner clones the exact Lore revision, discovers the workflow
files from that revision, adapts only `actions/checkout`, and invokes `act` with one workflow file and the stored event
JSON. The runner deletes the temporary Lore workspace after every job.

## Trust boundary

The default Compose profile does not start a runner and never mounts `/var/run/docker.sock`. The `runner` profile starts
an isolated Docker-in-Docker engine using the exact `docker:29.4.0-dind-rootless` image. The engine boundary is the
privileged part of this setup; the runner itself is not privileged. Job containers receive fixed CPU, memory, PID,
capability, and `no-new-privileges` limits. Jobs do not receive host mounts,
host networking, or the API/web Docker daemon socket. The runner passes `--privileged=false` and `act --rm` so job
containers are non-privileged and removed after the workflow.

The rootless engine boundary is not a claim that arbitrary job code is harmless. Each production trust domain must use
dedicated, disposable runner infrastructure separate from the LoreHub API and web services. Operators must apply the
same network, resource, image, and host policy to that infrastructure and verify the engine image before promotion. The
Compose engine has a separate egress network for pulling approved workflow images; it is not shared with the API.

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
`/var/run/docker.sock` bind mount, then run a tiny Lore workflow through `act` and verify that the workspace and `act`
containers are gone. The cleanup command above removes only the named smoke project and its volumes.
