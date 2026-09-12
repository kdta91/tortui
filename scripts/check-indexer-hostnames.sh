#!/usr/bin/env sh
# T-007: scans a diff for hostnames that look like a newly bundled/default
# indexer source, and fails if any such hostname is not on the allowlist.
#
# Why this exists, and what it deliberately does NOT do: AGENT.md section 2 bars
# naming any infringement-oriented site anywhere in this repository, in a
# blocklist, a regex, a fixture, or a comment. So this check can never be a
# denylist of known piracy hostnames -- there is nothing here to name. It is a
# shape-plus-context heuristic instead:
#
#   1. "Shape": a substring of an added line that looks like a hostname --
#      either the authority of an http(s):// URL, or the value of a
#      url/host/endpoint/base_url-style key.
#   2. "Context": the hostname-shaped substring only counts if it appears on a
#      line that either (a) lives in a file where tortui defines indexer
#      sources -- internal/indexer/**, config.example.toml,
#      docs/indexer-definitions.md, docs/bundled-sources.md,
#      docs/indexer-hostname-allowlist.md, or testdata/** fixtures -- or
#      (b) mentions "indexer", "torznab", "scraper", or "base_url" on the same
#      line. A hostname anywhere else in the diff (a README link, a CI action
#      reference, a go.mod/NOTICE dependency URL) is never even looked at.
#   3. Anything that survives (1) and (2) is allowed only if it is a
#      structurally non-resolving placeholder (localhost, an IP literal, or
#      one of the IANA-reserved example/test/invalid domains from RFC 2606),
#      or is explicitly listed in docs/indexer-hostname-allowlist.md.
#
# That allowlist file is the single place T-024's bundled lawful sources get
# added -- see its own header for what belongs there and what doesn't.
#
# Usage:
#   scripts/check-indexer-hostnames.sh <base-rev> [head-rev]
#
# With one rev, diffs that rev against the current working tree (uncommitted
# changes included; new files must be `git add`-ed to be seen, same as any
# other `git diff <rev>`) -- useful for a local check before committing. With
# two revs (both must be committed), diffs base-rev against head-rev -- what
# CI uses to check a pull request's diff.
set -eu

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
	echo "usage: $0 <base-rev> [head-rev]" >&2
	exit 2
fi

BASE_REV=$1
HEAD_REV=${2:-}
ALLOWLIST=${INDEXER_HOSTNAME_ALLOWLIST:-docs/indexer-hostname-allowlist.md}

if [ ! -f "$ALLOWLIST" ]; then
	echo "check-indexer-hostnames: allowlist file not found: $ALLOWLIST" >&2
	exit 1
fi

allow_file=$(mktemp)
diff_file=$(mktemp)
trap 'rm -f "$allow_file" "$diff_file"' EXIT

grep -v '^[[:space:]]*#' "$ALLOWLIST" | grep -v '^[[:space:]]*$' |
	sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' |
	tr '[:upper:]' '[:lower:]' >"$allow_file"

if [ -n "$HEAD_REV" ]; then
	if ! git diff --unified=0 --no-color "$BASE_REV" "$HEAD_REV" -- . >"$diff_file"; then
		echo "check-indexer-hostnames: git diff failed for $BASE_REV..$HEAD_REV" >&2
		exit 1
	fi
else
	if ! git diff --unified=0 --no-color "$BASE_REV" -- . >"$diff_file"; then
		echo "check-indexer-hostnames: git diff failed for $BASE_REV..working tree" >&2
		exit 1
	fi
fi

awk -v allowfile="$allow_file" -v allowlist_display="$ALLOWLIST" '
	BEGIN {
		n = 0
		while ((getline line < allowfile) > 0) {
			n++
			allow[n] = line
		}
		close(allowfile)
		violations = 0
	}
	/^\+\+\+ \/dev\/null/ { in_context = 0; next }
	/^\+\+\+ / {
		f = $0
		sub(/^\+\+\+ [ab]\//, "", f)
		file = f
		in_context = (file ~ /^internal\/indexer\//) \
			|| (file == "config.example.toml") \
			|| (file ~ /^docs\/(indexer-definitions|bundled-sources|indexer-hostname-allowlist)\.md$/) \
			|| (file ~ /^testdata\//)
		next
	}
	/^\+[^+]/ {
		linetext = substr($0, 2)
		lower = tolower(linetext)
		keyword_context = (lower ~ /indexer|torznab|scraper|base_url/)
		if (!(in_context || keyword_context)) next
		scan(linetext, file)
	}
	function is_allowed(host,   i, entry, hlen, elen, suffix) {
		if (host == "localhost") return 1
		if (host ~ /^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$/) return 1
		if (host ~ /(^|\.)example\.(com|net|org)$/) return 1
		# RFC 2606 / RFC 6761 reserved TLDs: any host ending in one of these
		# four labels is guaranteed never to resolve on the public Internet,
		# so it can never be a real bundled indexer regardless of what comes
		# before it -- e.g. "real-indexer.example", "my-indexer.test".
		if (host ~ /(^|\.)(example|test|invalid|localhost)$/) return 1
		for (i = 1; i <= n; i++) {
			entry = allow[i]
			if (entry == "") continue
			if (host == entry) return 1
			hlen = length(host)
			elen = length(entry)
			if (hlen > elen) {
				suffix = substr(host, hlen - elen)
				if (suffix == "." entry) return 1
			}
		}
		return 0
	}
	function scan(text, file,    rest, m, host) {
		# Authority after the scheme: may carry userinfo (user:pass@) and a
		# port, so match everything up to the first path/space/quote/angle
		# separator and let check_host() pick the hostname apart from that.
		rest = text
		while (match(rest, /https?:\/\/[^\/[:space:]"'\''<>]+/)) {
			m = substr(rest, RSTART, RLENGTH)
			sub(/^https?:\/\//, "", m)
			check_host(m, file, text)
			rest = substr(rest, RSTART + RLENGTH)
		}
		rest = text
		while (match(rest, /(url|host|endpoint|base_url)[[:space:]]*[:=][[:space:]]*"?[A-Za-z0-9][A-Za-z0-9.-]*\.[A-Za-z][A-Za-z]+/)) {
			m = substr(rest, RSTART, RLENGTH)
			sub(/^(url|host|endpoint|base_url)[[:space:]]*[:=][[:space:]]*"?/, "", m)
			check_host(m, file, text)
			rest = substr(rest, RSTART + RLENGTH)
		}
	}
	function check_host(host, file, text,   colon) {
		# Strip a trailing quote/space/paren/sentence-punctuation run picked
		# up from surrounding prose or markdown.
		gsub(/[",'"'"' 	.,;)\]]+$/, "", host)
		# Drop userinfo ("user:pass@host" -> "host"): a greedy match of
		# everything up to the LAST "@" removes it even if the password
		# itself contained "@".
		sub(/^.*@/, "", host)
		colon = index(host, ":")
		if (colon > 0) host = substr(host, 1, colon - 1)
		host = tolower(host)
		if (host == "") return
		if (!is_allowed(host)) {
			violations++
			printf "  %s: possible new indexer hostname: %s\n", file, host
			printf "    %s\n", text
		}
	}
	END {
		if (violations > 0) {
			printf "\ncheck-indexer-hostnames: %d possible new indexer hostname(s) not on the allowlist (%s)\n", violations, allowlist_display
			print "If this is a bundled lawful source landing under T-024, add it to the allowlist with a one-line justification."
			print "Otherwise: indexer endpoints/definitions/default sources are user-supplied only -- see CONTRIBUTING.md."
			exit 1
		}
	}
' <"$diff_file"
