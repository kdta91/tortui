#!/usr/bin/env sh
# T-007 QA remediation: regression test for scripts/check-indexer-hostnames.sh.
#
# Covers the case-sensitivity bug QA found on PR #7: the keyword-shaped match
# (url|host|endpoint|base_url) inside scan() was matched against the
# ORIGINAL-case diff text rather than a lowercased copy, so idiomatic Go
# struct-field style keys -- "Host:", "BaseURL:", capitalized the way Go
# actually reads -- inside internal/indexer/** were never caught, while a
# lowercase "host = ..." was. It also covers the DEC-040 correction: a
# private/loopback IPv4 literal is allowed, a public one is not.
#
# POSIX sh, passes `shellcheck -s sh`. Builds a disposable scratch git
# repository under a tempdir for every case, so no hostname is ever committed
# to this repository itself (AGENT.md section 2) -- only invented,
# non-resolving names ever appear here, the same convention T-007's other
# fixtures already use.
#
# Run directly, or via `make test-scripts`.
set -eu

SCRIPT_DIR=$(unset CDPATH; cd -- "$(dirname -- "$0")" && pwd)
CHECK_SCRIPT="$SCRIPT_DIR/check-indexer-hostnames.sh"
ALLOWLIST="$SCRIPT_DIR/../docs/indexer-hostname-allowlist.md"

fail() {
	echo "FAIL: $1" >&2
	exit 1
}

tmp_repo=$(mktemp -d)
# Check output lives outside the scratch repo so a later case's `git add -A`
# never commits an earlier case's flagged output into its own diff (T-9026).
out_dir=$(mktemp -d)
cleanup() { rm -rf "$tmp_repo" "$out_dir"; }
trap cleanup EXIT

(
	cd "$tmp_repo"
	git init -q
	git config user.email "test@example.invalid"
	git config user.name "check-indexer-hostnames_test"
	mkdir -p internal/indexer/fixture
	echo "placeholder" >internal/indexer/fixture/keep.go
	git add -A
	git commit -q -m "base commit"
)
base_sha=$(cd "$tmp_repo" && git rev-parse HEAD)

run_check() {
	# $1 = head sha, $2 = output file. Never let set -e kill the script on a
	# nonzero (expected) exit -- the caller inspects the captured status.
	head="$1"
	out="$2"
	set +e
	(cd "$tmp_repo" && INDEXER_HOSTNAME_ALLOWLIST="$ALLOWLIST" "$CHECK_SCRIPT" "$base_sha" "$head") \
		>"$out" 2>&1
	status=$?
	set -e
	return "$status"
}

# --- Case 1: capitalized Go-struct-field-style keys must be flagged. -------
# This is the exact shape QA found slipping through: "Host:" and "BaseURL:"
# read as idiomatic Go struct literals, capitalized, inside a gated path
# (internal/indexer/**) -- naming invented, non-reserved-TLD hosts so the
# check has something to catch that isn't already auto-allowed.
(
	cd "$tmp_repo"
	cat >internal/indexer/fixture/case_struct.go <<'EOF'
package fixture

// Deliberately invented, non-resolving test values -- never a real source.
var cfg = struct {
	Host    string
	BaseURL string
}{
	Host:    "some-invented-source-host.zzz",
	BaseURL: "https://another-invented-source.zzz/api",
}
EOF
	git add -A
	git commit -q -m "add capitalized struct fields"
)
head_case1=$(cd "$tmp_repo" && git rev-parse HEAD)

if run_check "$head_case1" "$out_dir/out1.txt"; then
	cat "$out_dir/out1.txt" >&2
	fail "capitalized Host:/BaseURL: struct fields were not flagged (case-sensitivity regression)"
fi
grep -q "some-invented-source-host.zzz" "$out_dir/out1.txt" ||
	fail "violation output did not name the capitalized Host: hostname"
grep -q "another-invented-source.zzz" "$out_dir/out1.txt" ||
	fail "violation output did not name the capitalized BaseURL: hostname"

# --- Case 2: control -- a reserved-TLD placeholder in the same shape must --
# still be allowed, proving the fix didn't turn this into "flag anything
# capitalized."
(
	cd "$tmp_repo"
	git checkout -q "$base_sha"
	cat >internal/indexer/fixture/case_struct_ok.go <<'EOF'
package fixture

// Deliberately invented, non-resolving test value -- never a real source.
var cfgOK = struct {
	Host string
}{
	Host: "harmless-fixture-host.example",
}
EOF
	git add -A
	git commit -q -m "add capitalized struct field using a reserved TLD"
)
head_case2=$(cd "$tmp_repo" && git rev-parse HEAD)

