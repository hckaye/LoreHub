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
    internal_jwks_endpoint=${LOREHUB_LORE_INTERNAL_AUTH_JWKS_URL:?LOREHUB_LORE_INTERNAL_AUTH_JWKS_URL is required}
    internal_auth_url=${LOREHUB_LORE_INTERNAL_AUTH_URL:?LOREHUB_LORE_INTERNAL_AUTH_URL is required}
    internal_policy_endpoint=${LOREHUB_LORE_INTERNAL_POLICY_ENDPOINT:?LOREHUB_LORE_INTERNAL_POLICY_ENDPOINT is required}
    : "${LOREHUB_LORE_INTERNAL_OBSERVATION_ENDPOINT:?required}"
    internal_observation_endpoint=$LOREHUB_LORE_INTERNAL_OBSERVATION_ENDPOINT
    lore_environment=production
    config=/etc/lore/config/production.toml
    for endpoint in "$jwks_endpoint" "$policy_endpoint" "$observation_endpoint" \
      "$internal_jwks_endpoint" "$internal_policy_endpoint" "$internal_observation_endpoint"; do
      case "$endpoint" in
        https://*) ;;
        *) echo "Lore production endpoints must use HTTPS" >&2; exit 1 ;;
      esac
    done
    ;;
  development|local-insecure)
    root_domain=${LOREHUB_LORE_ROOT_DOMAIN:-lorehub.localhost}
    auth_issuer=${LOREHUB_LORE_AUTH_ISSUER:-auth.${root_domain}}
    auth_audience=${LOREHUB_LORE_AUTH_AUDIENCE:-$root_domain}
    jwks_endpoint=${LOREHUB_LORE_AUTH_JWKS_URL:-http://${root_domain}:8080/.well-known/jwks.json}
    auth_url=${LOREHUB_LORE_AUTH_URL:-ucs-auth://auth.${root_domain}:8443}
    internal_endpoint_base=https://${root_domain}:8444/internal/lore
    policy_endpoint=${LOREHUB_LORE_POLICY_ENDPOINT:-$internal_endpoint_base/policy}
    observation_endpoint=${LOREHUB_LORE_OBSERVATION_ENDPOINT:-$internal_endpoint_base/observation}
    internal_jwks_endpoint=${LOREHUB_LORE_INTERNAL_AUTH_JWKS_URL:-http://${root_domain}:8080/.well-known/jwks.json}
    internal_auth_url=${LOREHUB_LORE_INTERNAL_AUTH_URL:-https://api.${root_domain}:8443}
    internal_policy_endpoint=${LOREHUB_LORE_INTERNAL_POLICY_ENDPOINT:-$internal_endpoint_base/policy}
    internal_observation_endpoint=${LOREHUB_LORE_INTERNAL_OBSERVATION_ENDPOINT:-$internal_endpoint_base/observation}
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

endpoint_authority() {
  endpoint_name=$1
  endpoint_value=$2
  endpoint_authority_value=$endpoint_value
  case "$endpoint_authority_value" in
    *:*) endpoint_authority_value=${endpoint_authority_value%:*} ;;
  esac
  test -n "$endpoint_authority_value" || {
    echo "$endpoint_name must contain a managed host" >&2
    exit 1
  }
  case "$endpoint_authority_value" in
    "$root_domain"|*."$root_domain") ;;
    *) echo "$endpoint_name must use the configured root domain" >&2; exit 1 ;;
  esac
}

validate_port() {
  endpoint_name=$1
  endpoint_port_value=$2
  case "$endpoint_port_value" in
    ''|*[!0-9]*) echo "$endpoint_name has an invalid port" >&2; exit 1 ;;
  esac
  if [ "$endpoint_port_value" -lt 1 ] || [ "$endpoint_port_value" -gt 65535 ]; then
    echo "$endpoint_name has an invalid port" >&2
    exit 1
  fi
}

validate_ucs_endpoint() {
  endpoint_name=$1
  endpoint_value=$2
  case "$endpoint_value" in
    ucs-auth://*) endpoint_rest=${endpoint_value#ucs-auth://} ;;
    *) echo "$endpoint_name must use ucs-auth://" >&2; exit 1 ;;
  esac
  case "$endpoint_rest" in
    ''|*[/?#@]*) echo "$endpoint_name must be a fixed authority" >&2; exit 1 ;;
  esac
  case "$endpoint_rest" in
    *:*)
      endpoint_host=${endpoint_rest%:*}
      endpoint_port=${endpoint_rest##*:}
      validate_port "$endpoint_name" "$endpoint_port"
      ;;
    *) endpoint_host=$endpoint_rest ;;
  esac
  endpoint_authority "$endpoint_name" "$endpoint_host"
}

