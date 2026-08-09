#!/bin/bash
# Idempotent provisioning of social identity providers for the LoreHub realm.
#
# A provider is created only when both its client ID and client secret are
# present in the environment. Providers with missing credentials are skipped so
# they never appear as broken login buttons. Re-runs update existing providers
# in place, so this script is safe to run on every `docker compose up`.
#
# Secrets are read from the environment and sent over the internal Docker
# network to the Admin CLI; they are never printed or logged.
set -euo pipefail

KCADM="/opt/keycloak/bin/kcadm.sh"
REALM="${LOREHUB_REALM:-lorehub}"
SERVER="${KEYCLOAK_URL:-http://keycloak:8080}"

echo "[bootstrap] waiting for Keycloak Admin API at ${SERVER}"
attempt=0
until "${KCADM}" config credentials \
  --server "${SERVER}" \
  --realm master \
  --user "${KEYCLOAK_ADMIN_USERNAME}" \
  --password "${KEYCLOAK_ADMIN_PASSWORD}" >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  if [ "${attempt}" -ge 60 ]; then
    echo "[bootstrap] could not authenticate to Keycloak after 60 attempts" >&2
    exit 1
  fi
  sleep 2
done
echo "[bootstrap] authenticated; configuring realm ${REALM}"

# Returns 0 if an identity provider with the given alias already exists.
provider_exists() {
  "${KCADM}" get "identity-provider/instances/$1" -r "${REALM}" >/dev/null 2>&1
}

# Create or update an identity provider. $1=alias, $2=providerId, remaining
# args are `-s key=value` config pairs passed through to kcadm.
upsert_provider() {
  local alias="$1"
  local provider_id="$2"
  shift 2
  if provider_exists "${alias}"; then
    echo "[bootstrap] updating existing provider: ${alias}"
    "${KCADM}" update "identity-provider/instances/${alias}" -r "${REALM}" "$@"
  else
    echo "[bootstrap] creating provider: ${alias} (${provider_id})"
    "${KCADM}" create identity-provider/instances -r "${REALM}" \
      -s "providerId=${provider_id}" \
      -s "alias=${alias}" \
      -s "enabled=true" \
      -s "trustEmail=false" \
      -s "storeToken=true" \
      -s "authenticateByDefault=false" \
      "$@"
  fi
}

# Google (built-in OIDC social provider)
if [ -n "${LOREHUB_IDP_GOOGLE_CLIENT_ID:-}" ] && [ -n "${LOREHUB_IDP_GOOGLE_CLIENT_SECRET:-}" ]; then
  upsert_provider google google \
    -s "config.clientId=${LOREHUB_IDP_GOOGLE_CLIENT_ID}" \
    -s "config.clientSecret=${LOREHUB_IDP_GOOGLE_CLIENT_SECRET}" \
    -s "config.defaultScope=openid email profile" \
    ${LOREHUB_IDP_GOOGLE_HOSTED_DOMAIN:+-s "config.hostedDomain=${LOREHUB_IDP_GOOGLE_HOSTED_DOMAIN}"}
else
  echo "[bootstrap] Google credentials absent; skipping"
fi

# GitHub (built-in OAuth2 social provider)
if [ -n "${LOREHUB_IDP_GITHUB_CLIENT_ID:-}" ] && [ -n "${LOREHUB_IDP_GITHUB_CLIENT_SECRET:-}" ]; then
  upsert_provider github github \
    -s "config.clientId=${LOREHUB_IDP_GITHUB_CLIENT_ID}" \
    -s "config.clientSecret=${LOREHUB_IDP_GITHUB_CLIENT_SECRET}" \
    -s "config.defaultScope=user:email"
else
  echo "[bootstrap] GitHub credentials absent; skipping"
fi

# Facebook (built-in OAuth2 social provider)
if [ -n "${LOREHUB_IDP_FACEBOOK_CLIENT_ID:-}" ] && [ -n "${LOREHUB_IDP_FACEBOOK_CLIENT_SECRET:-}" ]; then
  upsert_provider facebook facebook \
    -s "config.clientId=${LOREHUB_IDP_FACEBOOK_CLIENT_ID}" \
    -s "config.clientSecret=${LOREHUB_IDP_FACEBOOK_CLIENT_SECRET}" \
    -s "config.defaultScope=email"
else
  echo "[bootstrap] Facebook credentials absent; skipping"
fi

# X (formerly Twitter). The built-in Twitter broker is deprecated in Keycloak
# 26.7 (gated behind the twitter-broker feature flag, removed in 27.0) and uses
# legacy OAuth 1.0a. Per the latest Keycloak and X developer docs, X now supports
# OAuth 2.0, so we configure the generic OAuth v2 identity provider against X's
# real OAuth 2.0 endpoints. The /2/users/me response nests claims under "data",
# so claim names use dot-notation (supported by Keycloak's claim resolver).
if [ -n "${LOREHUB_IDP_X_CLIENT_ID:-}" ] && [ -n "${LOREHUB_IDP_X_CLIENT_SECRET:-}" ]; then
  upsert_provider x oauth2 \
    -s "config.authorizationUrl=https://twitter.com/i/oauth2/authorize" \
    -s "config.tokenUrl=https://api.twitter.com/2/oauth2/token" \
    -s "config.userInfoUrl=https://api.twitter.com/2/users/me" \
    -s "config.clientId=${LOREHUB_IDP_X_CLIENT_ID}" \
    -s "config.clientSecret=${LOREHUB_IDP_X_CLIENT_SECRET}" \
    -s "config.clientAuthMethod=client_secret_basic" \
    -s "config.defaultScope=tweet.read users.read offline.access" \
    -s "config.pkceEnabled=true" \
    -s "config.pkceMethod=S256" \
    -s "config.userIDClaim=data.id" \
    -s "config.userNameClaim=data.username" \
    -s "config.fullNameClaim=data.name"
else
  echo "[bootstrap] X credentials absent; skipping"
fi

echo "[bootstrap] done"
