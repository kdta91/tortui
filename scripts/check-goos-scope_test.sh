#!/usr/bin/env sh
# T-944: regression test for scripts/check-goos-scope.sh.
#
# Builds a disposable scratch git repository (same convention as
# scripts/check-indexer-hostnames_test.sh and
# scripts/check-license-scope_test.sh) so this test needs no fixture files
# committed to the real repository, and covers:
#   1. runtime.GOOS used outside internal/platform must be flagged, by file
#      and line.
#   2. The identical pattern inside internal/platform must be allowed.
#   3. A tree with no offending occurrence passes cleanly.
#
# POSIX sh, passes `shellcheck -s sh`.
#
# Run directly, or via `make test-scripts`.
set -eu

SCRIPT_DIR=$(unset CDPATH; cd -- "$(dirname -- "$0")" && pwd)
CHECK_SCRIPT="$SCRIPT_DIR/check-goos-scope.sh"

fail() {
	echo "FAIL: $1" >&2
	exit 1
}

tmp_repo=$(mktemp -d)
cleanup() { rm -rf "$tmp_repo"; }
trap cleanup EXIT

mkdir -p "$tmp_repo/internal/platform" "$tmp_repo/internal/other"

cat >"$tmp_repo/internal/platform/thing_darwin.go" <<'EOF'
package platform

import "runtime"

func isDarwin() bool { return runtime.GOOS == "darwin" }
EOF

cat >"$tmp_repo/internal/other/thing.go" <<'EOF'
package other

import "runtime"

func isWindows() bool { return runtime.GOOS == "windows" }
EOF

(
	cd "$tmp_repo"
	git init -q
	git config user.email "test@example.invalid"
	git config user.name "check-goos-scope_test"
	git add -A
)

# --- Case 1/2: an occurrence outside internal/platform must be flagged, --
# and the allowed occurrence inside internal/platform must not be. --------
set +e
out1=$("$CHECK_SCRIPT" "$tmp_repo" 2>&1)
status1=$?
set -e

if [ "$status1" -eq 0 ]; then
	echo "$out1" >&2
	fail "runtime.GOOS outside internal/platform was not flagged"
fi

if ! printf '%s\n' "$out1" | grep -q "internal/other/thing.go"; then
	echo "$out1" >&2
	fail "violation output did not name the offending file"
fi

if printf '%s\n' "$out1" | grep -q "internal/platform/thing_darwin.go"; then
	echo "$out1" >&2
	fail "an allowed internal/platform occurrence was incorrectly flagged"
fi

# --- Case 3: remove the offending file and prove a clean tree passes. ----
rm "$tmp_repo/internal/other/thing.go"
(cd "$tmp_repo" && git add -A)

if ! "$CHECK_SCRIPT" "$tmp_repo"; then
	fail "a tree with runtime.GOOS confined to internal/platform was incorrectly flagged"
fi

echo "check-goos-scope_test: all cases passed"
