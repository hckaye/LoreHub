#!/usr/bin/env bash
set -Eeuo pipefail

# Read the server address from terraform output and run lorehub-redeploy over SSH.
# Usage: ./redeploy.sh [git-ref]

usage() {
  printf 'usage: %s [git-ref]\n' "$(basename "$0")" >&2
  exit 2
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

[[ $# -le 1 ]] || usage

cd "$(dirname "$0")"

ipv4=$(terraform output -raw server_ipv4) || fail "terraform output server_ipv4 failed"
[[ -n "$ipv4" ]] || fail "server_ipv4 output is empty"

ssh_command=$(terraform output -raw ssh_command) || fail "terraform output ssh_command failed"
user=${ssh_command#ssh }
user=${user%%@*}
[[ -n "$user" ]] || fail "could not read SSH user from ssh_command"

if [[ $# -eq 1 ]]; then
  ssh "${user}@${ipv4}" lorehub-redeploy "$1"
else
  ssh "${user}@${ipv4}" lorehub-redeploy
fi
