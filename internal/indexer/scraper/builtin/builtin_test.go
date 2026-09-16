package builtin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/indexer/httpx"
	"github.com/kdta91/tortui/internal/indexer/scraper"
)

// internetArchiveID is the id the shipped Internet Archive definition
// declares. Asserted against directly so a rename of the file (which would
// not change the id) or of the id (which would) both get caught by name
// rather than by a test silently finding zero definitions.
const internetArchiveID = "internet-archive"

// TestDefinitionsParsesEveryBundledFile asserts every embedded file
// decodes and validates, and that today's one bundled source is present.
// A future definition added to definitions/ without also being wired into
// docs/bundled-sources.md and the hostname allowlist would still pass this
// test — those are reviewed by hand and by
// scripts/check-indexer-hostnames.sh, not by this package.
func TestDefinitionsParsesEveryBundledFile(t *testing.T) {
	t.Parallel()

	defs, err := Definitions()
	if err != nil {
		t.Fatalf("Definitions: %v", err)
	}

	if len(defs) == 0 {
		t.Fatal("Definitions returned none; expected at least the bundled Internet Archive source")
	}

	var found bool

	for _, def := range defs {
		if err := def.Validate(); err != nil {
			t.Errorf("definition %s does not validate: %v", def.ID, err)
		}

		if def.ID == internetArchiveID {
			found = true
		}
	}

	if !found {
		t.Errorf("Definitions did not include %q", internetArchiveID)
	}
}

// TestDefinitionsIsOrderedByID asserts the ordering guarantee the doc
// comment makes, so a caller iterating the result never depends on
// filesystem order.
func TestDefinitionsIsOrderedByID(t *testing.T) {
	t.Parallel()

	defs, err := Definitions()
	if err != nil {
		t.Fatalf("Definitions: %v", err)
	}

	for i := 1; i < len(defs); i++ {
		if defs[i-1].ID > defs[i].ID {
			t.Fatalf("not ordered by id: %q before %q", defs[i-1].ID, defs[i].ID)
		}
	}
}

// TestEveryBundledSourceSupportsBothModes is the T-024 acceptance
// criterion made executable: a bundled definition with only a search block
// or only a latest block would leave a fresh install with an empty first
// screen in one of the two modes tortui's search screen offers.
func TestEveryBundledSourceSupportsBothModes(t *testing.T) {
	t.Parallel()

	defs, err := Definitions()
	if err != nil {
		t.Fatalf("Definitions: %v", err)
	}

	for _, def := range defs {
		a, err := scraper.New(scraper.Options{Definition: def})
		if err != nil {
			t.Fatalf("New(%s): %v", def.ID, err)
		}

		caps := a.Caps()
		if !caps.Search {
			t.Errorf("%s: Caps.Search = false, every bundled source must support keyword search", def.ID)
		}

		if !caps.Latest {
			t.Errorf("%s: Caps.Latest = false, every bundled source must support a recent-additions feed", def.ID)
		}
	}
}

// internetArchiveJSON is a two-item response shaped exactly like the
// Internet Archive's own Advanced Search API
// (https://archive.org/advancedsearch.php?output=json), captured from a
// live request during T-024 and reduced to the fields the bundled
// definition maps. See docs/bundled-sources.md for how this was verified.
const internetArchiveJSON = `{
	"response": {
		"numFound": 2,
		"docs": [
			{
				"identifier": "vidspeed-e2a28f7c",
				"title": "vidspeed-e2a28f7c",
				"btih": "3e985462e61352e83f502342eb057643c1c90afa",
				"item_size": 419084115,
				"publicdate": "2026-09-16T10:09:43Z"
			},
			{
				"identifier": "fulltext-01_202609",
				"title": "The Making of Gendered Bodies in Human-Robot Interactions",
				"btih": "e96c025b89830ac40a489a11a0bfd9f72b7fca26",
				"item_size": 13000448,
				"publicdate": "2026-09-16T09:26:56Z"
			}
		]
	}
}`

// internetArchiveAdapter starts a fixture server standing in for
// archive.org, points a copy of the bundled Internet Archive definition at
// it (mutating only BaseURL — the field a definition's own source never
// carries a credential in, DEC-074 — leaves the selectors under test
// exactly as shipped), and builds an adapter from it.
func internetArchiveAdapter(t *testing.T) *scraper.Adapter {
	t.Helper()

	defs, err := Definitions()
	if err != nil {
		t.Fatalf("Definitions: %v", err)
	}

	var def *scraper.Definition

	for _, d := range defs {
		if d.ID == internetArchiveID {
			def = d
		}
	}

	if def == nil {
		t.Fatalf("no %q definition among %d bundled definitions", internetArchiveID, len(defs))
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/advancedsearch.php" {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(internetArchiveJSON))
	}))
	t.Cleanup(server.Close)

	addr := server.URL
	def.BaseURL = addr

	cfg := httpx.Config{MinHostInterval: -1, MaxAttempts: 1}

	a, err := scraper.New(scraper.Options{Definition: def, Client: httpx.New(cfg)})
	if err != nil {
		t.Fatalf("scraper.New: %v", err)
	}

	return a
}

