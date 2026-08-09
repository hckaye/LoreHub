#!/bin/sh
# Generate and persist the secrets required by LoreHub and Keycloak.
#
# Values are written to a local env file and are never printed. Existing
# non-empty values are preserved unless --force is supplied. An env file path
# can be provided with LOREHUB_ENV_FILE or --env-file.
set -eu

umask 077

root=$(cd "$(dirname "$0")/.." && pwd)
env_file=${LOREHUB_ENV_FILE:-${root}/.env}
force=0

while [ "$#" -gt 0 ]; do
  case "$1" in
    --force)
      force=1
      ;;
    --env-file)
      shift
      if [ "$#" -eq 0 ]; then
        echo "--env-file requires a path" >&2
        exit 2
      fi
      env_file=$1
      ;;
    --env-file=*)
      env_file=${1#--env-file=}
      if [ -z "$env_file" ]; then
        echo "--env-file requires a path" >&2
        exit 2
      fi
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 2
      ;;
  esac
  shift
done

gen_secret() {
  bytes=${1:-32}
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 "$bytes" | tr '+/' '-_' | tr -d '=\n'
  else
    head -c "$bytes" /dev/urandom | base64 | tr '+/' '-_' | tr -d '=\n'
  fi
}

if [ ! -f "$env_file" ]; then
  if [ -f "${root}/.env.example" ]; then
    cp "${root}/.env.example" "$env_file"
  else
    : >"$env_file"
  fi
fi

# Set KEY=VALUE without exposing VALUE. Blank values are replaced; existing
# non-empty values remain unchanged unless --force is requested.
set_var() {
  key=$1
  value=$2
  if grep -Fq "${key}=" "$env_file"; then
    current=$(awk -v key="$key" '
      index($0, key "=") == 1 { print substr($0, length(key) + 2); exit }
    ' "$env_file")
    if [ "$force" -eq 0 ] && [ -n "$current" ]; then
      return
    fi
    temporary=$(mktemp "${env_file}.tmp.XXXXXX")
    awk -v key="$key" -v value="$value" '
      index($0, key "=") == 1 { print key "=" value; next }
      { print }
    ' "$env_file" >"$temporary"
    mv "$temporary" "$env_file"
  else
    printf '%s=%s\n' "$key" "$value" >>"$env_file"
  fi
}

set_var POSTGRES_PASSWORD "$(gen_secret 24)"
set_var KEYCLOAK_ADMIN_PASSWORD "$(gen_secret 24)"
set_var KEYCLOAK_DB_PASSWORD "$(gen_secret 24)"
set_var LOREHUB_OIDC_CLIENT_SECRET "$(gen_secret 32)"
set_var LOREHUB_AUTH_SECRET "$(gen_secret 32)"

chmod 600 "$env_file"

echo "Updated ${env_file}; generated or preserved the configured secret fields."
echo "Run: docker compose -f infra/compose.yaml up"
