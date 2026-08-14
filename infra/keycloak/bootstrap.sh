#!/bin/bash
# Idempotently configure the LoreHub realm, web client, and social providers.
# Credentials are read only from the environment and are never printed.
set -euo pipefail

KCADM="${KCADM:-/opt/keycloak/bin/kcadm.sh}"
REALM="${LOREHUB_REALM:-lorehub}"
SERVER="${KEYCLOAK_URL:-http://keycloak:8080}"
ENVIRONMENT="${LOREHUB_ENV:-development}"
VERIFY_EMAIL="${LOREHUB_VERIFY_EMAIL:-false}"
CLIENT_ID="${LOREHUB_OIDC_CLIENT_ID:-lorehub-web}"
CLIENT_SECRET="${LOREHUB_OIDC_CLIENT_SECRET:-}"
PUBLIC_ORIGIN="${LOREHUB_PUBLIC_ORIGIN:-http://localhost:3000}"
REDIRECT_URL="${LOREHUB_OIDC_REDIRECT_URL:-${PUBLIC_ORIGIN}/auth/callback}"
LOGOUT_URL="${LOREHUB_OIDC_LOGOUT_REDIRECT_URL:-${PUBLIC_ORIGIN}/}"
SMTP_HOST="${KEYCLOAK_SMTP_HOST:-}"
SMTP_PORT="${KEYCLOAK_SMTP_PORT:-587}"
SMTP_FROM="${KEYCLOAK_SMTP_FROM:-}"
SMTP_FROM_DISPLAY_NAME="${KEYCLOAK_SMTP_FROM_DISPLAY_NAME:-LoreHub}"
SMTP_REPLY_TO="${KEYCLOAK_SMTP_REPLY_TO:-}"
SMTP_REPLY_TO_DISPLAY_NAME="${KEYCLOAK_SMTP_REPLY_TO_DISPLAY_NAME:-LoreHub}"
SMTP_AUTH="${KEYCLOAK_SMTP_AUTH:-false}"
SMTP_USER="${KEYCLOAK_SMTP_USER:-}"
SMTP_PASSWORD="${KEYCLOAK_SMTP_PASSWORD:-}"
SMTP_STARTTLS="${KEYCLOAK_SMTP_STARTTLS:-false}"
SMTP_SSL="${KEYCLOAK_SMTP_SSL:-false}"

PASSWORD_POLICY="length(12) and specialChars and digits and upperCase and lowerCase"
PASSWORD_POLICY="${PASSWORD_POLICY} and notUsername and notEmail and passwordHistory(3)"

fail() {
  echo "[bootstrap] $1" >&2
  exit 1
}

require_value() {
  if [ -z "$2" ]; then
    fail "$1 is required"
  fi
}

validate_boolean() {
  case "$2" in
    true | false) ;;
    *) fail "$1 must be true or false" ;;
  esac
}

require_value "LOREHUB_OIDC_CLIENT_SECRET" "$CLIENT_SECRET"
if [ "$CLIENT_ID" != "lorehub-web" ]; then
  fail "LOREHUB_OIDC_CLIENT_ID must be lorehub-web"
fi
case "$VERIFY_EMAIL" in
  true | false) ;;
  *) fail "LOREHUB_VERIFY_EMAIL must be true or false" ;;
esac
if [ "$ENVIRONMENT" = "production" ] && [ "$VERIFY_EMAIL" != "true" ]; then
  fail "production requires LOREHUB_VERIFY_EMAIL=true"
fi
case "$PUBLIC_ORIGIN" in
  http://* | https://*) ;;
  *) fail "LOREHUB_PUBLIC_ORIGIN must be an HTTP or HTTPS URL" ;;
esac
case "$REDIRECT_URL" in
  */auth/callback) ;;
  *) fail "LOREHUB_OIDC_REDIRECT_URL must end with /auth/callback" ;;
esac
validate_boolean "KEYCLOAK_SMTP_AUTH" "$SMTP_AUTH"
validate_boolean "KEYCLOAK_SMTP_STARTTLS" "$SMTP_STARTTLS"
validate_boolean "KEYCLOAK_SMTP_SSL" "$SMTP_SSL"

