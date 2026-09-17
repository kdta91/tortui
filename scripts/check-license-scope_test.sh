#!/usr/bin/env sh
# T-942 QA remediation: regression test for scripts/check-license-scope.sh.
#
# QA failed PR #28 on a scope gap: `go-licenses check --allowed_licenses=...`
# has no per-module scoping, so admitting MPL-2.0 for the allowlist actually
# admits it for ANY module, not just the named github.com/anacrolix/torrent
# exception. check-license-scope.sh closes that gap by inspecting the
# go-licenses report CSV directly; this test proves it actually rejects an
# unrelated MPL-2.0 module and still accepts the one that's allowed.
#
# Builds disposable CSV fixtures under a tempdir -- no go.mod, no network, no
# dependency on go-licenses being installed -- so this runs unconditionally
# in `make check`/`test-scripts` the same way check-indexer-hostnames_test.sh
# does. Module names below are invented (github.com/example/*) per AGENT.md
# section 2's fixture conventions.
#
# POSIX sh, passes `shellcheck -s sh`. Run directly, or via `make test-scripts`.
set -eu

SCRIPT_DIR=$(unset CDPATH; cd -- "$(dirname -- "$0")" && pwd)
CHECK_SCRIPT="$SCRIPT_DIR/check-license-scope.sh"
ALLOWED_MODULE="github.com/anacrolix/torrent"

fail() {
	echo "FAIL: $1" >&2
	exit 1
}

tmp_dir=$(mktemp -d)
cleanup() { rm -rf "$tmp_dir"; }
trap cleanup EXIT

run_check() {
	# $1 = report file, $2 = output-capture file. Never let set -e kill the
	# test on a nonzero (expected) exit -- the caller inspects the status.
	report="$1"
	out="$2"
	set +e
	"$CHECK_SCRIPT" "$ALLOWED_MODULE" "$report" >"$out" 2>&1
	status=$?
	set -e
	return "$status"
}

# --- Case 1: the allowed module alone must pass. ---------------------------
cat >"$tmp_dir/case1.csv" <<EOF
github.com/BurntSushi/toml,https://example.invalid/toml/LICENSE,MIT
$ALLOWED_MODULE,https://example.invalid/torrent/LICENSE,MPL-2.0
EOF
if ! run_check "$tmp_dir/case1.csv" "$tmp_dir/out1.txt"; then
	cat "$tmp_dir/out1.txt" >&2
	fail "the allowed module's own MPL-2.0 row was rejected"
fi

# --- Case 2: no MPL-2.0 rows at all must pass (trivial case). --------------
cat >"$tmp_dir/case2.csv" <<'EOF'
github.com/BurntSushi/toml,https://example.invalid/toml/LICENSE,MIT
github.com/adrg/xdg,https://example.invalid/xdg/LICENSE,MIT
EOF
if ! run_check "$tmp_dir/case2.csv" "$tmp_dir/out2.txt"; then
	cat "$tmp_dir/out2.txt" >&2
	fail "a report with no MPL-2.0 rows at all was incorrectly rejected"
fi

# --- Case 3: an MPL-2.0 module that is NOT the allowed one must be rejected,
# and named in the output. This is the exact scope gap QA found: an
# unrelated future dependency happening to be MPL-2.0 must fail loudly
# rather than silently riding through on the allowlist's coattails.
cat >"$tmp_dir/case3.csv" <<EOF
github.com/BurntSushi/toml,https://example.invalid/toml/LICENSE,MIT
$ALLOWED_MODULE,https://example.invalid/torrent/LICENSE,MPL-2.0
github.com/example/unrelated-mpl-module,https://example.invalid/unrelated/LICENSE,MPL-2.0
EOF
if run_check "$tmp_dir/case3.csv" "$tmp_dir/out3.txt"; then
	cat "$tmp_dir/out3.txt" >&2
	fail "an MPL-2.0 module other than $ALLOWED_MODULE was not rejected"
fi
grep -q "github.com/example/unrelated-mpl-module" "$tmp_dir/out3.txt" ||
	fail "rejection output did not name the offending module"
if grep -q "^  - $ALLOWED_MODULE\$" "$tmp_dir/out3.txt"; then
	fail "the allowed module was incorrectly listed as an offender alongside the real one"
fi

# --- Case 4: multiple non-allowed MPL-2.0 modules must all be named. -------
cat >"$tmp_dir/case4.csv" <<EOF
github.com/example/first-unrelated-mpl,https://example.invalid/first/LICENSE,MPL-2.0
github.com/example/second-unrelated-mpl,https://example.invalid/second/LICENSE,MPL-2.0
EOF
if run_check "$tmp_dir/case4.csv" "$tmp_dir/out4.txt"; then
	cat "$tmp_dir/out4.txt" >&2
	fail "multiple non-allowed MPL-2.0 modules were not rejected"
fi
grep -q "github.com/example/first-unrelated-mpl" "$tmp_dir/out4.txt" ||
	fail "rejection output did not name the first offending module"
grep -q "github.com/example/second-unrelated-mpl" "$tmp_dir/out4.txt" ||
	fail "rejection output did not name the second offending module"

# --- Case 5: reading from stdin (no report-file argument) must work the ----
# same way, since the Makefile may pipe report data through this script.
if ! "$CHECK_SCRIPT" "$ALLOWED_MODULE" <"$tmp_dir/case2.csv" >"$tmp_dir/out5.txt" 2>&1; then
	cat "$tmp_dir/out5.txt" >&2
	fail "reading a clean report from stdin was incorrectly rejected"
fi
set +e
"$CHECK_SCRIPT" "$ALLOWED_MODULE" <"$tmp_dir/case3.csv" >"$tmp_dir/out6.txt" 2>&1
status=$?
set -e
[ "$status" -ne 0 ] || fail "reading an offending report from stdin was not rejected"
grep -q "github.com/example/unrelated-mpl-module" "$tmp_dir/out6.txt" ||
	fail "stdin rejection output did not name the offending module"

# --- Case 6: usage error (no arguments) exits 2, not 0 or 1. ---------------
set +e
"$CHECK_SCRIPT" >"$tmp_dir/out7.txt" 2>&1
status=$?
set -e
[ "$status" -eq 2 ] || fail "missing arguments should exit 2, got $status"

echo "check-license-scope_test: all cases passed"
