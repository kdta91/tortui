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

# T-9026: run every case under a UTF-8 locale when the host has one, since
# that is where macOS awk counts characters and tolower() can shrink a
# multibyte letter. The scanner must pin its own locale to stay correct.
utf8_locale=$(locale -a 2>/dev/null | grep -i -x -e 'en_US\.UTF-8' -e 'en_US\.utf8' -e 'C\.UTF-8' -e 'C\.utf8' | head -n 1 || true)

run_check() {
	# $1 = head sha, $2 = output file. Never let set -e kill the script on a
	# nonzero (expected) exit -- the caller inspects the captured status.
	head="$1"
	out="$2"
	set +e
	(
		cd "$tmp_repo" || exit 1
		if [ -n "$utf8_locale" ]; then
			LC_ALL=$utf8_locale
			export LC_ALL
		fi
		INDEXER_HOSTNAME_ALLOWLIST="$ALLOWLIST" "$CHECK_SCRIPT" "$base_sha" "$head"
	) >"$out" 2>&1
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
# now skips a value that, as written, is exactly a bare two-part selector
# whose second part has an uppercase letter (DEC-141). Every identifier below
# is invented.

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

# Case 8: still flagged, exactly as before T-9026 -- the skip only ever
# applies to a bare two-part selector value, so every hostname below is
# checked the way it always was, whatever its case or trailing text.
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

// Two candidates on one line: the selector is skipped, the host is not.
var pair = struct{ URL, Host string }{URL: srv.URL, Host: "second-invented.example.zzz"}
var pairs = "https://Skip-Invented.example.ZZZ/ https://third-invented.example.zzz/"

// Scheme shape: never skipped, whatever follows or precedes the host.
var query = "https://query-invented.example.zzz?ApiKey=1"
var frag = "https://frag-invented.example.zzz#Section"
var tmpl = "https://tmpl-invented.example.zzz{{.Path}}"
var atq = "https://atq-invented.example.zzz?u=a@Other"
var amp = "https://amp-invented.example.zzz&X=1"
var dollar = "https://dollar-invented.example.zzz${Path}"
var wild = "https://*.wild-invented.example.zzz,Next"
var paren = "https://(paren-invented.example.zzz)Then"
var dotdot = "https://dotdot-invented.example.zzz..Next"
var nonascii = "https://ÄÄ.nonascii-invented.example.ZZZ/"
var under = "https://My_Tracker.under-invented.example.zzz/"
var dd = "https://A..dd-invented.example.zzz/"
var star = "https://X*star-invented.example.zzz/"

// Key/value shape: a second dot, a port or a quote is never a bare selector.
var kvdd = struct{ Host string }{Host: "A..kvdd-invented.example.zzz"}
var port = struct{ Host string }{Host: cfg.Addr:8080}
var deep = struct{ URL string }{URL: ix.URL.Host}

// İİİİİİİİ ẞẞ K URL: srv.URL, Host: "multibyte-invented.example.zzz"
EOF
	git add -A
	git commit -q -m "add hostnames that must stay flagged"
)
head_case8=$(cd "$tmp_repo" && git rev-parse HEAD)

if run_check "$head_case8" "$out_dir/out8.txt"; then
	cat "$out_dir/out8.txt" >&2
	fail "hostnames that must stay flagged were not flagged"
fi
grep 'possible new indexer hostname:' "$out_dir/out8.txt" >"$out_dir/names8.txt" || true
# Names are printed the way the scanner always printed them (the whole
# lowercased authority), so match the host as a fixed substring. atq's
# "?u=a@Other" is read as userinfo, so it is reported as "other" (T-9029).
for want in lower-invented.example.zzz mixed-invented.example.zzz shouted-invented.example.zzz \
	ix.feedurl second-invented.example.zzz skip-invented.example.zzz third-invented.example.zzz \
	query-invented.example.zzz frag-invented.example.zzz tmpl-invented.example.zzz \
	': other' amp-invented.example.zzz dollar-invented.example.zzz \
	wild-invented.example.zzz paren-invented.example.zzz dotdot-invented.example.zzz \
	nonascii-invented.example.zzz my_tracker.under-invented.example.zzz \
	a..dd-invented.example.zzz 'x*star-invented.example.zzz' a..kvdd-invented.example.zzz \
	cfg.addr ix.url.host multibyte-invented.example.zzz; do
	grep -qF -e "$want" "$out_dir/names8.txt" || {
		cat "$out_dir/out8.txt" >&2
		fail "violation output did not name $want"
	}
