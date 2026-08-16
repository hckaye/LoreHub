#!/usr/bin/env bash
set -Eeuo pipefail

# Usage: lorehub-redeploy [git-ref]
# Default ref is the repository_ref baked in at first boot.
#
# Named Docker volumes on this host hold Postgres, Keycloak, Lore data, and
# other application state. This script restarts lorehub.service, which runs
# `docker compose down` without --volumes and then `up --detach --build`.
# Never add --volumes to that stop path; it would delete application data.

LOREHUB_LOG_PREFIX=lorehub-redeploy
# shellcheck disable=SC1091
source /usr/local/lib/lorehub-node.sh

if [[ $# -gt 1 ]]; then
  printf 'usage: lorehub-redeploy [git-ref]\n' >&2
  exit 2
fi

ref=${1:-$REPOSITORY_REF}
[[ -n "$ref" ]] || fail "git ref is empty"
if [[ "$ref" == -* ]] || [[ "$ref" =~ [[:space:]] ]]; then
  fail "git ref must not contain whitespace or start with a dash"
fi

checkout_repository "$ref"
prepare_environment
systemctl restart lorehub.service
log "Redeploy of $ref is complete"
