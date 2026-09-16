package fake

import (
	"context"
	"testing"

	"github.com/kdta91/tortui/internal/indexer"
)

func TestNewDemoRegistryRegistersThreeSources(t *testing.T) {
	reg, err := NewDemoRegistry()
	if err != nil {
		t.Fatalf("NewDemoRegistry: %v", err)
	}

	if got := len(reg.List()); got != 3 {
		t.Fatalf("len(reg.List()) = %d, want 3", got)
	}
}

// TestNewDemoRegistrySearchAllDegradesOnFailingSource is the direct proof
// that the fixture registry exercises AGENT.md §6.3's "one failing indexer
// degrades, never crashes" path end to end: two of three sources succeed,
// one fails, and SearchAll still returns a nil error and merged results
// from the sources that worked, plus exactly one SourceError for the one
// that didn't.
func TestNewDemoRegistrySearchAllDegradesOnFailingSource(t *testing.T) {
	reg, err := NewDemoRegistry()
	if err != nil {
		t.Fatalf("NewDemoRegistry: %v", err)
	}

	results, srcErrs, err := reg.SearchAll(context.Background(), indexer.Query{Mode: indexer.ModeSearch, Text: "x"})
	if err != nil {
		t.Fatalf("SearchAll returned a fatal error with two sources still working: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("SearchAll returned no results from the two working sources")
	}

	if len(srcErrs) != 1 {
		t.Fatalf("len(srcErrs) = %d, want 1 (only %q should fail)", len(srcErrs), FailingIndexerID)
	}

	if srcErrs[0].IndexerID != FailingIndexerID {
		t.Fatalf("srcErrs[0].IndexerID = %q, want %q", srcErrs[0].IndexerID, FailingIndexerID)
	}

	if srcErrs[0].Skipped {
		t.Fatal("the failing source is reported as Skipped, not Failed — it declares real Caps and should be queried and fail")
	}
}

func TestNewDemoRegistryLatestSkipsNonLatestSource(t *testing.T) {
	reg, err := NewDemoRegistry()
	if err != nil {
		t.Fatalf("NewDemoRegistry: %v", err)
	}

	_, srcErrs, err := reg.SearchAll(context.Background(), indexer.Query{Mode: indexer.ModeLatest})
	if err != nil {
		t.Fatalf("SearchAll: %v", err)
	}

	var sawMirrorSkipped bool
	for _, se := range srcErrs {
		if se.IndexerID == "demo-mirror" && se.Skipped {
			sawMirrorSkipped = true
		}
	}
	if !sawMirrorSkipped {
		t.Fatal("demo-mirror (Caps.Latest == false) was not reported as skipped for a ModeLatest query")
	}
}

func TestNewDemoRegistryResultsSpanEveryTrustValue(t *testing.T) {
	reg, err := NewDemoRegistry()
	if err != nil {
		t.Fatalf("NewDemoRegistry: %v", err)
	}

	results, _, err := reg.SearchAll(context.Background(), indexer.Query{Mode: indexer.ModeSearch, Text: "x", Limit: 100})
	if err != nil {
		t.Fatalf("SearchAll: %v", err)
	}

	seen := map[indexer.Trust]bool{}
	for _, r := range results {
		seen[r.Trust] = true
	}

	for _, want := range []indexer.Trust{
		indexer.TrustUnknown, indexer.TrustNone, indexer.TrustVerified,
		indexer.TrustTrusted, indexer.TrustVIP,
	} {
		if !seen[want] {
			t.Errorf("merged SearchAll results never include Trust %v", want)
		}
	}
}
