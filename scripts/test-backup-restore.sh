#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
temporary_directory=$(mktemp -d)
trap 'rm -rf -- "$temporary_directory"' EXIT INT TERM

if "$script_dir/backup.sh" --project-name '../invalid' --output "$temporary_directory/backup" 2>/dev/null; then
  echo "backup accepted an invalid project name" >&2
  exit 1
fi

mkdir -p "$temporary_directory/incomplete/volumes"
printf '%s\n' \
  'format_version=1' \
  'status=incomplete' \
  'volumes=lore-data api-data auth-signing auth-tls auth-ca-state runner-data keycloak-data' \
  >"$temporary_directory/incomplete/manifest.txt"

if "$script_dir/restore.sh" \
  --backup "$temporary_directory/incomplete" \
  --project-name target \
  --confirm-project different 2>/dev/null; then
  echo "restore accepted a mismatched project confirmation" >&2
  exit 1
fi

if "$script_dir/restore.sh" \
  --backup "$temporary_directory/incomplete" \
  --project-name target \
  --confirm-project target 2>/dev/null; then
  echo "restore accepted an incomplete backup" >&2
  exit 1
fi

echo "Backup and restore safety checks passed."
