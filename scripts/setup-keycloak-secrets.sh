#!/bin/sh
# One-command generation of the persistent secrets LoreHub + Keycloak need.
#
# Docker Compose cannot safely generate persistent secrets itself, so this
# script creates cryptographically random values and writes them into a local
# .env file (which is git-ignored). No secret value is ever printed to the
# terminal or to logs. Existing values in .env are preserved.
#
# Usage:
#   scripts/setup-keycloak-secrets.sh           # create/merge .env
#   scripts/setup-keycloak-secrets.sh --force   # overwrite existing secrets
#
# This script only writes secrets to .env (git-ignored via .gitignore). It does
# not commit anything and never emits secret values.
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
env_file="${root}/.env"
force=0
for arg in "$@"; do
  case "$arg" in
    --force) force=1 ;;
    *) echo "unknown argument: $arg" >&2; exit 2 ;;
  esac
done

# Generate a URL-safe random secret of the given byte length (default 32).
gen_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 "${1:-32}" | tr -d '\n'
  else
    head -c "${1:-32}" /dev/urandom | base64 | tr -d '\n'
  fi
}

if [ ! -f "${env_file}" ]; then
  if [ -f "${root}/.env.example" ]; then
    cp "${root}/.env.example" "${env_file}"
  else
    : > "${env_file}"
  fi
  echo "Created ${env_file} from .env.example"
fi

# Set KEY=VALUE in .env if missing (or if --force). Never prints the value.
set_var() {
  key="$1"
  value="$2"
  if grep -q "^${key}=" "${env_file}"; then
    if [ "${force}" -eq 1 ]; then
      # Replace the existing line without echoing the value.
      tmp=$(mktemp)
      awk -v k="${key}" -v v="${value}" '
        $0 ~ "^" k "=" { print k "=" v; next }
        { print }
      ' "${env_file}" > "${tmp}" && mv "${tmp}" "${env_file}"
    fi
  else
    printf '%s=%s\n' "${key}" "${value}" >> "${env_file}"
  fi
}

set_var KEYCLOAK_ADMIN_PASSWORD "$(gen_secret 24)"
set_var KEYCLOAK_DB_PASSWORD "$(gen_secret 24)"
set_var LOREHUB_OIDC_CLIENT_SECRET "$(gen_secret 32)"

echo "Wrote secrets into ${env_file}:"
echo "  KEYCLOAK_ADMIN_PASSWORD"
echo "  KEYCLOAK_DB_PASSWORD"
echo "  LOREHUB_OIDC_CLIENT_SECRET"
echo "Review the file, then run: docker compose -f infra/compose.yaml up"
echo "Do not commit .env. It is already listed in .gitignore."
