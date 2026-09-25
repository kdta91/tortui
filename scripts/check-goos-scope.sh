#!/usr/bin/env sh
# T-944: enforces AGENT.md section 14's invariant — "every OS-specific line
# lives in internal/platform behind _darwin.go, _linux.go, _windows.go build
# tags... A runtime.GOOS switch anywhere else fails review." Review is a
# human running a grep and noticing, intermittently; three independent
# reviewers on PRs #31/#32 found runtime.GOOS already leaking outside
# internal/platform (internal/config/load_test.go, internal/doctor/doctor.go
# -- both read-only uses, neither added a behavioural branch, so neither was
# blocking at the time) and flagged it as a gap a later change could widen
# silently without anyone noticing. T-944 moved both call sites behind
# internal/platform (platform.IsWindows(), platform.OS()) and added this
# script so the next occurrence fails `make check` on sight instead of
# waiting for the next attentive reviewer.
#
# Usage: scripts/check-goos-scope.sh [root]
#
# Scans every file `git ls-files` reports under root (default: the current
# directory) matching *.go for the literal text "runtime.GOOS", excluding
# internal/platform/** -- the one package AGENT.md section 14 permits it
# in -- and this script's own source (its header above names the identifier
# in prose, same convention as scripts/check-indexer-hostnames.sh). Exits 1
# and names every offending file:line if any is found outside that scope,
# 0 otherwise. POSIX sh, passes `shellcheck -s sh`.
set -eu

ROOT=${1:-.}

cd -- "$ROOT"

offenders=$(git ls-files -- '*.go' |
	grep -v '^internal/platform/' |
	{ xargs grep -Hn 'runtime\.GOOS' -- 2>/dev/null || true; })

if [ -n "$offenders" ]; then
	echo "check-goos-scope: runtime.GOOS found outside internal/platform (AGENT.md section 14) -- an OS branch anywhere else fails review:" >&2
	printf '%s\n' "$offenders" | while IFS= read -r line; do
		echo "  $line" >&2
	done
	exit 1
fi

exit 0
