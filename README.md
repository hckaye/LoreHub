# LoreHub

[English](README.md) | [日本語](README.ja.md)

LoreHub is a self-hosted collaboration service for teams using Lore. It brings Lore repository browsing, issues,
pull requests, reviews, releases, and GitHub Actions-compatible CI into one web application. Its workflows and layout
take cues from GitHub and GitLab, while version control operations use Lore.

## Run with Docker

Install Docker Engine or Docker Desktop with Docker Compose, then run:

```bash
scripts/setup-keycloak-secrets.sh
docker compose -f infra/compose.yaml up --build
```

Open <http://localhost:3000>. A new installation starts empty. Use **Sign up** to create an account, then create an
organization and a Lore repository.

The local services are available at:

- Web: <http://localhost:3000>
- API health: <http://localhost:8080/health/ready>
- Lore Server health: <http://localhost:41339/health_check>
- Keycloak administration: <http://keycloak.localhost:8280/admin/master/console>

Stop the services without deleting their data:

```bash
docker compose -f infra/compose.yaml down
```

## Features

- Browse files, branches, revisions, history, and diffs
- Track work with issues, labels, comments, and projects
- Review changes with pull requests, reviews, and branch rules
- Merge Lore branches, resolve conflicts, and push the result
- Publish releases and attach links to externally hosted files
- Manage organizations, teams, repository visibility, and access
- Star and watch repositories, receive notifications, and search
- Sign in with email and password or configured social providers
- Use the interface in English or Japanese
- Run GitHub Actions-compatible workflows with logs, artifacts, variables, secrets, and SARIF results

LoreHub is under active development. It does not yet provide every screen or API available in GitHub and GitLab.

## Configure sign-in providers

Email and password sign-in works with the default local configuration. Google, GitHub, Facebook, and X appear on the
sign-in page after their client IDs and client secrets are added to `.env`. See the
[Keycloak operations guide](docs/operations/keycloak.md) for setup steps and callback URLs.

## Run GitHub Actions-compatible CI

Start the stack with the runner profile:

```bash
docker compose -f infra/compose.yaml --profile runner up --build
```

The runner executes `.github/workflows/*.yml` and `.yaml`. `actions/checkout@v4` places the requested Lore revision in
the job workspace.

Linux container workflows are supported. Windows and macOS runners and exact parity with GitHub-hosted runner images
are not supported. See the [Actions runner guide](docs/runner-actions.md) for supported workflow syntax and production
deployment requirements.

## Develop locally

Running outside Docker requires:

- Node.js 24.19 or later
- Go 1.26.5 or later
- PostgreSQL 18 or later
- Lore Server and CLI 0.8.6
- Lore Go SDK and native library 0.8.5

Start the web application:

```bash
npm ci
npm run dev
```

Start the API in another terminal. Set `LORE_LIB_PATH` to the `liblore` file for the host operating system.

```bash
set -a
. ./.env
set +a
export DATABASE_URL="postgresql://lorehub:${POSTGRES_PASSWORD}@localhost:5432/lorehub"
export LORE_LIB_PATH=/absolute/path/to/liblore
cd services/api
go run ./cmd/lorehub migrate
go run ./cmd/lorehub serve
```

Set `LOREHUB_AUTH_MODE=disabled` only for local development without sign-in. Do not disable authentication in
production.

## Check changes

```bash
npm run check
```

This command checks formatting, file limits, lint rules, types, tests, the production build, and dependency
vulnerabilities. [GitHub Actions](.github/workflows/ci.yml) runs the same checks.

## Design and operations

- [Architecture decisions](docs/adr/0001-platform-architecture.md)
- [Web, API, and Lore Server responsibilities](docs/architecture/components.md)
- [Sign-in and identity management](docs/architecture/identity.md)
- [Repository access control](docs/operations/control-plane-authorization.md)
- [Keycloak, social sign-in, email, and backups](docs/operations/keycloak.md)
- [GitHub Actions compatibility and runner operations](docs/runner-actions.md)

## License

LoreHub is available under the [MIT License](LICENSE).
