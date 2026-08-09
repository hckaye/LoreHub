#!/bin/sh
set -eu

root_dir=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
compose_file=$root_dir/infra/lore/integration-compose.yaml
project="lorehub-lore-integration-$(date -u +%Y%m%d%H%M%S)-$$"
export LORE_TEST_REPOSITORY_URL="lore://lore:41337/${project}"

cleanup() {
    docker compose -p "$project" -f "$compose_file" down --volumes --remove-orphans --rmi local
}

trap cleanup EXIT INT TERM
docker compose -p "$project" -f "$compose_file" up \
    --build --abort-on-container-exit --exit-code-from lore-test
