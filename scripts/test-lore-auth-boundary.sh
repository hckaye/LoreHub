#!/bin/sh
set -eu

root=$(cd "$(dirname "$0")/.." && pwd)

required_env() {
	if [ -z "$(printenv "$1" || true)" ]; then
		echo "$1 is required" >&2
		exit 1
	fi
}

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

cd "$root/services/api"
exec go test ./internal/lore -run '^TestSDKClientLoreAuthBoundaryAgainstLoreServer$' -count=1
