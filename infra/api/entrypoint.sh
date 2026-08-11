#!/bin/sh
set -eu

if [ "${1:-serve}" = "runner" ]; then
  mkdir -p \
    "${LOREHUB_LORE_CACHE_DIR:?LOREHUB_LORE_CACHE_DIR is required}" \
    "${LOREHUB_RUNNER_LOG_DIR:?LOREHUB_RUNNER_LOG_DIR is required}" \
    "${LOREHUB_RUNNER_ARTIFACT_DIR:?LOREHUB_RUNNER_ARTIFACT_DIR is required}" \
    "${LOREHUB_RUNNER_WORK_DIR:?LOREHUB_RUNNER_WORK_DIR is required}"
  exec /usr/local/bin/lorehub "$@"
fi

if [ -s /var/lib/lorehub/tls/ca.crt ]; then
  cp /var/lib/lorehub/tls/ca.crt /usr/local/share/ca-certificates/lorehub-local.crt
  update-ca-certificates >/dev/null
fi

mkdir -p /var/lib/lorehub/keys
mkdir -p /var/lib/lorehub/repositories /var/lib/lorehub/runner-logs
mkdir -p /var/lib/lorehub/runner-artifacts /var/lib/lorehub/runner-work
chown lorehub:lorehub /var/lib/lorehub
chown -R lorehub:lorehub /var/lib/lorehub/keys /var/lib/lorehub/repositories
chown -R lorehub:lorehub /var/lib/lorehub/runner-logs /var/lib/lorehub/runner-artifacts
chown -R lorehub:lorehub /var/lib/lorehub/runner-work
chmod 0700 /var/lib/lorehub/keys

exec runuser -u lorehub -- /usr/local/bin/lorehub "$@"
