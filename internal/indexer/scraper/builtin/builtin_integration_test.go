//go:build integration

package builtin

import (
	"context"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/indexer/scraper"
)

// TestInternetArchiveLiveSearchAndLatest hits the real Internet Archive
// Advanced Search API, unmodified, exactly as the registry would. This is
// what T-024 means by "the live-endpoint test is //go:build integration":
// make check (AGENT.md §6.7) never runs it, and it exists so a rotted
// selector or a field the source stopped returning is caught by CI's
// integration job rather than by a user's first search (T-081, T-091).
func TestInternetArchiveLiveSearchAndLatest(t *testing.T) {
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

	a, err := scraper.New(scraper.Options{Definition: def})
	if err != nil {
		t.Fatalf("build adapter: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	t.Run("search", func(t *testing.T) {
		results, err := a.Search(ctx, indexer.Query{Mode: indexer.ModeSearch, Text: "history", Limit: 5})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}

		assertLiveResults(t, results)
	})

	t.Run("latest", func(t *testing.T) {
		results, err := a.Search(ctx, indexer.Query{Mode: indexer.ModeLatest, Limit: 5})
		if err != nil {
			t.Fatalf("Search (latest): %v", err)
		}

		assertLiveResults(t, results)
	})
}

// assertLiveResults checks the shape a live response must have without
// asserting on any specific item, since the Internet Archive's contents
// change continuously.
func assertLiveResults(t *testing.T, results []indexer.Result) {
	t.Helper()

	if len(results) == 0 {
		t.Fatal("got zero results from a live query the Internet Archive should always have something for")
	}

	for _, r := range results {
		if r.Title == "" {
			t.Error("a live result has no title")
		}

		if r.InfoHash == "" {
			t.Errorf("result %q has no infohash: format:\"Archive BitTorrent\" should guarantee one", r.ID)
		}
	}
}
