package fake

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/indexer"
)

func TestIndexerSearchReturnsFixtures(t *testing.T) {
	ix := New("x", "X", indexer.Caps{Search: true}, ArchiveResults())

	got, err := ix.Search(context.Background(), indexer.Query{Mode: indexer.ModeSearch, Text: "anything"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != len(ArchiveResults()) {
		t.Fatalf("got %d results, want %d", len(got), len(ArchiveResults()))
	}
}

func TestIndexerSearchReturnsDefensiveCopy(t *testing.T) {
	ix := New("x", "X", indexer.Caps{Search: true}, ArchiveResults())

	got, err := ix.Search(context.Background(), indexer.Query{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	got[0].Title = "mutated"

	got2, _ := ix.Search(context.Background(), indexer.Query{})
	if got2[0].Title == "mutated" {
		t.Fatalf("Search result aliases the fixture backing array")
	}
}

func TestIndexerSearchHonoursMinSeeders(t *testing.T) {
	ix := New("x", "X", indexer.Caps{Search: true}, ArchiveResults())

	got, err := ix.Search(context.Background(), indexer.Query{MinSeeders: 50})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range got {
		if r.Seeders < 50 {
			t.Fatalf("result %q has %d seeders, below MinSeeders", r.ID, r.Seeders)
		}
	}
	if len(got) == 0 || len(got) == len(ArchiveResults()) {
		t.Fatalf("MinSeeders filter did not change the result count (%d of %d)", len(got), len(ArchiveResults()))
	}
}

func TestIndexerSearchHonoursLimit(t *testing.T) {
	ix := New("x", "X", indexer.Caps{Search: true}, ArchiveResults())

	got, err := ix.Search(context.Background(), indexer.Query{Limit: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
}

func TestIndexerSearchHonoursCancelledContext(t *testing.T) {
	ix := New("x", "X", indexer.Caps{Search: true}, ArchiveResults())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := ix.Search(ctx, indexer.Query{}); err == nil {
		t.Fatal("Search with a cancelled context returned nil error")
	}
}

func TestFailingIndexerAlwaysFails(t *testing.T) {
	ix := NewFailing("x", "X", nil)

	if _, err := ix.Search(context.Background(), indexer.Query{}); !errors.Is(err, ErrDemoSourceDown) {
		t.Fatalf("Search error = %v, want wrapping ErrDemoSourceDown", err)
	}
}

func TestFailingIndexerCustomError(t *testing.T) {
	custom := errors.New("custom failure")
	ix := NewFailing("x", "X", custom)

	if _, err := ix.Search(context.Background(), indexer.Query{}); !errors.Is(err, custom) {
		t.Fatalf("Search error = %v, want wrapping custom", err)
	}
}

func TestFailingIndexerDeclaresRealCaps(t *testing.T) {
	ix := NewFailing("x", "X", nil)
	if !ix.Caps().Search || !ix.Caps().Latest {
		t.Fatalf("Caps() = %+v, want Search and Latest both true (a failing source is not an unsupported one)", ix.Caps())
	}
}

func TestResolveIsNoOp(t *testing.T) {
	ix := New("x", "X", indexer.Caps{}, ArchiveResults())
	in := indexer.Result{ID: "a", Magnet: "magnet:?xt=urn:btih:abc"}

	out, err := ix.Resolve(context.Background(), in)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if out.ID != in.ID || out.Magnet != in.Magnet {
		t.Fatalf("Resolve(r) = %+v, want r unchanged (%+v)", out, in)
	}
}

func TestResolveHonoursCancelledContext(t *testing.T) {
	ix := New("x", "X", indexer.Caps{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := ix.Resolve(ctx, indexer.Result{}); err == nil {
		t.Fatal("Resolve with a cancelled context returned nil error")
	}
}

func TestArchiveResultsSpanEveryTrustValue(t *testing.T) {
	want := map[indexer.Trust]bool{
		indexer.TrustUnknown:  false,
		indexer.TrustNone:     false,
		indexer.TrustVerified: false,
		indexer.TrustTrusted:  false,
		indexer.TrustVIP:      false,
	}

	for _, r := range ArchiveResults() {
		want[r.Trust] = true
	}

	for trust, seen := range want {
		if !seen {
			t.Errorf("ArchiveResults never uses Trust %v", trust)
		}
	}
}

func TestArchiveResultsHaveAZeroSeederEntry(t *testing.T) {
	for _, r := range ArchiveResults() {
		if r.Seeders == 0 {
			return
		}
	}
	t.Fatal("ArchiveResults has no zero-seeder entry")
}

func TestArchiveResultsHaveAHugeSize(t *testing.T) {
	const huge = 1_000_000_000_000 // 1 TB

	for _, r := range ArchiveResults() {
		if r.SizeBytes >= huge {
			return
		}
	}
	t.Fatal("ArchiveResults has no huge-size entry")
}

func TestMirrorResultsHaveATinySize(t *testing.T) {
	const tiny = 4096

	for _, r := range MirrorResults() {
		if r.SizeBytes > 0 && r.SizeBytes < tiny {
			return
		}
	}
	t.Fatal("MirrorResults has no tiny-size entry")
}

func TestFixturesIncludeWideCJKAndEmojiTitles(t *testing.T) {
	var hasCJK, hasEmoji bool

	for _, r := range append(ArchiveResults(), MirrorResults()...) {
		for _, ru := range r.Title {
			switch {
			case ru >= 0x3040 && ru <= 0x30FF, ru >= 0x4E00 && ru <= 0x9FFF:
				hasCJK = true
			case ru >= 0x1F300 && ru <= 0x1FAFF:
				hasEmoji = true
			}
		}
	}

	if !hasCJK {
		t.Error("no fixture title contains a CJK character")
	}
	if !hasEmoji {
		t.Error("no fixture title contains an emoji character")
	}
}

// reservedFixtureHostSuffixes lists the only hostname labels a demo fixture's
// SourceURL may end in. These are the RFC 2606 / RFC 6761 reserved
// domains — structurally guaranteed to never resolve to a real, operable
// site — plus "localhost". This is a positive allowlist, not a blocklist of
// real sites: AGENT.md §2/§16 forbid naming an infringement-oriented site
// anywhere in this repository, including in a test's forbidden-token list,
// so this test can never enumerate the thing it is guarding against. Instead
// it asserts the fixtures only ever point at addresses that cannot be a real
// site at all — the same approach scripts/check-indexer-hostnames.sh uses
// for the same reason.
var reservedFixtureHostSuffixes = []string{
	"example.com", "example.net", "example.org",
	"example", "test", "invalid", "localhost",
}

// TestFixtureSourceURLsUseOnlyReservedDomains guards against a fixture ever
// pointing SourceURL at a real, resolvable domain — whether a real media
// site, a real indexer, or anything else. A real hostname has no business
// appearing in demo/test data regardless of what it is, so this check needs
// no list of specific sites to reject.
func TestFixtureSourceURLsUseOnlyReservedDomains(t *testing.T) {
	for _, r := range append(ArchiveResults(), MirrorResults()...) {
		u, err := url.Parse(r.SourceURL)
		if err != nil {
			t.Errorf("fixture %q: SourceURL %q does not parse: %v", r.ID, r.SourceURL, err)
			continue
		}
		host := strings.ToLower(u.Hostname())
		if !hostOnReservedSuffix(host) {
			t.Errorf("fixture %q: SourceURL host %q is not a reserved (RFC 2606) domain — demo fixtures must never point at a real, resolvable host", r.ID, host)
		}
	}
}

func hostOnReservedSuffix(host string) bool {
	for _, suffix := range reservedFixtureHostSuffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

// TestFixtureTitlesCarryNoEmbeddedURL guards against a fixture title
// smuggling in a real hostname via free text rather than SourceURL — the
// title fields are prose (dataset/collection names), so a scheme prefix or a
// bare "www." is never legitimate content there.
func TestFixtureTitlesCarryNoEmbeddedURL(t *testing.T) {
	for _, r := range append(ArchiveResults(), MirrorResults()...) {
		lower := strings.ToLower(r.Title)
		if strings.Contains(lower, "://") || strings.Contains(lower, "www.") {
			t.Errorf("fixture %q: Title %q appears to embed a URL/hostname", r.ID, r.Title)
		}
	}
}

func TestEveryFixtureValidates(t *testing.T) {
	for _, r := range append(ArchiveResults(), MirrorResults()...) {
		if err := r.Validate(); err != nil {
			t.Errorf("fixture %q: %v", r.ID, err)
		}
	}
}

func TestFixturePublishedTimesAreDeterministic(t *testing.T) {
	a1 := ArchiveResults()
	time.Sleep(0) // no-op: emphasises there is no wall-clock dependency
	a2 := ArchiveResults()

	for i := range a1 {
		if !a1[i].Published.Equal(a2[i].Published) {
			t.Fatalf("ArchiveResults()[%d].Published is not deterministic", i)
		}
	}
}
