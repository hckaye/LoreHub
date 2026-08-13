# ADR 0004: Repository server migration

[English](0004-repository-server-migration.md) | [日本語](0004-repository-server-migration.ja.md)

- Status: Accepted
- Date: 2026-08-13

## Context

ADR 0002 lets each repository use a registered Lore Server. It does not define how an existing repository moves.

- The control plane stores both `repositories.lore_url` and `repositories.lore_server_id`. Lore data lives on the
  assigned server, so changing the pointer before copying data can make a repository appear empty or inconsistent.
- The current Lore SDK provides repository clone, branch listing, branch switching, and branch push operations. It does
  not provide a whole-repository server-to-server mirror or an atomic cross-server transfer.
- An operator needs a deliberate maintenance operation with an audit trail. A failed copy must not change the active
  pointer, and a target created before the failure must remain available for manual cleanup.

## Decision

Migration is an operator-driven, offline operation started by an instance administrator for one repository and one
registered target server.

### Lifecycle

1. The administrator calls `POST /api/v1/admin/repositories/{owner}/{repo}/migrate` with `targetServerId`.
2. A serializable transaction verifies the repository and target. The target must be active and visible to the
   repository's organization. The transaction creates a `pending` audit row and sets `repositories.migrating_at`.
3. While that flag is set, repository write authorization is reduced to read-only. Reads remain available. The Lore
   policy hook accepts the target server only for this migration window.
4. A bounded background task marks the row `mirroring` and copies the Lore data. The first implementation has a
   ten-minute task deadline.
5. After the copy succeeds, the task marks the row `repointing`. One transaction then updates both Lore pointer columns,
   clears `migrating_at`, and marks the row `completed`.
6. Any failure marks the row `failed`, records the error, keeps the original pointer, and clears `migrating_at` in one
   transaction. If the target partition was created, it is orphaned and must be removed by an operator.

### Bounded mirror operation

The implementation creates a fresh target partition with the same Lore repository ID. It clones the source into a
temporary workspace, changes the workspace remote to the target server, and pushes every active, non-empty source
branch at its recorded tip.

This is not a complete Lore server mirror. The current SDK has no atomic mirror push. Reachable history from the copied
branch tips is preserved, while archived branches, branch metadata, and other server-local metadata are not promised to
be copied. The migration is therefore offline and must be verified before the pointer is changed.

### API and audit states

The endpoint returns `202 Accepted` with the new migration row. The instance-admin middleware protects both endpoints.

| Endpoint                            | Result                                     |
| ----------------------------------- | ------------------------------------------ |
| `POST .../{owner}/{repo}/migrate`   | Starts one migration with `targetServerId` |
| `GET .../{owner}/{repo}/migrations` | Lists the latest audit rows                |

| State        | Meaning                                               |
| ------------ | ----------------------------------------------------- |
| `pending`    | Read-only is set and the task has not started copying |
| `mirroring`  | The target partition is being created or populated    |
| `repointing` | Copy succeeded and the pointer transaction is next    |
| `completed`  | The target is assigned and read-only is lifted        |
| `failed`     | The source remains assigned and read-only is lifted   |

## Rejected alternatives

### Live migration with writes enabled

Concurrent writes could reach the source after copying. Without a conflict protocol, the result would be ambiguous.

### A server-to-server mirror push

The current Lore SDK has no whole-repository mirror primitive or atomic cross-server push. Claiming otherwise would make
the migration appear stronger than the available operations.

### Repoint before verification

Changing the pointer before all selected branches are pushed would expose incomplete data and complicate recovery.

### Reuse or overwrite an existing target partition

An existing partition may contain unrelated data. A fresh partition gives the operator a clear orphan to inspect and
clean up after a failed migration.

## Operational consequences

- An instance administrator must schedule a read-only window. Lore clients and repository write APIs are rejected while
  the copy runs, while repository reads remain available.
- The source and target Lore Servers must be reachable from LoreHub, and the target must have capacity for a fresh
  partition.
- A failed migration does not clean up the target automatically. Operators must inspect and remove the orphaned target
  partition after confirming that the source is still usable.
- The migration row records its current state and failure text. A retry creates a new row after the
  failed row is retained.
- The bounded copy preserves reachable revision history from active branch tips, not every piece of server-local Lore
  metadata. Operators should verify branches and important repository data before allowing normal writes.