// TestInternetArchiveSearchMapsResultFields drives the shipped definition
// exactly as the registry would, against a fixture answer shaped like the
// real Advanced Search API, and asserts every field the definition maps
// onto indexer.Result.
func TestInternetArchiveSearchMapsResultFields(t *testing.T) {
	t.Parallel()

	a := internetArchiveAdapter(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results, err := a.Search(ctx, indexer.Query{Mode: indexer.ModeSearch, Text: "gendered bodies"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	got := results[0]

	if got.IndexerID != internetArchiveID {
		t.Errorf("IndexerID = %q, want %q", got.IndexerID, internetArchiveID)
	}

	if got.ID != "vidspeed-e2a28f7c" {
		t.Errorf("ID = %q, want the item identifier", got.ID)
	}

	if got.Title != "vidspeed-e2a28f7c" {
		t.Errorf("Title = %q", got.Title)
	}

	const wantHash = "3e985462e61352e83f502342eb057643c1c90afa"
	if got.InfoHash != wantHash {
		t.Errorf("InfoHash = %q, want %q", got.InfoHash, wantHash)
	}

	if got.SizeBytes != 419084115 {
		t.Errorf("SizeBytes = %d, want 419084115", got.SizeBytes)
	}

	wantPublished := time.Date(2026, time.September, 16, 10, 9, 43, 0, time.UTC)
	if !got.Published.Equal(wantPublished) {
		t.Errorf("Published = %v, want %v", got.Published, wantPublished)
	}

	if got.Trust != indexer.TrustUnknown {
		t.Errorf("Trust = %v, want TrustUnknown: this source publishes no uploader trust metadata", got.Trust)
	}

	// Resolve derives a magnet from the infohash: this is what makes the
	// result actionable end to end without the definition needing a
	// magnet or torrent_url field the search API does not return.
	resolved, err := a.Resolve(ctx, got)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if !strings.HasPrefix(resolved.Magnet, "magnet:?xt=urn:btih:"+wantHash) {
		t.Errorf("Resolve did not derive a usable magnet: %q", resolved.Magnet)
	}
}

// TestInternetArchiveLatestUsesRecentAdditionsFeed asserts the latest block
// is reachable through the same Caps.Latest path the registry uses for a
// ModeLatest query, and that it requires no keyword.
func TestInternetArchiveLatestUsesRecentAdditionsFeed(t *testing.T) {
	t.Parallel()

	a := internetArchiveAdapter(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	results, err := a.Search(ctx, indexer.Query{Mode: indexer.ModeLatest})
	if err != nil {
		t.Fatalf("Search (latest): %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
}

// TestMergeUserDefinitionOverridesBundledID is the T-024 acceptance
// criterion that a user-supplied definition with the same id as a bundled
// one wins.
func TestMergeUserDefinitionOverridesBundledID(t *testing.T) {
	t.Parallel()

	bundled := []*scraper.Definition{
		{ID: "internet-archive", Name: "Internet Archive"},
		{ID: "only-bundled", Name: "Only bundled"},
	}
	user := []*scraper.Definition{
		{ID: "internet-archive", Name: "User override"},
		{ID: "only-user", Name: "Only user"},
	}

	got := merge(bundled, user)

	if len(got) != 3 {
		t.Fatalf("got %d definitions, want 3 (one overridden, one bundled-only, one user-only)", len(got))
	}

	byID := make(map[string]*scraper.Definition, len(got))
	for _, def := range got {
		byID[def.ID] = def
	}

	if def := byID["internet-archive"]; def == nil || def.Name != "User override" {
		t.Errorf("internet-archive was not overridden by the user definition: %+v", def)
	}

	if def := byID["only-bundled"]; def == nil {
		t.Error("only-bundled dropped from the merge")
	}

	if def := byID["only-user"]; def == nil {
		t.Error("only-user dropped from the merge")
	}
}

// TestMergeIsOrderedByID matches the ordering guarantee Definitions makes.
func TestMergeIsOrderedByID(t *testing.T) {
	t.Parallel()

	got := merge(
		[]*scraper.Definition{{ID: "zzz-bundled"}, {ID: "aaa-bundled"}},
		[]*scraper.Definition{{ID: "mmm-user"}},
	)

	for i := 1; i < len(got); i++ {
		if got[i-1].ID > got[i].ID {
			t.Fatalf("not ordered by id: %q before %q", got[i-1].ID, got[i].ID)
		}
	}
}

// TestMergeWithNoUserDefinitionsReturnsBundledOnly is the ordinary state of
// a fresh install: no definitions directory, no user-supplied source, and
// every bundled source still present.
func TestMergeWithNoUserDefinitionsReturnsBundledOnly(t *testing.T) {
	t.Parallel()

	got, err := Merge(nil)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	bundled, err := Definitions()
	if err != nil {
		t.Fatalf("Definitions: %v", err)
	}

	if len(got) != len(bundled) {
		t.Fatalf("Merge(nil) returned %d definitions, want %d (the bundled set unchanged)", len(got), len(bundled))
	}
}