done
grep -qF "srv.url" "$out_dir/names8.txt" && {
	cat "$out_dir/out8.txt" >&2
	fail "a bare srv.URL selector on a shared line was flagged"
}
[ "$(wc -l <"$out_dir/names8.txt")" -eq 24 ] || {
	cat "$out_dir/out8.txt" >&2
	fail "expected exactly 24 violations in case 8"
}

# Case 9: a mixed-case reserved-TLD placeholder is still auto-allowed
# exactly as before.
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

# Case 10: a capitalised final label does NOT buy a pass anywhere but a bare
# selector. A URL, a quoted value (even one shaped exactly like a selector)
# and a multi-label value with an uppercase final label are all FLAGGED
# (DEC-141 narrowed the earlier, wider gap).
(
	cd "$tmp_repo"
	git checkout -q "$base_sha"
	cat >internal/indexer/fixture/case_upper_final.go <<'EOF'
package fixture

var upper = struct{ Host, BaseURL, Endpoint, URL string }{
	Host:     "quoted-invented.example.zZz",
	BaseURL:  "https://url-invented.example.ZZZ/",
	Endpoint: "quotedtwolabel.Zzz",
	URL:      multi.label-invented.Zzz,
}
EOF
	git add -A
	git commit -q -m "add hostnames with an uppercase final label"
)
head_case10=$(cd "$tmp_repo" && git rev-parse HEAD)

if run_check "$head_case10" "$out_dir/out10.txt"; then
	cat "$out_dir/out10.txt" >&2
	fail "a URL, quoted or multi-label hostname with an uppercase final label passed"
fi
for want in quoted-invented.example.zzz url-invented.example.zzz quotedtwolabel.zzz \
	multi.label-invented.zzz; do
	grep 'possible new indexer hostname:' "$out_dir/out10.txt" | grep -qF -e "$want" || {
		cat "$out_dir/out10.txt" >&2
		fail "violation output did not name $want"
	}
done

# Case 11: the query, fragment, template, "*.", "(" and "_" forms in a commit
# message are still flagged. Keyword and hosts are joined at run time, the
# same convention as case 5.
(
	cd "$tmp_repo"
	git checkout -q "$base_sha"
	echo "placeholder11" >internal/indexer/fixture/keep11.go
	git add -A
	msg_keyword="indexer"
	msg_q="https://msg-query-invented.example.zzz?Feed=rss"
	msg_f="https://msg-frag-invented.example.zzz#Top"
	msg_t="https://msg-tmpl-invented.example.zzz{{.Keywords}}"
	msg_w="https://*.msg-wild-invented.example.zzz,Next"
	msg_p="https://(msg-paren-invented.example.zzz)Then"
	msg_u="https://My_Tracker.msg-under-invented.example.zzz/rss"
	git commit -q -m "feat: the ${msg_keyword} reads ${msg_q}, ${msg_f}, ${msg_t}, ${msg_w}, ${msg_p} and ${msg_u}"
)
head_case11=$(cd "$tmp_repo" && git rev-parse HEAD)

if run_check "$head_case11" "$out_dir/out11.txt"; then
	cat "$out_dir/out11.txt" >&2
	fail "URLs in a commit message were not flagged"
