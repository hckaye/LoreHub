# Backup and recovery

[English](backup-and-recovery.md) | [日本語](backup-and-recovery.ja.md)

The backup command stops services that can change persistent data, creates logical PostgreSQL dumps, archives the
required Docker volumes, and starts the services that were running before the backup. PostgreSQL stays available while
the dumps are created.

## Create a backup

Run the command from the repository that is used for the deployment:

```bash
scripts/backup.sh --output /srv/backups/lorehub-2026-08-14
```

The target directory must not exist. Use `--env-file` and `--project-name` when the deployment does not use `.env` and
the `lorehub` Compose project name.

The backup contains:

- Logical dumps of the LoreHub PostgreSQL database, and of the Keycloak database when the optional Keycloak profile
  is running
- Lore repository storage
- API state, signing keys, TLS keys, and CA state
- Runner repository caches, logs, and artifacts
- Keycloak auxiliary data when the optional Keycloak profile is running
- A copy of the environment file, including its secrets
- The creation time, Compose project name, and source commit in `manifest.txt`

The backup does not contain Mailpit messages, Docker build layers, runner container layers, or the PostgreSQL data
volumes. The logical dumps replace raw PostgreSQL volume copies.

The directory contains credentials and private keys. Store it with access limited to the service operators. Copy it to
storage outside the Docker host. A backup left only on that host is lost with the host.

## Restore a backup

Use the same LoreHub source version recorded as `source_commit` in `manifest.txt`. Restore to an isolated host first
when testing a new version or a backup from another deployment.

The restore command replaces data in the named Compose project. The value passed to `--confirm-project` must exactly
match the target project name:

```bash
scripts/restore.sh \
  --backup /srv/backups/lorehub-2026-08-14 \
  --project-name lorehub \
  --confirm-project lorehub
```

By default, the command uses the current `.env`. On a new host, restore the saved environment file explicitly:

```bash
scripts/restore.sh \
  --backup /srv/backups/lorehub-2026-08-14 \
  --project-name lorehub \
  --confirm-project lorehub \
  --restore-environment
```

`--restore-environment` replaces the target environment file with the saved copy. Use `--env-file PATH` when the
deployment keeps it elsewhere.

The command stops the Compose project, replaces the archived volumes, recreates both application databases, and loads
the logical dumps. It then leaves only the two database services running.

## Validate before starting services

Check the restored data before making the API available:

1. Query the LoreHub database and confirm that organizations, repositories, users, and repository provisioning states
   have the expected records.
2. Open the restored Lore storage with Lore and confirm that each database value in `repositories.lore_repository_id`
   refers to the expected repository.
3. Inspect recent audit and outbox records for unfinished repository creation, deletion, or migration work.
4. Start the services and check `/health/ready`, sign-in, repository browsing, and one read-only Lore operation.

Start the core services after validation:

```bash
docker compose -f infra/compose.yaml up --detach --wait
```

Add `--profile runner` when the deployment uses the bundled runner. The restore command also accepts `--start` and
`--runner`; these options are intended for automated recovery tests after the validation is performed by the test.

## Test recovery regularly

Restore the latest backup to an isolated Compose project on a separate host. Verify sign-in, repository IDs, recent
issues and pull requests, release metadata, and a Lore read. Record the backup creation time and the time required to
complete the restore. Run this test after changing PostgreSQL, Keycloak, Lore, or the persistent volume layout.
