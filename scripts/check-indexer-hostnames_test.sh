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
cleanup() { rm -rf "$tmp_repo"; }
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

if run_check "$head_case1" "$tmp_repo/out1.txt"; then
	cat "$tmp_repo/out1.txt" >&2
	fail "capitalized Host:/BaseURL: struct fields were not flagged (case-sensitivity regression)"
fi
grep -q "some-invented-source-host.zzz" "$tmp_repo/out1.txt" ||
	fail "violation output did not name the capitalized Host: hostname"
grep -q "another-invented-source.zzz" "$tmp_repo/out1.txt" ||
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

if ! run_check "$head_case2" "$tmp_repo/out2.txt"; then
	cat "$tmp_repo/out2.txt" >&2
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

if ! run_check "$head_case3" "$tmp_repo/out3.txt"; then
	cat "$tmp_repo/out3.txt" >&2
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

if run_check "$head_case4" "$tmp_repo/out4.txt"; then
	cat "$tmp_repo/out4.txt" >&2
	fail "public IPv4 literal was not flagged (DEC-040 regression)"
fi
grep -q "203.0.113.5" "$tmp_repo/out4.txt" ||
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

if run_check "$head_case5" "$tmp_repo/out5.txt"; then
	cat "$tmp_repo/out5.txt" >&2
	fail "a hostname named only in a commit message was not flagged"
fi
grep -q "some-message-only-host.zzz" "$tmp_repo/out5.txt" ||
	fail "violation output did not name the commit-message-only hostname"

echo "check-indexer-hostnames_test: all cases passed"
