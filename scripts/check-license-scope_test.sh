#!/usr/bin/env sh
# T-942 QA remediation, widened by T-943: regression test for
# scripts/check-license-scope.sh.
#
# QA failed PR #28 on a scope gap: `go-licenses check --allowed_licenses=...`
# has no per-module scoping, so admitting MPL-2.0 for the allowlist actually
# admits it for ANY module, not just the named exception.
# check-license-scope.sh closes that gap by inspecting the go-licenses report
# CSV directly; this test proves it actually rejects an unlisted MPL-2.0
# module and still accepts every module in the admitted set.
#
# T-943 widened that set from one module to ten (DEC-100). A wider allowlist
# that nothing polices would be worse than the narrow one it replaced, so the
# cases below prove all three halves of the contract: every listed module
# passes, an unlisted MPL-2.0 module still fails and is named, and a
# partially-listed set fails on exactly the unlisted members. The expected set
# is also asserted against the Makefile's ALLOWED_MPL_MODULES, so widening the
# gate cannot happen without this test being updated too -- which is the point
# of enumerating modules instead of matching a `github.com/anacrolix/*`
# prefix.
#
# Builds disposable CSV fixtures under a tempdir -- no go.mod, no network, no
# dependency on go-licenses being installed -- so this runs unconditionally
# in `make check`/`test-scripts` the same way check-indexer-hostnames_test.sh
# does. Module names for the negative cases are invented (github.com/example/*)
# per AGENT.md section 2's fixture conventions.
#
# POSIX sh, passes `shellcheck -s sh`. Run directly, or via `make test-scripts`.
set -eu

SCRIPT_DIR=$(unset CDPATH; cd -- "$(dirname -- "$0")" && pwd)
CHECK_SCRIPT="$SCRIPT_DIR/check-license-scope.sh"
MAKEFILE="$SCRIPT_DIR/../Makefile"

# The set T-943 derived empirically from `go-licenses report ./...` over
# GOOS=darwin/linux/windows with github.com/anacrolix/torrent v1.61.0 in
# go.mod (DEC-100). Kept here literally, not read from the Makefile, so that
# a change to the Makefile's list fails this test rather than silently
# redefining what "the admitted set" means. Order matches the Makefile's.
EXPECTED_MODULES="github.com/anacrolix/torrent,github.com/anacrolix/dht/v2,github.com/anacrolix/generics,github.com/anacrolix/log,github.com/anacrolix/mmsg,github.com/anacrolix/multiless,github.com/anacrolix/sync,github.com/anacrolix/upnp,github.com/anacrolix/utp,github.com/go-llsqlite/adapter"
ALLOWED_MODULES="$EXPECTED_MODULES"
# Same set, space-separated, for iteration. Deliberately iterated with a plain
# `for` over word splitting rather than `... | while read`: a pipeline's loop
# body runs in a subshell, where fail()'s `exit 1` would only kill the
# subshell and let the test go on to report success. Module paths contain no
# whitespace, so splitting on IFS is safe here.
ALLOWED_MODULES_SPACED=$(printf '%s' "$ALLOWED_MODULES" | tr ',' ' ')

fail() {
	echo "FAIL: $1" >&2
	exit 1
}

tmp_dir=$(mktemp -d)
cleanup() { rm -rf "$tmp_dir"; }
trap cleanup EXIT

run_check() {
	# $1 = allowed-modules argument, $2 = report file, $3 = output-capture
	# file. Never let set -e kill the test on a nonzero (expected) exit --
	# the caller inspects the status.
	allowed="$1"
	report="$2"
	out="$3"
	set +e
	"$CHECK_SCRIPT" "$allowed" "$report" >"$out" 2>&1
	status=$?
	set -e
	return "$status"
}

# --- Case 0: the Makefile's ALLOWED_MPL_MODULES is exactly the set this test
# exercises. Without this, widening the Makefile's list would quietly widen
# the gate with every case below still passing.
makefile_modules=$(LC_ALL=C sed -n 's/^ALLOWED_MPL_MODULES := *//p' "$MAKEFILE")
[ -n "$makefile_modules" ] ||
	fail "could not read ALLOWED_MPL_MODULES from $MAKEFILE"
[ "$makefile_modules" = "$EXPECTED_MODULES" ] || fail "the Makefile's ALLOWED_MPL_MODULES has changed.
  Makefile: $makefile_modules
  expected: $EXPECTED_MODULES
Admitting another MPL-2.0 module is a license-policy change (AGENT.md section 3/16):
update this test and add a DEC- entry, do not just widen the Makefile."

# --- Case 1: every module in the admitted set, all present at once, passes. -
: >"$tmp_dir/case1.csv"
echo "github.com/BurntSushi/toml,https://example.invalid/toml/LICENSE,MIT" >>"$tmp_dir/case1.csv"
for mod in $ALLOWED_MODULES_SPACED; do
	echo "$mod,https://example.invalid/$mod/LICENSE,MPL-2.0" >>"$tmp_dir/case1.csv"
