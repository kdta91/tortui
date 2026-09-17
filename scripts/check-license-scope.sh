#!/usr/bin/env sh
# T-942 QA remediation: enforces that MPL-2.0 is scoped to exactly the one
# module AGENT.md section 3/16 and DEC-098 name -- github.com/anacrolix/torrent
# -- not admitted as a license family generally.
#
# Why this exists: `go-licenses check --allowed_licenses=...` (wired into the
# Makefile's `licenses` target) is a global license allowlist with no
# per-module targeting flag -- confirmed directly against
# `go-licenses check --help` (v2.0.1): --allowed_licenses takes "a list of
# allowed license names", full stop, and every other flag on that command is
# either the package argument, --ignore (path-prefix exclusion, not a
# per-license scope), or generic klog/logging plumbing. Once MPL-2.0 is in
# that list, `go-licenses check` passes ANY MPL-2.0 module, present or
# future -- the "for anacrolix/torrent alone" scope described in AGENT.md,
# README.md and DEC-098 is a policy statement the allowlist by itself cannot
# enforce. This script closes that gap.
#
# It reads the same CSV `go-licenses report` already produces for the
# Makefile's NOTICE generation (module,license_url,license per line, no
# header) -- see the `licenses` target -- rather than running a second scan,
# and asserts every MPL-2.0 row names the one allowed module. This still
# leaves ALLOWED_LICENSES itself as a global allowlist (see the Makefile
# comment above it and DEC-099's "residual gap" note); this script is what
# turns "for X alone" from prose into something that fails a build.
#
# Usage:
#   scripts/check-license-scope.sh <allowed-module> [report-file ...]
#
# Reads CSV from the given report file(s), or from stdin if none are given.
# Exits 0 if every MPL-2.0 row's module is <allowed-module> (including the
# trivial case of no MPL-2.0 rows at all). Exits 1 and names every offending
# module otherwise. Exits 2 on a usage error. POSIX sh, passes
# `shellcheck -s sh`.
set -eu

if [ "$#" -lt 1 ]; then
	echo "usage: $0 <allowed-module> [report-file ...]" >&2
	exit 2
fi

allowed_module=$1
shift

if [ "$#" -eq 0 ]; then
	report=$(cat)
else
	report=$(cat -- "$@")
fi

# Only a row whose third CSV field is exactly "MPL-2.0" and whose first field
# (the module) is not the allowed one counts as an offender. NF >= 3 guards
# against a blank trailing line producing a spurious empty-string "module".
offenders=$(printf '%s\n' "$report" | LC_ALL=C awk -F, -v allowed="$allowed_module" '
	NF >= 3 && $3 == "MPL-2.0" && $1 != allowed { print $1 }
')

if [ -n "$offenders" ]; then
	echo "check-license-scope: MPL-2.0 is admitted only for $allowed_module (AGENT.md section 3/16, DEC-098) -- found MPL-2.0 module(s) outside that scope:" >&2
	printf '%s\n' "$offenders" | LC_ALL=C sort -u | while IFS= read -r mod; do
		echo "  - $mod" >&2
	done
	echo "If this module is meant to be admitted too, that is a license-policy change requiring a new DEC- entry and an AGENT.md section 3/16 update -- not a Makefile tweak." >&2
	exit 1
fi

exit 0
