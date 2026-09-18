#!/usr/bin/env sh
# T-942 QA remediation (DEC-099), widened by T-943 (DEC-100): enforces that
# MPL-2.0 is scoped to exactly the modules AGENT.md section 3/16 and DEC-100
# name -- the torrent engine and the named set it compiles against -- not
# admitted as a license family generally, and not as a
# `github.com/anacrolix/*` prefix wildcard.
#
# Why this exists: `go-licenses check --allowed_licenses=...` (wired into the
# Makefile's `licenses` target) is a global license allowlist with no
# per-module targeting flag -- confirmed directly against
# `go-licenses check --help` (v2.0.1): --allowed_licenses takes "a list of
# allowed license names", full stop, and every other flag on that command is
# either the package argument, --ignore (path-prefix exclusion, not a
# per-license scope), or generic klog/logging plumbing. Once MPL-2.0 is in
# that list, `go-licenses check` passes ANY MPL-2.0 module, present or
# future -- the named-module scope described in AGENT.md, README.md, DEC-098
# and DEC-100 is a policy statement the allowlist by itself cannot enforce.
# This script closes that gap.
#
# Why an enumerated list and not a prefix: nine of the ten admitted modules
# sit under github.com/anacrolix/, so `github.com/anacrolix/*` would be
# shorter -- and would silently admit any future module published under that
# path. The whole value of this gate is that admitting a module is a
# deliberate, reviewed act with a DEC- entry behind it, so the set is
# enumerated (see the Makefile's ALLOWED_MPL_MODULES) and a module outside it
# fails the build by name.
#
# It reads the same CSV `go-licenses report` already produces for the
# Makefile's NOTICE generation (module,license_url,license per line, no
# header) -- see the `licenses` target -- rather than running a second scan,
# and asserts every MPL-2.0 row names one of the allowed modules. This still
# leaves ALLOWED_LICENSES itself as a global allowlist (see the Makefile
# comment above it and DEC-099's "residual gap" note, unchanged by DEC-100);
# this script is what turns the named scope from prose into something that
# fails a build.
#
# Usage:
#   scripts/check-license-scope.sh <allowed-modules> [report-file ...]
#
# <allowed-modules> is a comma-separated list of module paths (a single
# module path is just a list of one). Reads CSV from the given report
# file(s), or from stdin if none are given. Exits 0 if every MPL-2.0 row's
# module is in the allowed set (including the trivial case of no MPL-2.0 rows
# at all). Exits 1 and names every offending module otherwise. Exits 2 on a
# usage error. POSIX sh, passes `shellcheck -s sh`.
set -eu

if [ "$#" -lt 1 ]; then
	echo "usage: $0 <allowed-modules> [report-file ...]" >&2
	echo "  <allowed-modules> is a comma-separated list of module paths." >&2
	exit 2
fi

allowed_modules=$1
shift

if [ -z "$allowed_modules" ]; then
	echo "$0: <allowed-modules> is empty -- pass at least one module path." >&2
	exit 2
fi

if [ "$#" -eq 0 ]; then
	report=$(cat)
else
	report=$(cat -- "$@")
fi

# Only a row whose third CSV field is exactly "MPL-2.0" and whose first field
# (the module) is not in the allowed set counts as an offender. NF >= 3 guards
# against a blank trailing line producing a spurious empty-string "module".
# Module paths never contain a comma, so splitting the allowed list on "," is
# unambiguous.
offenders=$(printf '%s\n' "$report" | LC_ALL=C awk -F, -v allowed="$allowed_modules" '
	BEGIN {
		n = split(allowed, allowed_list, ",")
		for (i = 1; i <= n; i++) {
			if (allowed_list[i] != "") {
				is_allowed[allowed_list[i]] = 1
			}
		}
	}
	NF >= 3 && $3 == "MPL-2.0" && !($1 in is_allowed) { print $1 }
')

if [ -n "$offenders" ]; then
	echo "check-license-scope: MPL-2.0 is admitted only for the named module set (AGENT.md section 3/16, DEC-098, DEC-100) -- found MPL-2.0 module(s) outside that scope:" >&2
	printf '%s\n' "$offenders" | LC_ALL=C sort -u | while IFS= read -r mod; do
		echo "  - $mod" >&2
	done
	echo "Admitted set: $allowed_modules" >&2
	echo "If this module is meant to be admitted too, that is a license-policy change requiring a new DEC- entry and an AGENT.md section 3/16 update -- not a Makefile tweak." >&2
	exit 1
fi

exit 0
