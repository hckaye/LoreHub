#!/bin/sh
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)

required_env() {
	if [ -z "$(printenv "$1" || true)" ]; then
		echo "$1 is required" >&2
		exit 1
	fi
}

# These values are deliberately supplied by the running control plane. The
# script never prints a token and does not mint a weaker test credential.
for variable in \
	LOREHUB_TEST_LORE_URL \
	LOREHUB_TEST_LORE_OTHER_URL \
	LOREHUB_TEST_LORE_AUTH_URL \
	LOREHUB_TEST_LORE_IDENTITY \
	LOREHUB_TEST_LORE_READ_TOKEN \
	LOREHUB_TEST_LORE_OTHER_TOKEN \
	LOREHUB_TEST_LORE_BASE_TOKEN \
	LOREHUB_TEST_LORE_EXPIRED_TOKEN \
	LOREHUB_TEST_LORE_WRONG_ISSUER_TOKEN \
	LOREHUB_TEST_LORE_WRONG_AUDIENCE_TOKEN \
	LOREHUB_TEST_LORE_WRONG_KID_TOKEN; do
	required_env "$variable"
done

cache_dir=${GOCACHE:-}
remove_cache=0
if [ -z "$cache_dir" ]; then
	cache_dir=$(mktemp -d "${TMPDIR:-/tmp}/lorehub-auth-smoke.XXXXXX")
	remove_cache=1
	export GOCACHE="$cache_dir"
fi
cleanup() {
	if [ "$remove_cache" -eq 1 ]; then
		rm -rf "$cache_dir"
	fi
}
trap cleanup EXIT INT TERM

cd "$root/services/api"
go test ./internal/lore \
	-run '^TestSDKClientLoreAuthBoundaryAgainstLoreServer$' \
	-count=1

# The Lore protocol test above proves the stock 0.8.6 gRPC and QUIC data-plane
# checks. The remaining controls are PostgreSQL-backed policy tests. They are
# part of the default smoke, while LOREHUB_SMOKE_LORE_ONLY=1 is an explicit
# protocol-only mode for a separately managed Lore server.
if [ "${LOREHUB_SMOKE_LORE_ONLY:-0}" = "1" ]; then
	exit 0
fi
required_env DATABASE_URL

platform_tests='^TestAuthorizationIntegration('
platform_tests="${platform_tests}PartitionsTeamsAndRevocation|OutsideDirectCollaboratorMatrix|"
platform_tests="${platform_tests}ProtectedBranchAndOneTimeMerge)$"
go test ./internal/platform -run "$platform_tests" -count=1