if [ "$VERIFY_EMAIL" = "true" ]; then
  require_value "KEYCLOAK_SMTP_HOST" "$SMTP_HOST"
  require_value "KEYCLOAK_SMTP_PORT" "$SMTP_PORT"
  require_value "KEYCLOAK_SMTP_FROM" "$SMTP_FROM"
  if [ "$SMTP_AUTH" = "true" ]; then
    require_value "KEYCLOAK_SMTP_USER" "$SMTP_USER"
    require_value "KEYCLOAK_SMTP_PASSWORD" "$SMTP_PASSWORD"
  fi
  if [ "$SMTP_STARTTLS" = "true" ] && [ "$SMTP_SSL" = "true" ]; then
    fail "only one of KEYCLOAK_SMTP_STARTTLS and KEYCLOAK_SMTP_SSL may be true"
  fi
  if [ "$SMTP_STARTTLS" != "true" ] && [ "$SMTP_SSL" != "true" ]; then
    fail "production SMTP must enable STARTTLS or SSL"
  fi
fi

json_quote() {
  local value=$1
  value=${value//\\\\/\\\\\\\\}
  value=${value//\"/\\\\\"}
  value=${value//$'\\n'/\\\\n}
  value=${value//$'\\r'/\\\\r}
  value=${value//$'\\t'/\\\\t}
  printf '"%s"' "$value"
}

smtp_json() {
  printf '{"host":%s,"port":%s,"from":%s,"fromDisplayName":%s,' \
    "$(json_quote "$SMTP_HOST")" "$(json_quote "$SMTP_PORT")" \
    "$(json_quote "$SMTP_FROM")" "$(json_quote "$SMTP_FROM_DISPLAY_NAME")"
  printf '"replyTo":%s,"replyToDisplayName":%s,"auth":%s,' \
    "$(json_quote "$SMTP_REPLY_TO")" "$(json_quote "$SMTP_REPLY_TO_DISPLAY_NAME")" \
    "$(json_quote "$SMTP_AUTH")"
  printf '"user":%s,"password":%s,"starttls":%s,"ssl":%s}' \
    "$(json_quote "$SMTP_USER")" "$(json_quote "$SMTP_PASSWORD")" \
    "$(json_quote "$SMTP_STARTTLS")" "$(json_quote "$SMTP_SSL")"
}

echo "[bootstrap] waiting for Keycloak Admin API at ${SERVER}"
attempt=0
until "$KCADM" config credentials \
  --server "$SERVER" \
  --realm master \
  --user "${KEYCLOAK_ADMIN_USERNAME}" \
  --password "${KEYCLOAK_ADMIN_PASSWORD}" >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 60 ]; then
    fail "could not authenticate to Keycloak after 60 attempts"
  fi
  sleep 2
done
echo "[bootstrap] authenticated; configuring realm ${REALM}"

ssl_required="NONE"
if [ "$ENVIRONMENT" = "production" ]; then
  ssl_required="EXTERNAL"
fi
realm_args=(
  -s "verifyEmail=${VERIFY_EMAIL}"
  -s "passwordPolicy=${PASSWORD_POLICY}"
  -s "sslRequired=${ssl_required}"
)
if [ "$VERIFY_EMAIL" = "true" ]; then
  realm_args+=(-s "smtpServer=$(smtp_json)")
fi
"$KCADM" update "realms/${REALM}" -r "$REALM" "${realm_args[@]}" >/dev/null

basic_scope_id=$(
  "$KCADM" get client-scopes -r "$REALM" --fields id,name 2>/dev/null |
    sed -n '
      /"id"[[:space:]]*:/ {
        s/.*"id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/
        h
      }
      /"name"[[:space:]]*:[[:space:]]*"basic"/ {
        g
        p
        q
      }
    '
)
if [ -z "$basic_scope_id" ]; then
  echo "[bootstrap] creating basic OIDC client scope"
  basic_scope_id=$(
    "$KCADM" create client-scopes -r "$REALM" -i \
      -s "name=basic" \
      -s "description=OpenID Connect subject claim scope" \
      -s "protocol=openid-connect" \
      -s 'attributes."include.in.token.scope"=false' \
      -s 'attributes."display.on.consent.screen"=false'
  )
fi
if ! "$KCADM" get "client-scopes/${basic_scope_id}/protocol-mappers/models" -r "$REALM" |
  grep -q '"protocolMapper" : "oidc-sub-mapper"'; then
  echo "[bootstrap] adding subject mapper to basic OIDC client scope"
  "$KCADM" create "client-scopes/${basic_scope_id}/protocol-mappers/models" -r "$REALM" \
    -s "name=sub" \
    -s "protocol=openid-connect" \
    -s "protocolMapper=oidc-sub-mapper" \
    -s "consentRequired=false" \
    -s 'config."introspection.token.claim"=true' \
    -s 'config."access.token.claim"=true' >/dev/null
fi

client_uuid=$(
  "$KCADM" get clients -r "$REALM" -q "clientId=${CLIENT_ID}" --fields id 2>/dev/null |
    sed -n 's/.*"id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
    head -n 1
)
if [ -z "$client_uuid" ]; then
  fail "OIDC client ${CLIENT_ID} is missing from realm ${REALM}"
fi

client_args=(
  -s "clientId=${CLIENT_ID}"
  -s "enabled=true"
  -s "protocol=openid-connect"
  -s "publicClient=false"
  -s "bearerOnly=false"
  -s "standardFlowEnabled=true"
  -s "implicitFlowEnabled=false"
  -s "directAccessGrantsEnabled=false"
  -s "serviceAccountsEnabled=false"
  -s "rootUrl=${PUBLIC_ORIGIN}"
  -s "baseUrl=${PUBLIC_ORIGIN}"
  -s "redirectUris=[\"${REDIRECT_URL}\"]"
  -s "webOrigins=[\"${PUBLIC_ORIGIN}\"]"
  -s "attributes.pkceCodeChallengeMethod=S256"
  -s "attributes.\"post.logout.redirect.uris\"=${LOGOUT_URL}"
  -s "secret=${CLIENT_SECRET}"
)
echo "[bootstrap] updating OIDC client ${CLIENT_ID}"
"$KCADM" update "clients/${client_uuid}" -r "$REALM" "${client_args[@]}" >/dev/null
"$KCADM" update "clients/${client_uuid}/default-client-scopes/${basic_scope_id}" -r "$REALM" >/dev/null

provider_exists() {
  "$KCADM" get "identity-provider/instances/$1" -r "$REALM" >/dev/null 2>&1
}

upsert_provider() {
  local alias=$1
  local provider_id=$2
  shift 2
  local common=(-s "providerId=${provider_id}" -s "enabled=true" -s "trustEmail=false")
  common+=(-s "storeToken=false" -s "authenticateByDefault=false")
  if provider_exists "$alias"; then
    echo "[bootstrap] updating provider ${alias}"
    "$KCADM" update "identity-provider/instances/${alias}" -r "$REALM" \
      "${common[@]}" "$@" >/dev/null
  else
    echo "[bootstrap] creating provider ${alias}"
    "$KCADM" create identity-provider/instances -r "$REALM" \
      -s "alias=${alias}" "${common[@]}" "$@" >/dev/null
  fi
}

disable_provider() {
  local alias=$1
  if provider_exists "$alias"; then
    echo "[bootstrap] disabling provider ${alias}; credentials are absent"
    "$KCADM" update "identity-provider/instances/${alias}" -r "$REALM" \
      -s "enabled=false" -s "trustEmail=false" -s "storeToken=false" \
      -s "authenticateByDefault=false" >/dev/null
  else
    echo "[bootstrap] provider ${alias} is not configured; skipping"
  fi
}

idp_mapper_id() {
  local alias=$1
  local name=$2
  "$KCADM" get "identity-provider/instances/${alias}/mappers" -r "$REALM" \
    --fields name,id --format csv --noquotes 2>/dev/null \
    | awk -F, -v name="$name" '$1 == name { print $2; exit }'
}

upsert_idp_mapper() {
  local alias=$1
  local name=$2
  shift 2
  if ! provider_exists "$alias"; then
    return 0
  fi
  local mapper_id
  mapper_id=$(idp_mapper_id "$alias" "$name")
  if [ -n "$mapper_id" ]; then
    echo "[bootstrap] updating ${alias} mapper ${name}"
    "$KCADM" update "identity-provider/instances/${alias}/mappers/${mapper_id}" -r "$REALM" \
      -s "name=${name}" -s "identityProviderAlias=${alias}" "$@" >/dev/null
  else
    echo "[bootstrap] creating ${alias} mapper ${name}"
    "$KCADM" create "identity-provider/instances/${alias}/mappers" -r "$REALM" \
      -s "name=${name}" -s "identityProviderAlias=${alias}" "$@" >/dev/null
  fi
}

upsert_picture_mapper() {
  local alias=$1
  local mapper_type=$2
  local source_field=$3
  if [ "$mapper_type" = "oidc-user-attribute-idp-mapper" ]; then
    upsert_idp_mapper "$alias" picture \
      -s "identityProviderMapper=${mapper_type}" \
      -s "config.syncMode=INHERIT" \
      -s "config.claim=${source_field}" \
      -s "config.\"user.attribute\"=picture"
    return
  fi
  upsert_idp_mapper "$alias" picture \
    -s "identityProviderMapper=${mapper_type}" \
    -s "config.syncMode=INHERIT" \
    -s "config.jsonField=${source_field}" \
    -s "config.userAttribute=picture"
}

if [ -n "${LOREHUB_IDP_GOOGLE_CLIENT_ID:-}" ] && [ -n "${LOREHUB_IDP_GOOGLE_CLIENT_SECRET:-}" ]; then
  upsert_provider google google \
    -s "config.clientId=${LOREHUB_IDP_GOOGLE_CLIENT_ID}" \
    -s "config.clientSecret=${LOREHUB_IDP_GOOGLE_CLIENT_SECRET}" \
    -s "config.defaultScope=openid email profile" \
    -s "config.hostedDomain=${LOREHUB_IDP_GOOGLE_HOSTED_DOMAIN:-}"
  upsert_picture_mapper google oidc-user-attribute-idp-mapper picture
else
  disable_provider google
fi

if [ -n "${LOREHUB_IDP_GITHUB_CLIENT_ID:-}" ] && [ -n "${LOREHUB_IDP_GITHUB_CLIENT_SECRET:-}" ]; then
  upsert_provider github github \
    -s "config.clientId=${LOREHUB_IDP_GITHUB_CLIENT_ID}" \
    -s "config.clientSecret=${LOREHUB_IDP_GITHUB_CLIENT_SECRET}" \
    -s "config.defaultScope=user:email"
  upsert_picture_mapper github github-user-attribute-mapper avatar_url
else
  disable_provider github
fi

if [ -n "${LOREHUB_IDP_FACEBOOK_CLIENT_ID:-}" ] && [ -n "${LOREHUB_IDP_FACEBOOK_CLIENT_SECRET:-}" ]; then
  upsert_provider facebook facebook \
    -s "config.clientId=${LOREHUB_IDP_FACEBOOK_CLIENT_ID}" \
    -s "config.clientSecret=${LOREHUB_IDP_FACEBOOK_CLIENT_SECRET}" \
    -s "config.defaultScope=email"
  upsert_picture_mapper facebook facebook-user-attribute-mapper picture.data.url
else
  disable_provider facebook
fi

if [ -n "${LOREHUB_IDP_X_CLIENT_ID:-}" ] && [ -n "${LOREHUB_IDP_X_CLIENT_SECRET:-}" ]; then
  upsert_provider x oauth2 \
    -s "config.authorizationUrl=https://x.com/i/oauth2/authorize" \
    -s "config.tokenUrl=https://api.x.com/2/oauth2/token" \
    -s "config.userInfoUrl=https://api.x.com/2/users/me?user.fields=confirmed_email,profile_image_url" \
    -s "config.clientId=${LOREHUB_IDP_X_CLIENT_ID}" \
    -s "config.clientSecret=${LOREHUB_IDP_X_CLIENT_SECRET}" \
    -s "config.clientAuthMethod=client_secret_basic" \
    -s "config.defaultScope=users.read users.email" \
    -s "config.pkceEnabled=true" \
    -s "config.pkceMethod=S256" \
    -s "config.userIDClaim=data.id" \
    -s "config.userNameClaim=data.username" \
    -s "config.fullNameClaim=data.name" \
    -s "config.emailClaim=data.confirmed_email"
  upsert_picture_mapper x oidc-user-attribute-idp-mapper data.profile_image_url
else
  disable_provider x
fi

echo "[bootstrap] done"
