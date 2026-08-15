# Resource limits

[English](resource-limits.md) | [日本語](resource-limits.ja.md)

The API and Lore Server can cap organizations per user, repositories per organization, the file-tree
size of a pushed revision, and cumulative upload bytes per repository.

## Organizations per user

Set `LOREHUB_MAX_ORGANIZATIONS_PER_USER` on the `api` service. The default is `0`, which is
unlimited. Creating an organization beyond the limit returns HTTP 409 with problem code
`organization_limit`. The web UI shows the error.

## Repositories per organization

Set `LOREHUB_MAX_REPOSITORIES_PER_ORGANIZATION` on the `api` service. The default is `0`, which is
unlimited. The limit applies when creating a hosted repository and when importing an existing one.
Retrying a failed provisioning of an already-registered repository is not blocked. Exceeding the
limit returns HTTP 409 with problem code `repository_limit`.

## Repository size at push time

Set `LOREHUB_MAX_REPOSITORY_SIZE_BYTES` on the `api` service. The default is `0`, which is
unlimited. The Lore Server reports the total file-tree size of each pushed revision to the control
plane. A push that would exceed the limit is rejected before the branch updates. The official `lore`
client shows the server's message. Data already uploaded for a rejected push remains in the Lore
store until an administrator obliterates it.

## Instance admin overrides

The three API limits can also be overridden from the instance admin settings screen. A database
override wins over the environment value. Clearing the override returns to the environment default.

## Upload quota on the Lore Server

A build-time patch adds an `[upload_quota]` section to the Lore Server config in
`infra/lore/production.toml`:

- `enabled`
- `default_bytes`: cumulative received bytes allowed per repository. The count is taken before
  data is written. An upload that would exceed the quota is rejected with an Oversized error.
- `state_path`: persists the per-repository counter under `/data`
- `flush_seconds`

The counter is cumulative uploaded bytes, not current disk usage. History rewrites and rejected
pushes consume the quota. `default_bytes` is 32 MiB. That value is above a typical 10 MB repository
size limit so history has room.

## Recommended settings for a public test deployment

```dotenv
LOREHUB_MAX_ORGANIZATIONS_PER_USER=1
LOREHUB_MAX_REPOSITORIES_PER_ORGANIZATION=1
LOREHUB_MAX_REPOSITORY_SIZE_BYTES=10485760
```

Keep the Lore Server `[upload_quota]` defaults in `infra/lore/production.toml`, including
`default_bytes` of 32 MiB. Also cap the Lore data volume size as a last-resort backstop.
