# lh CLI

[English](lh-cli.md) | [日本語](lh-cli.ja.md)

`lh` is the LoreHub command-line client. It uses the LoreHub API for forge operations. The `lore` CLI remains the
client for repository data such as cloning, syncing, and pushing.

## Install

Go 1.26 or later is required. Build the binary from the repository root:

```bash
mkdir -p bin
(cd cli && go build -o ../bin/lh ./cmd/lh)
```

Add `bin` to `PATH`, or call the binary by its path.

## Log in

Create a personal access token in **Account settings** under **Personal access tokens**. Use `read_api` for
read-only API commands and `api` for commands that change LoreHub data. `lh repo clone` also needs `read_repository` or
`write_repository`. See [Personal access tokens](personal-access-tokens.md) for the available permissions.

An interactive login reads the token without echoing it:

```bash
lh --host lorehub.example auth login
```

Pass a token on standard input for scripts:

```bash
lh --host lorehub.example auth login --with-token < token.txt
```

Check the stored host, user, permissions, and expiry:

```bash
lh --host lorehub.example auth status
```

`LH_HOST` selects a host when `--host` is not set. `LH_TOKEN` supplies a token for the selected host without writing it
to the hosts file. The default configuration file is `~/.config/lh/hosts.yml`, or `$XDG_CONFIG_HOME/lh/hosts.yml` when
`XDG_CONFIG_HOME` is set.

## Repository context

Commands that operate on a repository use this order:

1. `--repo [HOST/]OWNER/NAME`
2. `LH_REPO`
3. The default saved by `lh repo set-default`

Set a default repository for the selected host:

```bash
lh --host lorehub.example repo set-default acme/widget
```

## Commands

| Command                                                         | Description                    |
| --------------------------------------------------------------- | ------------------------------ |
| `lh auth login`, `logout`, `status`                             | Manage the API token per host. |
| `lh repo list`, `view`, `create`, `clone`                       | List and manage repositories.  |
| `lh issue list`, `view`, `create`, `comment`, `close`, `reopen` | Manage issues.                 |
| `lh pr list`, `view`, `create`, `merge`                         | Manage pull requests.          |
| `lh release list`, `view TAG-or-ID`, `create`                   | Manage releases.               |
| `lh run list`, `view NUMBER`, `watch NUMBER`                    | Inspect Actions workflow runs. |
| `lh label list`, `create`, `delete NAME`                        | Manage repository labels.      |
| `lh search repos QUERY`, `issues QUERY`, `prs QUERY`            | Search the host.               |
| `lh api PATH`                                                   | Send a raw `/api/v1` request.  |

Command notes:

- `lh repo clone` logs the `lore` CLI in with the repository URL, then runs `lore clone`.
- `lh release create` accepts `--tag`, `--title`, `--notes`, and `--branch`.
- `lh run watch` accepts `--interval` and `--timeout` and exits successfully only for a `success` conclusion.
- `lh label create` accepts `--name`, `--color`, and `--description`. `delete` resolves the name to a label ID.

`lh repo clone` requires the `lore` binary on `PATH`. It checks the API token permissions before starting the Lore
authentication. If a required permission is missing, it reports the missing permission and does not run `lore`.

## JSON output

Use the global `--json` flag with list, view, and search commands to write the API response as indented JSON. The flag
can appear before or after the command:

```bash
lh --repo acme/widget --json issue list
lh --repo acme/widget release view v1.0.0 --json
lh --repo acme/widget --json run view 42
```

Without `--json`, a terminal receives a table. Piped output uses tab-separated rows.

## Examples

Create and inspect a release:

```bash
lh --repo acme/widget release create \
  --tag v1.0.0 \
  --title "First release" \
  --notes "Initial public release" \
  --branch main
lh --repo acme/widget release view v1.0.0
```

Wait for a workflow run and preserve its exit status in a script:

```bash
lh --repo acme/widget run watch 42 --interval 5s --timeout 30m
```

Create and remove a label by name:

```bash
lh --repo acme/widget label create --name bug --color ff0000 --description "Confirmed defect"
lh --repo acme/widget label delete bug
```

Search and consume the response with a JSON tool:

```bash
lh --json search prs "release notes" | jq '.pullRequests[] | [.repository.slug, .number, .title]'
```
