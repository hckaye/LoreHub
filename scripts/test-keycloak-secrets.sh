#!/bin/sh
# Isolated behavior test for setup-keycloak-secrets.sh. Secret values stay in
# shell variables and are never included in test output.
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)
sandbox=$(mktemp -d \
  "${TMPDIR:-/tmp}/lorehub-keycloak-secrets.XXXXXX"
)
trap 'rm -rf "$sandbox"' EXIT HUP INT TERM
env_file="${sandbox}/.env"

value_for() {
  awk -F= -v key="$1" '$1 == key { print substr($0, index($0, "=") + 1); exit }' "$env_file"
}

count_for() {
  awk -F= -v key="$1" '$1 == key { count++ } END { print count + 0 }' "$env_file"
}

assert_no_secret_output() {
  output_text=$1
  case "$output_text" in
    *POSTGRES_PASSWORD=*)
      echo "setup output must not contain secret assignments" >&2
      exit 1
      ;;
  esac
  for key in POSTGRES_PASSWORD KEYCLOAK_ADMIN_PASSWORD KEYCLOAK_DB_PASSWORD \
    LOREHUB_OIDC_CLIENT_SECRET LOREHUB_AUTH_SECRET LOREHUB_ACTIONS_SECRET_KEY; do
    secret=$(value_for "$key")
    case "$output_text" in
      *"$secret"*)
        echo "setup output must not contain secret values" >&2
        exit 1
        ;;
    esac
  done
}

mode=$(uname -s)
if [ "$mode" = "Darwin" ]; then
  file_mode() { stat -f '%A' "$1"; }
else
  file_mode() { stat -c '%a' "$1"; }
fi

output=$("${root}/scripts/setup-keycloak-secrets.sh" --env-file "$env_file")
assert_no_secret_output "$output"
test "$(file_mode "$env_file")" = 600
for key in POSTGRES_PASSWORD KEYCLOAK_ADMIN_PASSWORD KEYCLOAK_DB_PASSWORD \
  LOREHUB_OIDC_CLIENT_SECRET LOREHUB_AUTH_SECRET LOREHUB_ACTIONS_SECRET_KEY; do
  test -n "$(value_for "$key")"
done
test "$(value_for LOREHUB_ACTIONS_SECRET_KEY_ID)" = local-actions-v1

printf '%s\n' \
  'MY_POSTGRES_PASSWORD=must-stay' \
  'MY_KEYCLOAK_ADMIN_PASSWORD=must-stay' \
  'MY_KEYCLOAK_DB_PASSWORD=must-stay' \
  'MY_LOREHUB_OIDC_CLIENT_SECRET=must-stay' \
  'MY_LOREHUB_AUTH_SECRET=must-stay' \
  'MY_LOREHUB_ACTIONS_SECRET_KEY=must-stay' \
  'POSTGRES_PASSWORD=preserve-me' \
  'NON_SECRET_SETTING=keep-me' >"$env_file"
chmod 644 "$env_file"
output=$("${root}/scripts/setup-keycloak-secrets.sh" --env-file "$env_file")
assert_no_secret_output "$output"
test "$(file_mode "$env_file")" = 600
test "$(value_for POSTGRES_PASSWORD)" = preserve-me
test "$(value_for MY_POSTGRES_PASSWORD)" = must-stay
test "$(value_for MY_KEYCLOAK_ADMIN_PASSWORD)" = must-stay
test "$(value_for MY_KEYCLOAK_DB_PASSWORD)" = must-stay
test "$(value_for MY_LOREHUB_OIDC_CLIENT_SECRET)" = must-stay
test "$(value_for MY_LOREHUB_AUTH_SECRET)" = must-stay
test "$(value_for MY_LOREHUB_ACTIONS_SECRET_KEY)" = must-stay
test "$(value_for NON_SECRET_SETTING)" = keep-me
for key in POSTGRES_PASSWORD KEYCLOAK_ADMIN_PASSWORD KEYCLOAK_DB_PASSWORD \
  LOREHUB_OIDC_CLIENT_SECRET LOREHUB_AUTH_SECRET LOREHUB_ACTIONS_SECRET_KEY; do
  test "$(count_for "$key")" = 1
  test -n "$(value_for "$key")"
done
test "$(value_for LOREHUB_ACTIONS_SECRET_KEY_ID)" = local-actions-v1

preserved=$(value_for POSTGRES_PASSWORD)
output=$("${root}/scripts/setup-keycloak-secrets.sh" --env-file "$env_file" --force)
assert_no_secret_output "$output"
forced=$(value_for POSTGRES_PASSWORD)
test "$forced" != "$preserved"
test "$(file_mode "$env_file")" = 600
for key in POSTGRES_PASSWORD KEYCLOAK_ADMIN_PASSWORD KEYCLOAK_DB_PASSWORD \
  LOREHUB_OIDC_CLIENT_SECRET LOREHUB_AUTH_SECRET LOREHUB_ACTIONS_SECRET_KEY; do
  test "$(count_for "$key")" = 1
done
test "$(count_for LOREHUB_ACTIONS_SECRET_KEY_ID)" = 1
test "$(value_for NON_SECRET_SETTING)" = keep-me

echo "Keycloak secret setup behavior passed."
