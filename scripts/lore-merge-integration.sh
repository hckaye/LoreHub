#!/bin/sh
set -eu

# This fixture is an unauthenticated Lore component integration. It validates
# SDK tree/history/diff and merge behavior, not production auth or hook wiring.
root_dir=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
compose_file=$root_dir/infra/lore/integration-compose.yaml
project="lorehub-lore-integration-$(date -u +%Y%m%d%H%M%S)-$$"
repository_id=$(openssl rand -hex 16)
export LORE_TEST_REPOSITORY_URL="lore://lore:41337/${repository_id}"
LORE_TEST_TLS_DIR=$(mktemp -d)
export LORE_TEST_TLS_DIR

openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
    -keyout "$LORE_TEST_TLS_DIR/server.key" \
    -out "$LORE_TEST_TLS_DIR/server.crt" \
    -subj "/CN=lore" \
    -addext "subjectAltName=DNS:lore,DNS:localhost,IP:127.0.0.1" >/dev/null 2>&1
chmod 0600 "$LORE_TEST_TLS_DIR/server.key"

cleanup() {
    docker compose -p "$project" -f "$compose_file" down --volumes --remove-orphans --rmi local
    rm -rf "$LORE_TEST_TLS_DIR"
}

trap cleanup EXIT INT TERM
docker compose -p "$project" -f "$compose_file" up \
    --build --abort-on-container-exit --exit-code-from lore-test