validate_internal_auth_endpoint() {
  endpoint_name=$1
  endpoint_value=$2
  case "$endpoint_value" in
    https://*) endpoint_rest=${endpoint_value#https://} ;;
    *) echo "$endpoint_name must use HTTPS" >&2; exit 1 ;;
  esac
  case "$endpoint_rest" in
    ''|*[/?#@]*) echo "$endpoint_name must be a fixed HTTPS authority" >&2; exit 1 ;;
  esac
  case "$endpoint_rest" in
    *:*)
      endpoint_host=${endpoint_rest%:*}
      endpoint_port=${endpoint_rest##*:}
      validate_port "$endpoint_name" "$endpoint_port"
      ;;
    *) endpoint_host=$endpoint_rest ;;
  esac
  endpoint_authority "$endpoint_name" "$endpoint_host"
}

validate_http_endpoint() {
  endpoint_name=$1
  endpoint_value=$2
  expected_path=$3
  case "$endpoint_value" in
    https://*) endpoint_rest=${endpoint_value#https://} ;;
    http://*)
      endpoint_rest=${endpoint_value#http://}
      test "$environment" != production || {
        echo "$endpoint_name must use HTTPS in production" >&2
        exit 1
      }
      ;;
    *) echo "$endpoint_name must use HTTP or HTTPS" >&2; exit 1 ;;
  esac
  case "$endpoint_rest" in
    */*) endpoint_authority_part=${endpoint_rest%%/*}; endpoint_path=/${endpoint_rest#*/} ;;
    *) echo "$endpoint_name must contain the fixed path $expected_path" >&2; exit 1 ;;
  esac
  case "$endpoint_authority_part" in *[?#@]*) echo "$endpoint_name has invalid authority" >&2; exit 1 ;; esac
  case "$endpoint_authority_part" in
    *:*) endpoint_host=${endpoint_authority_part%:*}; endpoint_port=${endpoint_authority_part##*:}
      validate_port "$endpoint_name" "$endpoint_port"
      ;;
    *) endpoint_host=$endpoint_authority_part ;;
  esac
  test "$endpoint_path" = "$expected_path" || {
    echo "$endpoint_name must use the fixed path $expected_path" >&2
    exit 1
  }
  endpoint_authority "$endpoint_name" "$endpoint_host"
}

validate_http_endpoint LOREHUB_LORE_AUTH_JWKS_URL "$jwks_endpoint" \
  "/.well-known/jwks.json"
validate_http_endpoint LOREHUB_LORE_POLICY_ENDPOINT "$policy_endpoint" \
  "/internal/lore/policy"
validate_http_endpoint LOREHUB_LORE_OBSERVATION_ENDPOINT "$observation_endpoint" \
  "/internal/lore/observation"
validate_http_endpoint LOREHUB_LORE_INTERNAL_AUTH_JWKS_URL "$internal_jwks_endpoint" \
  "/.well-known/jwks.json"
validate_http_endpoint LOREHUB_LORE_INTERNAL_POLICY_ENDPOINT "$internal_policy_endpoint" \
  "/internal/lore/policy"
validate_http_endpoint LOREHUB_LORE_INTERNAL_OBSERVATION_ENDPOINT "$internal_observation_endpoint" \
  "/internal/lore/observation"
validate_ucs_endpoint LOREHUB_LORE_AUTH_URL "$auth_url"
validate_internal_auth_endpoint LOREHUB_LORE_INTERNAL_AUTH_URL "$internal_auth_url"

case "$auth_url" in
  ucs-auth://*) auth_authority=${auth_url#ucs-auth://} ;;
  *) echo "LOREHUB_LORE_AUTH_URL must use ucs-auth://" >&2; exit 1 ;;
esac
case "$auth_authority" in
  *:*) auth_proxy_port=${auth_authority##*:} ;;
  *) auth_proxy_port=443 ;;
esac
case "$auth_proxy_port" in
  ''|*[!0-9]*) echo "LOREHUB_LORE_AUTH_URL has an invalid port" >&2; exit 1 ;;