done
if ! run_check "$ALLOWED_MODULES" "$tmp_dir/case1.csv" "$tmp_dir/out1.txt"; then
	cat "$tmp_dir/out1.txt" >&2
	fail "the full admitted module set was rejected"
fi

# --- Case 2: each listed module on its own passes. The real reports do not all
# carry the same rows (anacrolix/utp and anacrolix/mmsg are alternative uTP
# transports, selected by CGO_ENABLED rather than by GOOS, and never appear
# together), so no single module may depend on another being present.
for mod in $ALLOWED_MODULES_SPACED; do
	cat >"$tmp_dir/case2.csv" <<EOF
github.com/BurntSushi/toml,https://example.invalid/toml/LICENSE,MIT
$mod,https://example.invalid/$mod/LICENSE,MPL-2.0
EOF
	if ! run_check "$ALLOWED_MODULES" "$tmp_dir/case2.csv" "$tmp_dir/out2.txt"; then
		cat "$tmp_dir/out2.txt" >&2
		fail "listed module $mod was rejected on its own"
	fi
done

# --- Case 3: no MPL-2.0 rows at all must pass (trivial case). --------------
cat >"$tmp_dir/case3.csv" <<'EOF'
github.com/BurntSushi/toml,https://example.invalid/toml/LICENSE,MIT
github.com/adrg/xdg,https://example.invalid/xdg/LICENSE,MIT
EOF
if ! run_check "$ALLOWED_MODULES" "$tmp_dir/case3.csv" "$tmp_dir/out3.txt"; then
	cat "$tmp_dir/out3.txt" >&2
	fail "a report with no MPL-2.0 rows at all was incorrectly rejected"
fi

# --- Case 4: an MPL-2.0 module that is NOT in the set must be rejected, and
# named in the output. This is the exact scope gap QA found, and the reason
# T-943's wider set is still an enumeration: an unrelated future dependency
# happening to be MPL-2.0 must fail loudly rather than silently riding
# through on the allowlist's coattails.
cat >"$tmp_dir/case4.csv" <<EOF
github.com/BurntSushi/toml,https://example.invalid/toml/LICENSE,MIT
github.com/anacrolix/torrent,https://example.invalid/torrent/LICENSE,MPL-2.0
github.com/example/unrelated-mpl-module,https://example.invalid/unrelated/LICENSE,MPL-2.0
EOF
if run_check "$ALLOWED_MODULES" "$tmp_dir/case4.csv" "$tmp_dir/out4.txt"; then
	cat "$tmp_dir/out4.txt" >&2
	fail "an MPL-2.0 module outside the admitted set was not rejected"
fi
grep -q "^  - github.com/example/unrelated-mpl-module\$" "$tmp_dir/out4.txt" ||
	fail "rejection output did not name the offending module"
if grep -q "^  - github.com/anacrolix/torrent\$" "$tmp_dir/out4.txt"; then
	fail "a listed module was incorrectly reported as an offender alongside the real one"
fi

# --- Case 5: a module under the same path prefix as the admitted ones but not
# itself listed must still be rejected. The set is an enumeration, not a
# `github.com/anacrolix/*` wildcard -- a future sibling module must not ride
# in on its path.
cat >"$tmp_dir/case5.csv" <<EOF
github.com/anacrolix/torrent,https://example.invalid/torrent/LICENSE,MPL-2.0
github.com/anacrolix/not-a-listed-sibling,https://example.invalid/sibling/LICENSE,MPL-2.0
EOF
if run_check "$ALLOWED_MODULES" "$tmp_dir/case5.csv" "$tmp_dir/out5.txt"; then
	cat "$tmp_dir/out5.txt" >&2
	fail "an unlisted module sharing the admitted path prefix was not rejected"
fi
grep -q "^  - github.com/anacrolix/not-a-listed-sibling\$" "$tmp_dir/out5.txt" ||
	fail "rejection output did not name the unlisted same-prefix module"

# --- Case 6: a PARTIALLY listed set fails on exactly the unlisted members.
# The report carries the whole admitted set, but only the first two modules
# are passed as allowed -- every other member must be named as an offender,
# and the two that were allowed must not be.
partial_allowed="github.com/anacrolix/torrent,github.com/anacrolix/dht/v2"
if run_check "$partial_allowed" "$tmp_dir/case1.csv" "$tmp_dir/out6.txt"; then
	cat "$tmp_dir/out6.txt" >&2
	fail "a partially-listed set did not fail on its unlisted members"
fi
for mod in $ALLOWED_MODULES_SPACED; do
	case ",$partial_allowed," in
	*",$mod,"*)
		if grep -q "^  - $mod\$" "$tmp_dir/out6.txt"; then
			fail "allowed module $mod was reported as an offender under the partial set"
		fi
		;;
	*)
		grep -q "^  - $mod\$" "$tmp_dir/out6.txt" ||
			fail "unlisted module $mod was not named under the partial set"
		;;
	esac
