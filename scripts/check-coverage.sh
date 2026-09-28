#!/usr/bin/env sh
# T-093 (resolves Backlog T-916): per-package coverage floors from AGENT.md
# section 9, enforced against a `go test -coverprofile` profile.
#
# Usage: check-coverage.sh <coverprofile> <prefix=floor>...
#
# Every package whose import path equals <prefix> or sits under "<prefix>/"
# must have statement coverage >= floor percent. Each package is checked on
# its own, so a weak subpackage cannot hide behind a strong sibling. A
# prefix that matches no package in the profile fails too: a floor that
# silently checks nothing (a renamed tree, a typo) is the gap this closes.
#
# Coverage is computed exactly as `go test -cover` reports it: covered
# statements over total statements, per package, from the profile's
# "file:start,end numStmts count" block lines.
#
# POSIX sh + awk, passes `shellcheck -s sh`. Self-test:
# scripts/check-coverage_test.sh (run by `make test-scripts`).
set -eu

if [ "$#" -lt 2 ]; then
	echo "usage: $0 <coverprofile> <prefix=floor>..." >&2
	exit 2
fi

profile=$1
shift

if [ ! -r "$profile" ]; then
	echo "check-coverage: cannot read profile $profile" >&2
	exit 2
fi

LC_ALL=C awk -v floors="$*" '
	BEGIN {
		n = split(floors, spec, " ")
		for (i = 1; i <= n; i++) {
			eq = index(spec[i], "=")
			if (eq == 0) {
				printf "check-coverage: bad floor %s (want prefix=percent)\n", spec[i] > "/dev/stderr"
				bad = 1
				exit 2
			}
			prefix[i] = substr(spec[i], 1, eq - 1)
			floor[i] = substr(spec[i], eq + 1) + 0
		}
	}
	/^mode:/ { next }
	NF == 3 {
		# $1 is "import/path/file.go:start,end"; the package is everything
		# before the last "/".
		file = $1
		sub(/:.*/, "", file)
		pkg = file
		sub(/\/[^\/]*$/, "", pkg)
		total[pkg] += $2
		if ($3 + 0 > 0) {
			covered[pkg] += $2
		}
	}
	END {
		if (bad) {
			exit 2
		}
		status = 0
		for (i = 1; i <= n; i++) {
			matched = 0
			for (pkg in total) {
				if (pkg != prefix[i] && index(pkg, prefix[i] "/") != 1) {
					continue
				}
				matched = 1
				pct = total[pkg] ? 100 * covered[pkg] / total[pkg] : 100
				verdict = "ok"
				if (pct < floor[i]) {
					verdict = "FAIL"
					status = 1
				}
				printf "check-coverage: %-4s %s %.1f%% (floor %s%%)\n", verdict, pkg, pct, floor[i]
			}
			if (!matched) {
				printf "check-coverage: FAIL no package in the profile matches %s\n", prefix[i]
				status = 1
			}
		}
		exit status
	}
' "$profile"
