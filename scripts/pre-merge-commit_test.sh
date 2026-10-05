#!/usr/bin/env sh
# T-911: regression test for scripts/pre-merge-commit.
#
# Builds a disposable scratch repository (same convention as the other
# scripts/*_test.sh) with scripts/ active as core.hooksPath, then:
#   1. merges a side branch that carries a real config.toml, committed with
#      hooks off the way the QA on T-004 did -- the merge must be refused,
#      for the pre-commit guard's reason, and leave HEAD where it was;
#   2. merges a clean side branch -- the merge must go through (skipped, with
#      a note, where gitleaks is not installed: the hook refuses without it).
#
# POSIX sh, passes `shellcheck -s sh`. Only invented names appear here.
# Run directly, or via `make test-scripts`.
set -eu

SCRIPT_DIR=$(unset CDPATH; cd -- "$(dirname -- "$0")" && pwd)
ROOT_DIR=$(dirname -- "$SCRIPT_DIR")

fail() {
	echo "FAIL: $1" >&2
	exit 1
}

tmp_repo=$(mktemp -d)
cleanup() { rm -rf "$tmp_repo"; }
trap cleanup EXIT

(
	cd "$tmp_repo"
	git init -q
	git config user.email "test@example.invalid"
	git config user.name "pre-merge-commit_test"
	mkdir scripts
	cp "$SCRIPT_DIR/pre-commit" "$SCRIPT_DIR/pre-merge-commit" scripts/
	chmod +x scripts/pre-commit scripts/pre-merge-commit
	cp "$ROOT_DIR/.gitleaks.toml" .gitleaks.toml
	echo base >base.txt
	git add -A
	git commit -q -m "base"
	main_branch=$(git symbolic-ref --short HEAD)

	git switch -q -c side-bad
	echo 'token = "placeholder"' >config.toml
	git add -f config.toml
	git commit -q -m "add a real config.toml" # hooks are not active yet
	git switch -q "$main_branch"
	echo main >main.txt
	git add main.txt
	git commit -q -m "main moves on"

	git switch -q -c side-good
	git reset -q --hard "$main_branch"
	echo fine >good.txt
	git add good.txt
	git commit -q -m "a clean change"
	git switch -q "$main_branch"

	git config core.hooksPath scripts
)

before=$(cd "$tmp_repo" && git rev-parse HEAD)
out="$tmp_repo/../pre-merge-commit_out.$$"
set +e
(cd "$tmp_repo" && git merge --no-ff --no-edit side-bad) >"$out" 2>&1
status=$?
set -e
if [ "$status" -eq 0 ]; then
	cat "$out" >&2
	rm -f "$out"
	fail "a merge bringing in a real config.toml was not refused (T-911)"
fi
grep -q "refusing to commit a real config.toml" "$out" || {
	cat "$out" >&2
	rm -f "$out"
	fail "the merge was refused, but not by the pre-commit guard's config.toml rule"
}
after=$(cd "$tmp_repo" && git rev-parse HEAD)
[ "$before" = "$after" ] || {
	rm -f "$out"
	fail "a refused merge moved HEAD"
}

if command -v gitleaks >/dev/null 2>&1; then
	(cd "$tmp_repo" && git merge --abort 2>/dev/null || true; git reset -q --hard)
	if ! (cd "$tmp_repo" && git merge --no-ff --no-edit side-good) >"$out" 2>&1; then
		cat "$out" >&2
		rm -f "$out"
		fail "a clean merge was refused"
	fi
else
	echo "pre-merge-commit_test: gitleaks not installed, skipping the clean-merge case"
fi
rm -f "$out"

echo "pre-merge-commit_test: all cases passed"
