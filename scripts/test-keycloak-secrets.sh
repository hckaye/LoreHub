#!/bin/sh
# Isolated behavior test for setup-keycloak-secrets.sh. Secret values stay in
# shell variables and are never included in test output.
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
sandbox=$(mktemp -d "${TMPDIR:-/tmp}/lorehub-keycloak-secrets.XXXXXX")
trap 'rm -rf "$sandbox"' EXIT HUP INT TERM
env_file="${sandbox}/.env"

value_for() {
  awk -F= -v key="$1" '$1 == key { print substr($0, index($0, "=") + 1); exit }' "$env_file"
}

mode=$(uname -s)
if [ "$mode" = "Darwin" ]; then
  file_mode() { stat -f '%A' "$1"; }
else
  file_mode() { stat -c '%a' "$1"; }
fi

output=$("${root}/scripts/setup-keycloak-secrets.sh" --env-file "$env_file")
case "$output" in
  *POSTGRES_PASSWORD=*)
    echo "setup output must not contain secret assignments" >&2
    exit 1
    ;;
esac
test "$(file_mode "$env_file")" = 600
for key in POSTGRES_PASSWORD KEYCLOAK_ADMIN_PASSWORD KEYCLOAK_DB_PASSWORD \
  LOREHUB_OIDC_CLIENT_SECRET LOREHUB_AUTH_SECRET; do
  test -n "$(value_for "$key")"
  secret=$(value_for "$key")
  case "$output" in
    *"$secret"*)
      echo "setup output must not contain secret values" >&2
      exit 1
      ;;
  esac
done

printf '%s\n' \
  'POSTGRES_PASSWORD=preserve-me' \
  'KEYCLOAK_ADMIN_PASSWORD=' \
  'KEYCLOAK_DB_PASSWORD=keep-db' \
  'LOREHUB_OIDC_CLIENT_SECRET=' \
  'LOREHUB_AUTH_SECRET=keep-auth' >"$env_file"
chmod 644 "$env_file"
output=$("${root}/scripts/setup-keycloak-secrets.sh" --env-file "$env_file")
test "$(file_mode "$env_file")" = 600
test "$(value_for POSTGRES_PASSWORD)" = preserve-me
test "$(value_for KEYCLOAK_DB_PASSWORD)" = keep-db
test "$(value_for LOREHUB_AUTH_SECRET)" = keep-auth
test -n "$(value_for KEYCLOAK_ADMIN_PASSWORD)"
test -n "$(value_for LOREHUB_OIDC_CLIENT_SECRET)"

preserved=$(value_for POSTGRES_PASSWORD)
output=$("${root}/scripts/setup-keycloak-secrets.sh" --env-file "$env_file" --force)
forced=$(value_for POSTGRES_PASSWORD)
test "$forced" != "$preserved"
test "$(file_mode "$env_file")" = 600

echo "Keycloak secret setup behavior passed."
