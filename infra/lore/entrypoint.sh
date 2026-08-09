#!/bin/sh
set -eu

mkdir -p /data/store

test -s /etc/lore/auth-tls/ca.crt
test -s /etc/lore/auth-tls/server.crt
test -s /etc/lore/auth-tls/server.key
test -s /etc/lore/auth-tls/lore-client.crt
test -s /etc/lore/auth-tls/lore-client.key

cp /etc/lore/auth-tls/ca.crt /usr/local/share/ca-certificates/lorehub-local.crt
update-ca-certificates >/dev/null

case "${LOREHUB_ENV:-development}" in
  production)
    config=/etc/lore/config/production.toml
    lore_environment=production
    policy_endpoint='https://api.lorehub.example:8444/internal/lore/policy'
    observation_endpoint='https://api.lorehub.example:8444/internal/lore/observation'
    auth_url='ucs-auth://auth.lorehub.example:8443'
    ;;
  development|local-insecure)
    config=/etc/lore/config/local.toml
    lore_environment=local-insecure
    policy_endpoint='https://api.lorehub.localhost:8444/internal/lore/policy'
    observation_endpoint='https://api.lorehub.localhost:8444/internal/lore/observation'
    auth_url='ucs-auth://auth.lorehub.localhost:8443'
    ;;
  *)
    echo "LOREHUB_ENV must be production, development, or local-insecure" >&2
  exit 1
  ;;
esac
test -s "$config"

# Lore 0.8.6 always layers local.toml after the selected environment file. Use
# a fresh directory containing only the selected file so production never gets
# local issuer, audience, endpoint, or hook settings mixed into its config.
config_dir=/run/lore-config
mkdir -p "$config_dir"
install -m 0644 "$config" "$config_dir/default.toml"

require_setting() {
  if ! grep -Eq "$1" "$config"; then
    echo "Missing required Lore setting: $2" >&2
    exit 1
  fi
}

require_literal() {
  if ! grep -Fq "$1" "$config"; then
    echo "Missing required Lore setting: $2" >&2
    exit 1
  fi
}

require_setting '^\[server\.auth\][[:space:]]*$' '[server.auth]'
require_setting '^[[:space:]]*jwt_issuer[[:space:]]*=[[:space:]]*"[^"]+"' \
  'server.auth.jwt_issuer'
require_setting '^[[:space:]]*jwt_audience[[:space:]]*=[[:space:]]*\[[^]]+\]' \
  'server.auth.jwt_audience'
require_setting '^\[server\.auth\.jwk\][[:space:]]*$' '[server.auth.jwk]'
require_setting '^[[:space:]]*endpoint[[:space:]]*=[[:space:]]*"https?://[^"[:space:]]+"' \
  'server.auth.jwk.endpoint'
require_setting '^\[server\.quic\.certificate\][[:space:]]*$' \
  '[server.quic.certificate]'
require_setting '^\[server\.grpc\.certificate\][[:space:]]*$' \
  '[server.grpc.certificate]'
require_literal "auth_url = \"${auth_url}\"" 'environment.endpoint.auth_url'
require_setting '^\[hooks\.lorehub_policy\][[:space:]]*$' '[hooks.lorehub_policy]'
require_literal "endpoint = \"${policy_endpoint}\"" 'hooks.lorehub_policy.endpoint'
require_literal "observation_endpoint = \"${observation_endpoint}\"" \
  'hooks.lorehub_policy.observation_endpoint'

exec loreserver --config "$config_dir" --env "$lore_environment"
