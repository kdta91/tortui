package fake

import (
	"context"
	"errors"
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

func TestFixturesNameNoRealMediaOrInfringingSite(t *testing.T) {
	// AGENT.md §2/§16: no infringement-oriented site named anywhere, and
	// fixture titles must not name real-world media. This is a coarse
	// guard, not a legal review — it fails loudly on the one thing that
	// would be a bug rather than a design choice.
	forbidden := []string{"pirate", "thepiratebay", "1337x", "rarbg", "yts", "torrentz"}

	for _, r := range append(ArchiveResults(), MirrorResults()...) {
		for _, bad := range forbidden {
			if containsFold(r.Title, bad) || containsFold(r.SourceURL, bad) {
				t.Fatalf("fixture %q contains forbidden token %q", r.ID, bad)
			}
		}
	}
}

func containsFold(s, substr string) bool {
	sl, subl := []rune(s), []rune(substr)
	toLower := func(rs []rune) []rune {
		out := make([]rune, len(rs))
		for i, r := range rs {
			if r >= 'A' && r <= 'Z' {
				r += 'a' - 'A'
			}
			out[i] = r
		}
		return out
	}
	sl, subl = toLower(sl), toLower(subl)

	if len(subl) == 0 || len(subl) > len(sl) {
		return len(subl) == 0
	}
	for i := 0; i+len(subl) <= len(sl); i++ {
		match := true
		for j := range subl {
			if sl[i+j] != subl[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
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
