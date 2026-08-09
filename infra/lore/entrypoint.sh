#!/bin/sh
set -eu

mkdir -p /data/store

for required in ca.crt server.crt server.key lore-client.crt lore-client.key; do
  test -s "/etc/lore/auth-tls/${required}"
done

cp /etc/lore/auth-tls/ca.crt /usr/local/share/ca-certificates/lorehub-local.crt
update-ca-certificates >/dev/null

environment=${LOREHUB_ENV:-development}
case "$environment" in
  production)
    root_domain=${LOREHUB_LORE_ROOT_DOMAIN:?LOREHUB_LORE_ROOT_DOMAIN is required}
    auth_issuer=${LOREHUB_LORE_AUTH_ISSUER:?LOREHUB_LORE_AUTH_ISSUER is required}
    auth_audience=${LOREHUB_LORE_AUTH_AUDIENCE:?LOREHUB_LORE_AUTH_AUDIENCE is required}
    jwks_endpoint=${LOREHUB_LORE_AUTH_JWKS_URL:?LOREHUB_LORE_AUTH_JWKS_URL is required}
    auth_url=${LOREHUB_LORE_AUTH_URL:?LOREHUB_LORE_AUTH_URL is required}
    policy_endpoint=${LOREHUB_LORE_POLICY_ENDPOINT:?LOREHUB_LORE_POLICY_ENDPOINT is required}
    observation_endpoint=${LOREHUB_LORE_OBSERVATION_ENDPOINT:?LOREHUB_LORE_OBSERVATION_ENDPOINT is required}
    lore_environment=production
    config=/etc/lore/config/production.toml
    case "$jwks_endpoint:$policy_endpoint:$observation_endpoint" in
      https://*) ;;
      *) echo "Lore production endpoints must use HTTPS" >&2; exit 1 ;;
    esac
    ;;
  development|local-insecure)
    root_domain=${LOREHUB_LORE_ROOT_DOMAIN:-lorehub.localhost}
    auth_issuer=${LOREHUB_LORE_AUTH_ISSUER:-auth.${root_domain}}
    auth_audience=${LOREHUB_LORE_AUTH_AUDIENCE:-$root_domain}
    jwks_endpoint=${LOREHUB_LORE_AUTH_JWKS_URL:-http://api.${root_domain}:8080/.well-known/jwks.json}
    auth_url=${LOREHUB_LORE_AUTH_URL:-ucs-auth://auth.${root_domain}:8443}
    policy_endpoint=${LOREHUB_LORE_POLICY_ENDPOINT:-https://api.${root_domain}:8444/internal/lore/policy}
    observation_endpoint=${LOREHUB_LORE_OBSERVATION_ENDPOINT:-https://api.${root_domain}:8444/internal/lore/observation}
    lore_environment=$environment
    config=/etc/lore/config/local.toml
    ;;
  *)
    echo "LOREHUB_ENV must be production, development, or local-insecure" >&2
    exit 1
    ;;
esac

test -s "$config"
case "$root_domain" in
  *[!A-Za-z0-9.-]*) echo "Lore root domain contains invalid characters" >&2; exit 1 ;;
esac
case "$auth_issuer" in
  *[!A-Za-z0-9.-]*) echo "Lore issuer contains invalid characters" >&2; exit 1 ;;
esac
case "$auth_audience" in
  *[!A-Za-z0-9.-]*) echo "Lore audience contains invalid characters" >&2; exit 1 ;;
esac
case "$auth_url" in
  ucs-auth://*) ;;
  *) echo "LOREHUB_LORE_AUTH_URL must be a ucs-auth:// endpoint" >&2; exit 1 ;;
esac
case "$auth_audience" in
  *','*|*\"*) echo "LOREHUB_LORE_AUTH_AUDIENCE must contain one root domain" >&2; exit 1 ;;
esac

# Lore 0.8.6 layers environment and local files after default.toml. Rendering a
# single selected file keeps local values from ever leaking into production.
config_dir=/run/lore-config
mkdir -p "$config_dir"
sed \
  -e "s|@ROOT_DOMAIN@|$root_domain|g" \
  -e "s|@AUTH_ISSUER@|$auth_issuer|g" \
  -e "s|@AUTH_AUDIENCE@|$auth_audience|g" \
  -e "s|@JWKS_ENDPOINT@|$jwks_endpoint|g" \
  -e "s|@AUTH_URL@|$auth_url|g" \
  -e "s|@POLICY_ENDPOINT@|$policy_endpoint|g" \
  -e "s|@OBSERVATION_ENDPOINT@|$observation_endpoint|g" \
  "$config" >"$config_dir/default.toml"

require_setting() {
  if ! grep -Eq "$1" "$config_dir/default.toml"; then
    echo "Missing required Lore setting: $2" >&2
    exit 1
  fi
}

require_setting '^\[server\.auth\][[:space:]]*$' '[server.auth]'
require_setting '^[[:space:]]*jwt_issuer[[:space:]]*=[[:space:]]*"[^"@]+' \
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
require_setting '^\[hooks\.lorehub_policy\][[:space:]]*$' '[hooks.lorehub_policy]'
require_setting '^[[:space:]]*root_domain[[:space:]]*=[[:space:]]*"[^"[:space:]]+"' \
  'hooks.lorehub_policy.root_domain'
require_setting '^[[:space:]]*auth_endpoint[[:space:]]*=[[:space:]]*"ucs-auth://[^"[:space:]]+"' \
  'hooks.lorehub_policy.auth_endpoint'
require_setting '^[[:space:]]*jwks_endpoint[[:space:]]*=[[:space:]]*"https?://[^"[:space:]]+"' \
  'hooks.lorehub_policy.jwks_endpoint'
require_setting '^[[:space:]]*endpoint[[:space:]]*=[[:space:]]*"https?://[^"[:space:]]+"' \
  'hooks.lorehub_policy.endpoint'
require_setting '^[[:space:]]*observation_endpoint[[:space:]]*=[[:space:]]*"https?://[^"[:space:]]+"' \
  'hooks.lorehub_policy.observation_endpoint'
require_setting '^[[:space:]]*auth_url[[:space:]]*=[[:space:]]*"ucs-auth://[^"[:space:]]+"' \
  'environment.endpoint.auth_url'

exec loreserver --config "$config_dir" --env "$lore_environment"
