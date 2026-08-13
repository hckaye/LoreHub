#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repository_root=$(dirname -- "$script_dir")
compose_file="$repository_root/infra/compose.yaml"
env_file="$repository_root/.env"
project_name=${COMPOSE_PROJECT_NAME:-lorehub}
output_path=
postgres_image=postgres:18.4-alpine
volume_names="lore-data api-data auth-signing auth-tls auth-ca-state runner-data keycloak-data"

usage() {
  echo "usage: scripts/backup.sh [--env-file PATH] [--output DIRECTORY] [--project-name NAME]" >&2
}

fail() {
  echo "backup: $*" >&2
  exit 1
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      env_file=$2
      shift 2
      ;;
    --output)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      output_path=$2
      shift 2
      ;;
    --project-name)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      project_name=$2
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage
      exit 2
      ;;
  esac
done

case "$project_name" in
  ""|*[!a-zA-Z0-9_-]*) fail "project name may contain only letters, numbers, underscores, and hyphens" ;;
esac

[ -f "$env_file" ] || fail "environment file not found: $env_file"
command -v docker >/dev/null 2>&1 || fail "docker is required"

if [ -z "$output_path" ]; then
  output_path="$repository_root/backups/lorehub-$(date -u +%Y%m%dT%H%M%SZ)"
fi
case "$output_path" in
  /*) ;;
  *) output_path="$(pwd)/$output_path" ;;
esac
[ ! -e "$output_path" ] || fail "output already exists: $output_path"

compose() {
  docker compose --project-name "$project_name" --env-file "$env_file" -f "$compose_file" "$@"
}

compose config -q
running_services=$(compose ps --services --filter status=running)
for required_service in postgres keycloak-postgres; do
  printf '%s\n' "$running_services" | grep -qx "$required_service" ||
    fail "$required_service must be running"
done
for initialization_service in tls-init keycloak-bootstrap runner-data-init runner-cert-init; do
  if printf '%s\n' "$running_services" | grep -qx "$initialization_service"; then
    fail "$initialization_service is still running; wait for initialization to finish"
  fi
done

active_writers=
for service in web api lore keycloak runner; do
  if printf '%s\n' "$running_services" | grep -qx "$service"; then
    active_writers="$active_writers $service"
  fi
done

writers_stopped=false
restart_writers() {
  if [ "$writers_stopped" = true ] && [ -n "$active_writers" ]; then
    # active_writers contains only service names from the fixed list above.
    # shellcheck disable=SC2086
    compose start $active_writers >/dev/null
  fi
}
trap restart_writers EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

mkdir -p "$output_path/volumes"
chmod 700 "$output_path" "$output_path/volumes"

if [ -n "$active_writers" ]; then
  # active_writers contains only service names from the fixed list above.
  # shellcheck disable=SC2086
  compose stop $active_writers >/dev/null
  writers_stopped=true
fi

# POSTGRES_USER and POSTGRES_DB expand inside the container.
# shellcheck disable=SC2016
compose exec -T postgres sh -c \
  'exec pg_dump --format=custom --no-owner --no-acl -U "$POSTGRES_USER" -d "$POSTGRES_DB"' \
  >"$output_path/lorehub.pgdump"
# shellcheck disable=SC2016
compose exec -T keycloak-postgres sh -c \
  'exec pg_dump --format=custom --no-owner --no-acl -U "$POSTGRES_USER" -d "$POSTGRES_DB"' \
  >"$output_path/keycloak.pgdump"

for volume_name in $volume_names; do
  docker volume inspect "${project_name}_${volume_name}" >/dev/null 2>&1 ||
    fail "Docker volume not found: ${project_name}_${volume_name}"
  docker run --rm \
    --volume "${project_name}_${volume_name}:/source:ro" \
    --volume "$output_path/volumes:/backup" \
    "$postgres_image" \
    sh -c 'cd /source && exec tar -czf "/backup/$1.tar.gz" .' sh "$volume_name"
done

cp "$env_file" "$output_path/environment.env"
chmod 600 "$output_path/lorehub.pgdump" "$output_path/keycloak.pgdump" \
  "$output_path/environment.env" "$output_path"/volumes/*.tar.gz

created_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
source_commit=$(git -C "$repository_root" rev-parse --verify HEAD 2>/dev/null || echo unknown)
{
  echo "format_version=1"
  echo "status=complete"
  echo "created_at=$created_at"
  echo "compose_project=$project_name"
  echo "source_commit=$source_commit"
  echo "postgres_image=$postgres_image"
  echo "volumes=$volume_names"
} >"$output_path/manifest.txt"
chmod 600 "$output_path/manifest.txt"

restart_writers
writers_stopped=false
trap - EXIT INT TERM

echo "Backup completed: $output_path"
