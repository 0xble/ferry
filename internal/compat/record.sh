#!/usr/bin/env bash
# Re-record the caller-compatibility goldens from the pre-toolkit ferry.
# Usage: internal/compat/record.sh [git-ref]
# The default ref is 6267d4d, origin/main immediately before the rewrite.
# The old client needs no change: FERRY_ADMIN_ADDR points it at the test
# daemon. Only ferry is built, never ferryd, so a case whose daemon is down
# cannot spawn one.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."

ref="${1:-6267d4d}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/src" "$tmp/bin"
git archive "$ref" | tar -x -C "$tmp/src"

(cd "$tmp/src" && GOWORK=off go build -o "$tmp/bin/ferry" ./cmd/ferry)
GOWORK=off go test ./internal/compat -run 'TestCallers$' -count=1 -args -record "$tmp/bin/ferry"
echo "recorded goldens from $ref"
