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

config=/etc/lore/config/local.toml
test -s "$config"

require_setting() {
  if ! grep -Eq "$1" "$config"; then
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
require_setting '^[[:space:]]*auth_url[[:space:]]*=[[:space:]]*"https://[^"[:space:]]+"' \
  'environment.endpoint.auth_url'
require_setting '^\[hooks\.lorehub_policy\][[:space:]]*$' '[hooks.lorehub_policy]'
require_setting '^[[:space:]]*endpoint[[:space:]]*=[[:space:]]*"https://api:8444/internal/lore/policy"' \
  'hooks.lorehub_policy.endpoint'

exec loreserver --config /etc/lore/config
