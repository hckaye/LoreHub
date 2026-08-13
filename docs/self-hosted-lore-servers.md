# Self-hosted Lore Servers

[English](self-hosted-lore-servers.md) | [日本語](self-hosted-lore-servers.ja.md)

A self-hosted Lore Server belongs to one organization. LoreHub stores its advertised `lores://` URL and gives the
server a credential for the registration and heartbeat API.

## Registration flow

An organization owner creates a Lore Server registration token from the organization settings. The token begins with
`lhsr_` and is consumed once.

On the Lore Server host, run `configure`:

```bash
sudo -u lorehub-lores env \
  LOREHUB_LORE_VERSION=0.8.6 \
  LOREHUB_HOOK_MODULE_VERSION=1.0.0 \
  /usr/local/bin/lorehub-lores-agent configure \
  --url https://lorehub.example \
  --lores-url lores://lore.example:41337 \
  --name lore-storage-1 \
  --config-dir /var/lib/lorehub-lores-agent \
  < /path/to/lhsr-registration-token
```

The command sends the server name, advertised URL, Lore build version, and hook module version to
`POST /api/v1/lore-servers/register`. The API returns a credential beginning with `lhss_` and a server ID. The
agent saves them in `config.json`. The configuration file has mode `0600`; its parent directory has mode `0700`.

The token is read from standard input by default. `--token` is available for automation, but the command prints a
warning because command-line arguments can appear in process lists. The registration token is not saved.

The agent accepts `--lore-version` and `--hook-module-version` on `configure` and `run`. The corresponding environment
variables are `LOREHUB_LORE_VERSION` and `LOREHUB_HOOK_MODULE_VERSION`. The defaults are `0.8.6` and `1.0.0`.
Override them when the Lore build or hook module uses another supported version.

## Install and start the agent

Build the command and install the binary and systemd unit:

```bash
go build -o /tmp/lorehub-lores-agent ./services/api/cmd/lorehub-lores-agent
sudo ./scripts/lores-agent/install.sh --binary /tmp/lorehub-lores-agent
```

The installer creates the `lorehub-lores` system user, `/var/lib/lorehub-lores-agent`, and
`lorehub-lores-agent.service`. The unit runs with the configuration directory
`/var/lib/lorehub-lores-agent`.

Create `/etc/lorehub/lorehub-lores-agent.env` if the version values need to be set for systemd:

```text
LOREHUB_LORE_VERSION=0.8.6
LOREHUB_HOOK_MODULE_VERSION=1.0.0
```

Register the server, then start the unit:

```bash
sudo -u lorehub-lores /usr/local/bin/lorehub-lores-agent configure \
  --url https://lorehub.example \
  --lores-url lores://lore.example:41337 \
  --config-dir /var/lib/lorehub-lores-agent
sudo systemctl enable --now lorehub-lores-agent.service
```

`run` sends the first heartbeat immediately and sends another heartbeat every 60 seconds by default. Use
`--interval 30s` to change the interval. Each request includes the build versions, process ID, start time, and
uptime in `healthMetadata`.

The agent exits with a clear authentication error when LoreHub returns 401, including after a server is revoked.
Other heartbeat failures are logged and retried. SIGTERM stops the loop without an error.

The heartbeat shows that the agent host is running. It does not prove that the advertised Lore endpoint is reachable.
The API checks that endpoint separately before provisioning and during server health evaluation.

## Server selection for repositories

Repository creation selects a server in this order:

1. An explicit repository server ID.
2. The organization's default Lore Server.
3. The instance Lore Server.

An organization-scoped registered server can be selected without the `hosted_lore_server` entitlement. The instance
server is different. Selecting it, including falling through to it when no organization server is configured, requires
an active organization entitlement with feature `hosted_lore_server`.

Without that entitlement, repository creation fails with guidance to register an organization Lore Server or obtain the
entitlement. Existing organizations receive a migration grant so their existing behavior continues after the schema
upgrade.

Importing a repository also requires a registered server ID. The imported `lores://` URL must have the same authority
as the registered server.
