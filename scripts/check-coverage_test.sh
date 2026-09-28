#!/usr/bin/env sh
# T-093: regression test for scripts/check-coverage.sh.
#
# Builds disposable coverprofile fixtures (same convention as
# scripts/check-license-scope_test.sh) and covers:
#   1. Every package at or above its floor passes.
#   2. One subpackage under the floor fails, and is named, even when the
#      tree's other packages would lift an aggregate above it.
#   3. A prefix matching no package in the profile fails.
#   4. A prefix does not match a sibling that merely shares its spelling
#      (internal/tuiextra is not under internal/tui).
#
# POSIX sh, passes `shellcheck -s sh`.
#
# Run directly, or via `make test-scripts`.
set -eu

SCRIPT_DIR=$(unset CDPATH; cd -- "$(dirname -- "$0")" && pwd)
CHECK_SCRIPT="$SCRIPT_DIR/check-coverage.sh"

fail() {
	echo "FAIL: $1" >&2
	exit 1
}

tmp=$(mktemp -d)
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT

# example.org/m/internal/engine: 8 of 10 statements covered (80%).
# example.org/m/internal/engine/sub: 7 of 10 covered (70%).
# Together 15 of 20 (75%), which would pass an aggregate 75% floor.
# example.org/m/internal/tuiextra: 0 of 10 covered.
cat >"$tmp/profile.out" <<'EOF'
mode: set
example.org/m/internal/engine/a.go:1.1,2.2 8 1
example.org/m/internal/engine/a.go:3.1,4.2 2 0
example.org/m/internal/engine/sub/b.go:1.1,2.2 7 1
example.org/m/internal/engine/sub/b.go:3.1,4.2 3 0
example.org/m/internal/tuiextra/c.go:1.1,2.2 10 0
EOF

# --- Case 1: every package meets its floor. ------------------------------
if ! "$CHECK_SCRIPT" "$tmp/profile.out" example.org/m/internal/engine=70; then
	fail "packages at or above their floor were rejected"
fi

# --- Case 2: a subpackage under the floor fails and is named. ------------
set +e
out2=$("$CHECK_SCRIPT" "$tmp/profile.out" example.org/m/internal/engine=75 2>&1)
status2=$?
set -e

if [ "$status2" -eq 0 ]; then
	echo "$out2" >&2
	fail "a subpackage at 70% passed a 75% floor"
fi

if ! printf '%s\n' "$out2" | grep -q "FAIL example.org/m/internal/engine/sub 70.0%"; then
	echo "$out2" >&2
	fail "the failing subpackage was not named with its coverage"
fi

if ! printf '%s\n' "$out2" | grep -q "ok   example.org/m/internal/engine 80.0%"; then
	echo "$out2" >&2
	fail "the passing package was not reported as ok"
fi

# --- Case 3: a prefix with no package in the profile fails. --------------
set +e
out3=$("$CHECK_SCRIPT" "$tmp/profile.out" example.org/m/internal/indexer=75 2>&1)
status3=$?
set -e

if [ "$status3" -eq 0 ]; then
	echo "$out3" >&2
	fail "a floor matching no package passed"
fi

# --- Case 4: internal/tui does not match internal/tuiextra. --------------
set +e
out4=$("$CHECK_SCRIPT" "$tmp/profile.out" example.org/m/internal/tui=50 2>&1)
status4=$?
set -e

if [ "$status4" -eq 0 ] || printf '%s\n' "$out4" | grep -q "tuiextra"; then
	echo "$out4" >&2
	fail "a prefix matched a sibling package that only shares its spelling"
fi

echo "check-coverage_test: all cases passed"
