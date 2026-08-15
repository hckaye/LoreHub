#!/bin/sh
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
sandbox=$(mktemp -d \
  "${TMPDIR:-/tmp}/lorehub-compose-setup.XXXXXX"
)
trap 'rm -rf "$sandbox"' EXIT HUP INT TERM

env_file="${sandbox}/.env"
"${root}/scripts/setup-secrets.sh" --env-file "$env_file" >/dev/null
docker compose --env-file "$env_file" -f "${root}/infra/compose.yaml" config -q

echo "Clean Compose setup validation passed."