if ! run_check "$head_case2" "$out_dir/out2.txt"; then
	cat "$out_dir/out2.txt" >&2
	fail "reserved-TLD placeholder host was incorrectly flagged"
fi

# --- Case 3: DEC-040 correction -- a private IPv4 literal in the same -----
# capitalized shape is still allowed.
(
	cd "$tmp_repo"
	git checkout -q "$base_sha"
	cat >internal/indexer/fixture/case_private_ip.go <<'EOF'
package fixture

var cfgPrivateIP = struct {
	BaseURL string
}{
	BaseURL: "https://192.168.1.5/announce",
}
EOF
	git add -A
	git commit -q -m "add capitalized struct field using a private IP literal"
)
head_case3=$(cd "$tmp_repo" && git rev-parse HEAD)

if ! run_check "$head_case3" "$out_dir/out3.txt"; then
	cat "$out_dir/out3.txt" >&2
	fail "private IPv4 literal was incorrectly flagged"
fi

# --- Case 4: DEC-040 correction -- a public IPv4 literal is NOT auto- -----
# allowed anymore (it can be a real production endpoint), so it must be
# flagged the same as any other new hostname.
(
	cd "$tmp_repo"
	git checkout -q "$base_sha"
	cat >internal/indexer/fixture/case_public_ip.go <<'EOF'
package fixture

var cfgPublicIP = struct {
	BaseURL string
}{
	BaseURL: "https://203.0.113.5/announce",
}
EOF
	git add -A
	git commit -q -m "add capitalized struct field using a public IP literal"
)
head_case4=$(cd "$tmp_repo" && git rev-parse HEAD)

if run_check "$head_case4" "$out_dir/out4.txt"; then
	cat "$out_dir/out4.txt" >&2
	fail "public IPv4 literal was not flagged (DEC-040 regression)"
fi
grep -q "203.0.113.5" "$out_dir/out4.txt" ||
	fail "violation output did not name the public IP literal"

# --- Case 5: a site named only in a commit message (no diff content at ---
# all) must be flagged -- AGENT.md section 2 names commit messages
# explicitly, alongside diff content. The keyword and the hostname are kept
# on separate shell lines/variables here (only combined into one line by
# shell expansion at test-run time, inside the message actually committed to
# the scratch repo) so this test's own source never puts a keyword and a
# hostname-shaped string on the same line of this file -- see this repo's
# own indexer-hostnames CI job, which would otherwise flag this file for
# exactly the pattern this test constructs.
(
	cd "$tmp_repo"
	git checkout -q "$base_sha"
	echo "placeholder2" >internal/indexer/fixture/keep2.go
	git add -A
	msg_keyword="base_url"
	msg_host="https://some-message-only-host.zzz"
	git commit -q -m "docs: point the indexer ${msg_keyword} at ${msg_host}"
)
head_case5=$(cd "$tmp_repo" && git rev-parse HEAD)

if run_check "$head_case5" "$out_dir/out5.txt"; then
	cat "$out_dir/out5.txt" >&2
	fail "a hostname named only in a commit message was not flagged"
fi
grep -q "some-message-only-host.zzz" "$out_dir/out5.txt" ||
	fail "violation output did not name the commit-message-only hostname"

# --- T-9026: capitalised final labels ------------------------------------
# A Go selector expression on a url/host-style key (`URL: srv.URL`,
# `var baseURL = ix.URL`) reads as a hostname to the key/value shape. The scanner
# now skips a candidate whose final label has an uppercase letter in the
# ORIGINAL text (DEC-141). Every identifier below is invented.

# Case 6: selector expressions in a gated file no longer flag.
(
	cd "$tmp_repo"
	git checkout -q "$base_sha"
	cat >internal/indexer/fixture/case_selectors.go <<'EOF'
package fixture

func wire(ix, srv, a, pkg holder) []string {
	var baseURL = ix.URL
	feed := struct{ URL, Host, Endpoint string }{
		URL:      srv.URL,
		Host:     pkg.FeedURL,
		Endpoint: a.baseURL,
	}
	return []string{baseURL, feed.URL, feed.Host, feed.Endpoint}
}
EOF
	git add -A
	git commit -q -m "add selector expressions on url-style keys"
)
head_case6=$(cd "$tmp_repo" && git rev-parse HEAD)

if ! run_check "$head_case6" "$out_dir/out6.txt"; then
	cat "$out_dir/out6.txt" >&2
	fail "Go selector expressions (ix.URL, srv.URL, pkg.FeedURL, a.baseURL) were flagged as hostnames"
fi