esac
validate_port LOREHUB_LORE_AUTH_URL "$auth_proxy_port"
case "$internal_auth_url" in
  https://*) internal_auth_authority=${internal_auth_url#https://} ;;
  *) echo "LOREHUB_LORE_INTERNAL_AUTH_URL must use HTTPS" >&2; exit 1 ;;
esac
case "$internal_auth_authority" in
  *:*)
    internal_auth_host=${internal_auth_authority%:*}
    internal_auth_port=${internal_auth_authority##*:}
    ;;
  *)
    internal_auth_host=$internal_auth_authority
    internal_auth_port=443
    ;;
esac
command -v socat >/dev/null 2>&1 || {
  echo "socat is required for the Lore internal UCS TLS bridge" >&2
  exit 1
}

# Lore 0.8.6's server-side UCS helper constructs a plaintext tonic endpoint
# from environment.endpoint.auth_url, while stock clients convert ucs-auth://
# to HTTPS. Keep the public URL and token-store key unchanged and bridge only
# the server-local connection to the configured internal HTTPS authority.
auth_host=${auth_authority%:*}
if [ "$auth_host" = "$auth_authority" ]; then
  auth_proxy_port=443
fi
if ! grep -Eq "^[[:space:]]*127\\.0\\.0\\.1[[:space:]].*([[:space:]]|^)${auth_host}([[:space:]]|$)" /etc/hosts; then
  printf '127.0.0.1 %s\n' "$auth_host" >> /etc/hosts
fi
openssl_target="OPENSSL:${internal_auth_host}:${internal_auth_port}"
openssl_options="cafile=/etc/lore/auth-tls/ca.crt,verify=1,commonname=${internal_auth_host}"
socat "TCP-LISTEN:${auth_proxy_port},bind=127.0.0.1,reuseaddr,fork" \
  "${openssl_target},${openssl_options}" &
auth_proxy_pid=$!
trap 'kill "$auth_proxy_pid" 2>/dev/null || true' EXIT INT TERM

# Lore 0.8.6 layers environment and local files after default.toml. Rendering a
# single selected file keeps local values from ever leaking into production.
config_dir=/run/lore-config
mkdir -p "$config_dir"
sed \
  -e "s|@ROOT_DOMAIN@|$root_domain|g" \
  -e "s|@AUTH_ISSUER@|$auth_issuer|g" \
  -e "s|@AUTH_AUDIENCE@|$auth_audience|g" \
  -e "s|@PUBLIC_JWKS_ENDPOINT@|$jwks_endpoint|g" \
  -e "s|@PUBLIC_AUTH_URL@|$auth_url|g" \
  -e "s|@PUBLIC_POLICY_ENDPOINT@|$policy_endpoint|g" \
  -e "s|@PUBLIC_OBSERVATION_ENDPOINT@|$observation_endpoint|g" \
  -e "s|@INTERNAL_JWKS_ENDPOINT@|$internal_jwks_endpoint|g" \
  -e "s|@INTERNAL_AUTH_URL@|$internal_auth_url|g" \
  -e "s|@INTERNAL_POLICY_ENDPOINT@|$internal_policy_endpoint|g" \
  -e "s|@INTERNAL_OBSERVATION_ENDPOINT@|$internal_observation_endpoint|g" \
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
require_setting '^[[:space:]]*auth_endpoint[[:space:]]*=[[:space:]]*"https://[^"[:space:]]+"' \
  'hooks.lorehub_policy.auth_endpoint'
require_setting '^[[:space:]]*jwks_endpoint[[:space:]]*=[[:space:]]*"https?://[^"[:space:]]+"' \
  'hooks.lorehub_policy.jwks_endpoint'
require_setting '^[[:space:]]*endpoint[[:space:]]*=[[:space:]]*"https?://[^"[:space:]]+"' \
  'hooks.lorehub_policy.endpoint'
require_setting '^[[:space:]]*observation_endpoint[[:space:]]*=[[:space:]]*"https?://[^"[:space:]]+"' \
  'hooks.lorehub_policy.observation_endpoint'
require_setting '^[[:space:]]*auth_url[[:space:]]*=[[:space:]]*"ucs-auth://[^"[:space:]]+"' \
  'environment.endpoint.auth_url'

loreserver --config "$config_dir" --env "$lore_environment" &
lore_pid=$!
trap 'kill "$lore_pid" "$auth_proxy_pid" 2>/dev/null || true' EXIT INT TERM
wait "$lore_pid"