fi
for want in msg-query-invented.example.zzz msg-frag-invented.example.zzz msg-tmpl-invented.example.zzz \
	msg-wild-invented.example.zzz msg-paren-invented.example.zzz my_tracker.msg-under-invented.example.zzz; do
	grep 'possible new indexer hostname:' "$out_dir/out11.txt" | grep -qF -e "$want" || {
		cat "$out_dir/out11.txt" >&2
		fail "commit-message violation output did not name $want"
	}
done

# Case 12: the owner-accepted residual gap (DEC-141), pinned so any change is
# deliberate -- a bare, unquoted two-part value whose second part has an
# uppercase letter reads exactly like a Go selector, so it passes even if it
# is really a two-label hostname.
(
	cd "$tmp_repo"
	git checkout -q "$base_sha"
	cat >internal/indexer/fixture/case_gap.go <<'EOF'
package fixture

var gap = struct{ Host string }{Host: gapinvented.Zzz}
EOF
	git add -A
	git commit -q -m "add a bare two-part value with an uppercase final label"
)
head_case12=$(cd "$tmp_repo" && git rev-parse HEAD)

if ! run_check "$head_case12" "$out_dir/out12.txt"; then
	cat "$out_dir/out12.txt" >&2
	fail "a bare two-part selector-shaped value was flagged (DEC-141 gap changed)"
fi

# Case 13 (T-913): a Go raw string literal -- backtick-delimited, no scheme --
# in a gated path is flagged, and a reserved-TLD one is still allowed.
(
	cd "$tmp_repo"
	git checkout -q "$base_sha"
	cat >internal/indexer/fixture/case_raw.go <<'EOF2'
package fixture

var raw = struct{ Host, Endpoint string }{
	Host:     `raw-invented-source.zzz`,
	Endpoint: `raw-fine-fixture.example`,
}
EOF2
	git add -A
	git commit -q -m "add raw string literal values"
)
head_case13=$(cd "$tmp_repo" && git rev-parse HEAD)

if run_check "$head_case13" "$out_dir/out13.txt"; then
	cat "$out_dir/out13.txt" >&2
	fail "a backtick raw-string hostname was not flagged (T-913)"
fi
grep 'possible new indexer hostname:' "$out_dir/out13.txt" | grep -qF -e "raw-invented-source.zzz" || {
	cat "$out_dir/out13.txt" >&2
	fail "violation output did not name the raw-string hostname"
}
if grep -qF -e "raw-fine-fixture.example" "$out_dir/out13.txt"; then
	cat "$out_dir/out13.txt" >&2
	fail "a reserved-TLD raw-string value was flagged"
fi

# Case 14 (T-9029): a hostname-shaped userinfo is checked and named, not just
# the host after the "@". A plain user name, and a userinfo on an allowed host
# with an allowed name, are not flagged.
(
	cd "$tmp_repo"
	git checkout -q "$base_sha"
	cat >internal/indexer/fixture/case_userinfo.go <<'EOF2'
package fixture

var userinfo = []string{
	"https://userinfo-invented.zzz@other-fine.example/feed",
	"https://plainuser:secret@third-fine.example/feed",
}
EOF2
	git add -A
	git commit -q -m "add userinfo URLs"
)
head_case14=$(cd "$tmp_repo" && git rev-parse HEAD)

if run_check "$head_case14" "$out_dir/out14.txt"; then
	cat "$out_dir/out14.txt" >&2
	fail "a hostname-shaped userinfo was not flagged (T-9029)"
fi
grep 'possible new indexer hostname:' "$out_dir/out14.txt" | grep -qF -e "userinfo-invented.zzz" || {
	cat "$out_dir/out14.txt" >&2
	fail "violation output did not name the userinfo hostname"
}
if grep 'possible new indexer hostname:' "$out_dir/out14.txt" | grep -qF -e "plainuser" -e "other-fine" -e "third-fine"; then
	cat "$out_dir/out14.txt" >&2
	fail "a plain user name or an allowed host was flagged"
fi

echo "check-indexer-hostnames_test: all cases passed"
