#!/usr/bin/env sh
# T-945: regression test for scripts/next-task.sh. Builds disposable tracker fixtures and checks
# that archived ids count as done, dependencies gate eligibility, file order (not id order)
# decides the next task, and a blocked chain reports "none eligible".
#
# POSIX sh, passes `shellcheck -s sh`. Run directly, or via `make test-scripts`.
# Literal backticks in the fixtures are markdown, not command substitution.
# shellcheck disable=SC2016
set -eu

SCRIPT_DIR=$(unset CDPATH; cd -- "$(dirname -- "$0")" && pwd)
NEXT="$SCRIPT_DIR/next-task.sh"

fail() {
	echo "FAIL: $1" >&2
	exit 1
}

tmp_dir=$(mktemp -d)
cleanup() { rm -rf "$tmp_dir"; }
trap cleanup EXIT

block() {
	# $1 id+title, $2 status, $3 depends, $4 tier
	printf '### %s\n```\nstatus: %s\ndepends: %s\ntier: %s\n```\n**Acceptance**\n- x\n\n---\n\n' "$1" "$2" "$3" "$4"
}

# Case 1: T-001 archived, T-002 done in place, T-950 (file order first) waits on T-003,
# T-003 depends on both done tasks -> next is T-003 even though T-950 appears earlier.
{
	printf '# tracker\n\n## Phase 0\n\n**Done (archived in `docs/tracker-archive.md`):** `T-001` Boot.\n\n'
	block 'T-950 · Follow-up' todo 'T-003' M
	block 'T-002 · Config' 'done' 'T-001' M
	block 'T-003 · Engine' todo 'T-001, T-002' H
	block 'T-004 · Screen' todo 'T-003' L
	printf '## Backlog\n\n- `T-999` not a block\n'
} >"$tmp_dir/one.md"

out=$("$NEXT" "$tmp_dir/one.md")
printf '%s\n' "$out" | grep -qx 'next: T-003 · Engine (tier H)' || fail "case 1 next, got: $out"
printf '%s\n' "$out" | grep -qx 'done: 2  todo: 3  in-progress: 0  blocked: 0' || fail "case 1 counts, got: $out"

# Case 2: the only todo depends on a blocked task -> none eligible, blocked task listed.
{
	printf '## Phase 0\n\n'
	block 'T-010 · Store' blocked '—' H
	block 'T-011 · Resume' todo 'T-010' H
} >"$tmp_dir/two.md"

out=$("$NEXT" "$tmp_dir/two.md")
printf '%s\n' "$out" | grep -qx 'next: none eligible' || fail "case 2 next, got: $out"
printf '%s\n' "$out" | grep -qx 'blocked: T-010 · Store' || fail "case 2 blocked listing, got: $out"

# Case 3: two eligible todos -> the first in file order wins, even with the higher id.
{
	printf '## Phase 0\n\n'
	block 'T-944 · Follow-up' todo '—' M
	block 'T-041 · Resume' todo '—' H
} >"$tmp_dir/three.md"

out=$("$NEXT" "$tmp_dir/three.md")
printf '%s\n' "$out" | grep -qx 'next: T-944 · Follow-up (tier M)' || fail "case 3 file order, got: $out"

# Case 4: a missing file exits 2.
set +e
"$NEXT" "$tmp_dir/absent.md" >/dev/null 2>&1
rc=$?
set -e
[ "$rc" -eq 2 ] || fail "case 4 expected exit 2, got $rc"

echo "next-task_test: all cases passed"