# Case 7: the same selector shape in a commit message no longer flags. The
# keyword and the selector are joined only at run time, the same convention
# as case 5.
(
	cd "$tmp_repo"
	git checkout -q "$base_sha"
	echo "placeholder7" >internal/indexer/fixture/keep7.go
	git add -A
	msg_keyword="indexer"
	msg_selector="url = ix.URL"
	git commit -q -m "fix: the ${msg_keyword} reads ${msg_selector} and base_url = srv.FeedURL"
)
head_case7=$(cd "$tmp_repo" && git rev-parse HEAD)

if ! run_check "$head_case7" "$out_dir/out7.txt"; then
	cat "$out_dir/out7.txt" >&2
	fail "Go selector expressions in a commit message were flagged as hostnames"
fi

# Case 8: still flagged -- an all-lowercase hostname, a lowercase selector
# (the rule is case, not Go syntax), and a mixed-case hostname whose final
# label is lowercase, in both the key/value and the scheme shape.
(
	cd "$tmp_repo"
	git checkout -q "$base_sha"
	cat >internal/indexer/fixture/case_still_flagged.go <<'EOF'
package fixture

var still = struct {
	Host, BaseURL, Mirror, Feed string
}{
	Host:    "lower-invented.example.zzz",
	BaseURL: "https://Mixed-Invented.Example.zzz/api",
	Mirror:  "HTTPS://SHOUTED-INVENTED.EXAMPLE.zzz/",
}

func lower(ix holder) string { var url = ix.feedurl; return url }

// Two candidates on one line: the second is cut at the right offset.
var pair = struct{ URL, Host string }{URL: srv.URL, Host: "second-invented.example.zzz"}
var pairs = "https://Skip-Invented.example.ZZZ/ https://third-invented.example.zzz/"
EOF
	git add -A
	git commit -q -m "add lowercase and mixed-case hostnames"
)
head_case8=$(cd "$tmp_repo" && git rev-parse HEAD)

if run_check "$head_case8" "$out_dir/out8.txt"; then
	cat "$out_dir/out8.txt" >&2
	fail "lowercase or lowercase-final-label hostnames were not flagged"
fi
for want in lower-invented.example.zzz mixed-invented.example.zzz shouted-invented.example.zzz \
	ix.feedurl second-invented.example.zzz third-invented.example.zzz; do
	grep -q "possible new indexer hostname: $want\$" "$out_dir/out8.txt" || {
		cat "$out_dir/out8.txt" >&2
		fail "violation output did not name $want"
	}
done
grep -q "skip-invented\|srv.url" "$out_dir/out8.txt" && {
	cat "$out_dir/out8.txt" >&2
	fail "an uppercase-final-label candidate on a shared line was flagged"
}
[ "$(grep -c 'possible new indexer hostname:' "$out_dir/out8.txt")" -eq 6 ] || {
	cat "$out_dir/out8.txt" >&2
	fail "expected exactly 6 violations in case 8"
}

# Case 9: a mixed-case reserved-TLD placeholder with a lowercase final label
# is still auto-allowed exactly as before (the skip rule never runs for it).
(
	cd "$tmp_repo"
	git checkout -q "$base_sha"
	cat >internal/indexer/fixture/case_mixed_reserved.go <<'EOF'
package fixture

var reserved = struct{ Host string }{Host: "Evil.example.net"}
EOF
	git add -A
	git commit -q -m "add a mixed-case reserved placeholder"
)
head_case9=$(cd "$tmp_repo" && git rev-parse HEAD)

if ! run_check "$head_case9" "$out_dir/out9.txt"; then
	cat "$out_dir/out9.txt" >&2
	fail "a mixed-case reserved placeholder was flagged"
fi

# Case 10: the owner-accepted residual gap (DEC-141), pinned so any change to
# it is deliberate -- a hostname whose final label has ANY uppercase letter
# passes, even with a non-reserved TLD.
(
	cd "$tmp_repo"
	git checkout -q "$base_sha"
	cat >internal/indexer/fixture/case_gap.go <<'EOF'
package fixture

var gap = struct{ Host, BaseURL string }{
	Host:    "gap-invented.example.zZz",
	BaseURL: "https://gap-invented.example.ZZZ/",
}
EOF
	git add -A
	git commit -q -m "add hostnames with an uppercase final label"
)
head_case10=$(cd "$tmp_repo" && git rev-parse HEAD)

if ! run_check "$head_case10" "$out_dir/out10.txt"; then
	cat "$out_dir/out10.txt" >&2
	fail "a hostname with an uppercase final label was flagged (DEC-141 gap changed)"
fi

echo "check-indexer-hostnames_test: all cases passed"
