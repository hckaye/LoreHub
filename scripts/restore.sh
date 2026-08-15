#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repository_root=$(dirname -- "$script_dir")
compose_file="$repository_root/infra/compose.yaml"
env_file="$repository_root/.env"
project_name=${COMPOSE_PROJECT_NAME:-lorehub}
confirmed_project=
backup_path=
restore_environment=false
start_services=false
runner_profile=false
postgres_image=postgres:18.4-alpine
supported_volume_names="lore-data api-data auth-signing auth-tls auth-ca-state runner-data keycloak-data"

usage() {
  cat >&2 <<'EOF'
usage: scripts/restore.sh --backup DIRECTORY --confirm-project NAME
       [--env-file PATH] [--project-name NAME] [--restore-environment]
       [--start] [--runner]
EOF
}

fail() {
  echo "restore: $*" >&2
  exit 1
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --backup)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      backup_path=$2
      shift 2
      ;;
    --confirm-project)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      confirmed_project=$2
      shift 2
      ;;
    --env-file)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      env_file=$2
      shift 2
      ;;
    --project-name)
      [ "$#" -ge 2 ] || { usage; exit 2; }
      project_name=$2
      shift 2
      ;;
    --restore-environment)
      restore_environment=true
      shift
      ;;
    --start)
      start_services=true
      shift
      ;;
    --runner)
      runner_profile=true
      shift
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
[ -n "$backup_path" ] || fail "--backup is required"
[ "$confirmed_project" = "$project_name" ] ||
  fail "--confirm-project must exactly match the target project: $project_name"

case "$backup_path" in
  /*) ;;
  *) backup_path="$(pwd)/$backup_path" ;;
esac
manifest="$backup_path/manifest.txt"
[ -f "$manifest" ] || fail "backup manifest not found: $manifest"

manifest_value() {
  sed -n "s/^$1=//p" "$manifest" | tail -n 1
}

[ "$(manifest_value format_version)" = 1 ] || fail "unsupported backup format"
[ "$(manifest_value status)" = complete ] || fail "backup is incomplete"
volume_names=$(manifest_value volumes)
[ -n "$volume_names" ] || fail "backup volume list is missing"
keycloak_included=false
for volume_name in $volume_names; do
  case " $supported_volume_names " in
    *" $volume_name "*) ;;
    *) fail "backup volume list is not supported: $volume_name" ;;
  esac
  [ "$volume_name" = keycloak-data ] && keycloak_included=true
done

required_files="lorehub.pgdump environment.env"
if [ "$keycloak_included" = true ]; then
  required_files="$required_files keycloak.pgdump"
fi
for required_file in $required_files; do
  [ -f "$backup_path/$required_file" ] || fail "backup file not found: $required_file"
done
for volume_name in $volume_names; do
  [ -f "$backup_path/volumes/$volume_name.tar.gz" ] ||
    fail "backup file not found: volumes/$volume_name.tar.gz"
done

command -v docker >/dev/null 2>&1 || fail "docker is required"

if [ "$restore_environment" = true ]; then
  environment_parent=$(dirname -- "$env_file")
  [ -d "$environment_parent" ] || fail "environment file directory not found: $environment_parent"
  environment_temp="$env_file.restore.$$"
  trap 'rm -f -- "$environment_temp"' EXIT INT TERM
  cp "$backup_path/environment.env" "$environment_temp"
  chmod 600 "$environment_temp"
  mv "$environment_temp" "$env_file"
  trap - EXIT INT TERM
fi
[ -f "$env_file" ] ||
  fail "environment file not found; restore it with --restore-environment or provide --env-file"

compose() {
  docker compose --project-name "$project_name" --env-file "$env_file" -f "$compose_file" "$@"
}

compose config -q
compose --profile runner --profile keycloak down --remove-orphans
docker image inspect "$postgres_image" >/dev/null 2>&1 || docker pull "$postgres_image"

for volume_name in $volume_names; do
  docker volume create \
    --label "com.docker.compose.project=$project_name" \
    --label "com.docker.compose.volume=$volume_name" \
    "${project_name}_${volume_name}" >/dev/null
  docker run --rm \
    --volume "${project_name}_${volume_name}:/target" \
    --volume "$backup_path/volumes:/backup:ro" \
    "$postgres_image" \
    sh -c 'find /target -mindepth 1 -maxdepth 1 -exec rm -rf -- {} \; && exec tar -xzf "/backup/$1.tar.gz" -C /target' \
    sh "$volume_name"
done

if [ "$keycloak_included" = true ]; then
  compose --profile keycloak up --detach --wait postgres keycloak-postgres
else
  compose up --detach --wait postgres
fi
# POSTGRES_USER and POSTGRES_DB expand inside each database container.
# shellcheck disable=SC2016
compose exec -T postgres sh -c \
  'dropdb --if-exists --force -U "$POSTGRES_USER" "$POSTGRES_DB" && createdb -U "$POSTGRES_USER" "$POSTGRES_DB"'
# shellcheck disable=SC2016
compose exec -T postgres sh -c \
  'exec pg_restore --no-owner --no-acl --exit-on-error -U "$POSTGRES_USER" -d "$POSTGRES_DB"' \
  <"$backup_path/lorehub.pgdump"
if [ "$keycloak_included" = true ]; then
  # shellcheck disable=SC2016
  compose exec -T keycloak-postgres sh -c \
    'dropdb --if-exists --force -U "$POSTGRES_USER" "$POSTGRES_DB" && createdb -U "$POSTGRES_USER" "$POSTGRES_DB"'
  # shellcheck disable=SC2016
  compose exec -T keycloak-postgres sh -c \
    'exec pg_restore --no-owner --no-acl --exit-on-error -U "$POSTGRES_USER" -d "$POSTGRES_DB"' \
    <"$backup_path/keycloak.pgdump"
fi

profile_arguments=""
if [ "$runner_profile" = true ]; then
  profile_arguments="$profile_arguments --profile runner"
fi
if [ "$keycloak_included" = true ]; then
  profile_arguments="$profile_arguments --profile keycloak"
fi
if [ "$start_services" = true ]; then
  # profile_arguments contains only fixed flag words assembled above.
  # shellcheck disable=SC2086
  compose $profile_arguments up --detach --wait
  echo "Restore completed and services started for project: $project_name"
else
  echo "Restore completed for project: $project_name"
  echo "Only the restored database services are running. Validate the data before starting the other services."
fi