done

# --- Case 6b: membership is exact, not substring or prefix. A module whose
# path contains an admitted one, or extends it, must still be rejected --
# otherwise `github.com/anacrolix/torrent-fork` (or a path that merely embeds
# an admitted one) would ride in on a listed module's name.
cat >"$tmp_dir/case6b.csv" <<'EOF'
github.com/anacrolix/torrent-fork,https://example.invalid/fork/LICENSE,MPL-2.0
evil.example/github.com/anacrolix/log,https://example.invalid/embed/LICENSE,MPL-2.0
github.com/anacrolix/torren,https://example.invalid/short/LICENSE,MPL-2.0
EOF
if run_check "$ALLOWED_MODULES" "$tmp_dir/case6b.csv" "$tmp_dir/out6b.txt"; then
	cat "$tmp_dir/out6b.txt" >&2
	fail "modules matching an admitted path only as a substring were not rejected"
fi
for mod in github.com/anacrolix/torrent-fork evil.example/github.com/anacrolix/log github.com/anacrolix/torren; do
	grep -q "^  - $mod\$" "$tmp_dir/out6b.txt" ||
		fail "substring-match rejection output did not name $mod"
done

# --- Case 6c: a malformed row with an empty module field must still fail,
# named, rather than being silently dropped. go-licenses does not emit this
# today; the gate must fail closed if it ever does.
cat >"$tmp_dir/case6c.csv" <<'EOF'
,https://example.invalid/mystery/LICENSE,MPL-2.0
EOF
if run_check "$ALLOWED_MODULES" "$tmp_dir/case6c.csv" "$tmp_dir/out6c.txt"; then
	cat "$tmp_dir/out6c.txt" >&2
	fail "an MPL-2.0 row with an empty module field was accepted"
fi
grep -q "(unnamed module)" "$tmp_dir/out6c.txt" ||
	fail "empty-module-field rejection output did not flag the unnamed module"

# --- Case 7: multiple non-allowed MPL-2.0 modules must all be named. -------
cat >"$tmp_dir/case7.csv" <<EOF
github.com/example/first-unrelated-mpl,https://example.invalid/first/LICENSE,MPL-2.0
github.com/example/second-unrelated-mpl,https://example.invalid/second/LICENSE,MPL-2.0
EOF
if run_check "$ALLOWED_MODULES" "$tmp_dir/case7.csv" "$tmp_dir/out7.txt"; then
	cat "$tmp_dir/out7.txt" >&2
	fail "multiple non-allowed MPL-2.0 modules were not rejected"
fi
grep -q "github.com/example/first-unrelated-mpl" "$tmp_dir/out7.txt" ||
	fail "rejection output did not name the first offending module"
grep -q "github.com/example/second-unrelated-mpl" "$tmp_dir/out7.txt" ||
	fail "rejection output did not name the second offending module"

# --- Case 8: reading from stdin (no report-file argument) must work the ----
# same way, since the Makefile may pipe report data through this script.
if ! "$CHECK_SCRIPT" "$ALLOWED_MODULES" <"$tmp_dir/case3.csv" >"$tmp_dir/out8.txt" 2>&1; then
	cat "$tmp_dir/out8.txt" >&2
	fail "reading a clean report from stdin was incorrectly rejected"
fi
set +e
"$CHECK_SCRIPT" "$ALLOWED_MODULES" <"$tmp_dir/case4.csv" >"$tmp_dir/out9.txt" 2>&1
status=$?
set -e
[ "$status" -ne 0 ] || fail "reading an offending report from stdin was not rejected"
grep -q "github.com/example/unrelated-mpl-module" "$tmp_dir/out9.txt" ||
	fail "stdin rejection output did not name the offending module"

# --- Case 9: a single module path is still a valid list of one, preserving
# the pre-T-943 calling convention.
cat >"$tmp_dir/case10.csv" <<'EOF'
github.com/anacrolix/torrent,https://example.invalid/torrent/LICENSE,MPL-2.0
EOF
if ! run_check "github.com/anacrolix/torrent" "$tmp_dir/case10.csv" "$tmp_dir/out10.txt"; then
	cat "$tmp_dir/out10.txt" >&2
	fail "a single-module allowed list was rejected"
fi

# --- Case 10: usage errors exit 2, not 0 or 1. -----------------------------
set +e
"$CHECK_SCRIPT" >"$tmp_dir/out11.txt" 2>&1
status=$?
set -e
[ "$status" -eq 2 ] || fail "missing arguments should exit 2, got $status"

set +e
"$CHECK_SCRIPT" "" "$tmp_dir/case10.csv" >"$tmp_dir/out12.txt" 2>&1
status=$?
set -e
[ "$status" -eq 2 ] || fail "an empty allowed-modules list should exit 2, got $status"

echo "check-license-scope_test: all cases passed"
