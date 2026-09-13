package indexer

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// fakeIndexer exists to prove the Indexer interface is implementable exactly
// as declared, and to pin the Resolve no-op contract in a compile-time check.
// It is not a general-purpose test double: T-012's registry tests bring their
// own fakes. The invented name and example.org URL satisfy AGENT.md §2.
type fakeIndexer struct{}

func (fakeIndexer) ID() string   { return "example-archive" }
func (fakeIndexer) Name() string { return "Example Archive" }

func (fakeIndexer) Caps() Caps {
	return Caps{Search: true, ProvidesMagnet: true}
}

func (f fakeIndexer) Search(_ context.Context, _ Query) ([]Result, error) {
	return []Result{{
		IndexerID: f.ID(),
		ID:        "1",
		Title:     "example dataset",
		Magnet:    testMagnet,
		SourceURL: "https://archive.example.org/items/1",
	}}, nil
}

func (fakeIndexer) Resolve(_ context.Context, r Result) (Result, error) { return r, nil }

var _ Indexer = fakeIndexer{}

// testMagnet is a syntactically shaped but deliberately non-real magnet URI.
const testMagnet = "magnet:?xt=urn:btih:0000000000000000000000000000000000000001"

func TestTrustZeroValueAndIotaOrdering(t *testing.T) {
	t.Parallel()

	// The zero value must be TrustUnknown: a Result built by an adapter that
	// never touches the Trust field must read as "no trust info", not as a
	// positive claim about the uploader.
	var zero Trust
	if zero != TrustUnknown {
		t.Errorf("zero value of Trust = %d, want TrustUnknown (%d)", zero, TrustUnknown)
	}

	// Ordering is load-bearing: AGENT.md §7 allows sorting on the trust
	// column, which sorts on the underlying int, so the constants must run
	// from least to most trusted in the order AGENT.md §5 declares them.
	want := []struct {
		trust Trust
		value int
	}{
		{TrustUnknown, 0},
		{TrustNone, 1},
		{TrustVerified, 2},
		{TrustTrusted, 3},
		{TrustVIP, 4},
	}
	for _, tc := range want {
		if int(tc.trust) != tc.value {
			t.Errorf("Trust constant %q = %d, want %d", tc.trust, int(tc.trust), tc.value)
		}
	}
}

func TestModeZeroValueAndIotaOrdering(t *testing.T) {
	t.Parallel()

	// A zero-value Query is a keyword search, so a caller that forgets to set
	// Mode gets the mode that always requires an explicit Text rather than
	// silently requesting a whole latest feed.
	var zero Mode
	if zero != ModeSearch {
		t.Errorf("zero value of Mode = %d, want ModeSearch (%d)", zero, ModeSearch)
	}
	if int(ModeSearch) != 0 {
		t.Errorf("ModeSearch = %d, want 0", int(ModeSearch))
	}
	if int(ModeLatest) != 1 {
		t.Errorf("ModeLatest = %d, want 1", int(ModeLatest))
	}

	var q Query
	if q.Mode != ModeSearch {
		t.Errorf("zero-value Query.Mode = %d, want ModeSearch", q.Mode)
	}
}

func TestTrustString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		trust Trust
		want  string
	}{
		{TrustUnknown, "unknown"},
		{TrustNone, "none"},
		{TrustVerified, "verified"},
		{TrustTrusted, "trusted"},
		{TrustVIP, "vip"},
	}
	for _, tc := range tests {
		if got := tc.trust.String(); got != tc.want {
			t.Errorf("Trust(%d).String() = %q, want %q", int(tc.trust), got, tc.want)
		}
	}

	// An out-of-range value must stay diagnosable rather than masquerading as
	// a known level or panicking.
	if got := Trust(99).String(); got != "trust(99)" {
		t.Errorf("Trust(99).String() = %q, want %q", got, "trust(99)")
	}
	if got := Trust(-1).String(); got != "trust(-1)" {
		t.Errorf("Trust(-1).String() = %q, want %q", got, "trust(-1)")
	}
}

func TestTrustBadge(t *testing.T) {
	t.Parallel()

	// AGENT.md §7: the results table shows VIP / TR / ✓ / blank, never a word.
	tests := []struct {
		trust Trust
		want  string
	}{
		{TrustUnknown, ""},
		{TrustNone, ""},
		{TrustVerified, "✓"},
		{TrustTrusted, "TR"},
		{TrustVIP, "VIP"},
	}
	for _, tc := range tests {
		if got := tc.trust.Badge(); got != tc.want {
			t.Errorf("Trust(%d).Badge() = %q, want %q", int(tc.trust), got, tc.want)
		}
	}

	// An out-of-range value renders as blank rather than leaking a
	// placeholder string into a fixed-width table column.
	if got := Trust(99).Badge(); got != "" {
		t.Errorf("Trust(99).Badge() = %q, want an empty string", got)
	}
	if got := Trust(-1).Badge(); got != "" {
		t.Errorf("Trust(-1).Badge() = %q, want an empty string", got)
	}
}

func TestResultValidate(t *testing.T) {
	t.Parallel()

	base := Result{IndexerID: "example-archive", ID: "42", Title: "example dataset"}

	withMagnet := base
	withMagnet.Magnet = testMagnet
	if err := withMagnet.Validate(); err != nil {
		t.Errorf("Validate() with a magnet only = %v, want nil", err)
	}

	withURL := base
	withURL.TorrentURL = "https://archive.example.org/items/42.torrent"
	if err := withURL.Validate(); err != nil {
		t.Errorf("Validate() with a torrent URL only = %v, want nil", err)
	}

	withBoth := base
	withBoth.Magnet = testMagnet
	withBoth.TorrentURL = "https://archive.example.org/items/42.torrent"
	if err := withBoth.Validate(); err != nil {
		t.Errorf("Validate() with both = %v, want nil", err)
	}

	// The one rejection the contract requires: neither link present.
	err := base.Validate()
	if err == nil {
		t.Fatal("Validate() with neither a magnet nor a torrent URL = nil, want an error")
	}
	if !errors.Is(err, ErrNoLink) {
		t.Errorf("Validate() error = %v, want it to wrap ErrNoLink", err)
	}
	// AGENT.md §6.9: errors carry context. The message must identify which
	// source produced the unusable result and which result it was.
	if !strings.Contains(err.Error(), "example-archive") || !strings.Contains(err.Error(), "42") {
		t.Errorf("Validate() error = %q, want it to name the indexer id and result id", err)
	}

	// A whitespace-only link is no link at all.
	whitespace := base
	whitespace.Magnet = "   "
	whitespace.TorrentURL = "\t\n"
	if err := whitespace.Validate(); !errors.Is(err, ErrNoLink) {
		t.Errorf("Validate() with whitespace-only links = %v, want ErrNoLink", err)
	}

	// A zero-value Result has no link either, and must not panic on empty ids.
	var zero Result
	if err := zero.Validate(); !errors.Is(err, ErrNoLink) {
		t.Errorf("zero-value Result.Validate() = %v, want ErrNoLink", err)
	}
}

func TestResultZeroValue(t *testing.T) {
	t.Parallel()

	var r Result
	if r.Trust != TrustUnknown {
		t.Errorf("zero-value Result.Trust = %d, want TrustUnknown", r.Trust)
	}
	if r.Extra != nil {
		t.Errorf("zero-value Result.Extra = %v, want nil", r.Extra)
	}
	if !r.Published.IsZero() {
		t.Errorf("zero-value Result.Published = %v, want the zero time", r.Published)
	}
}

func TestCapsZeroValue(t *testing.T) {
	t.Parallel()

	// Every capability defaults to false, so an adapter that forgets to
	// declare one is treated as not having it (and is skipped by the
	// registry) rather than being asked to serve a query it cannot.
	var c Caps
	if c.Search || c.Latest || c.Categories || c.Pagination || c.RequiresAuth || c.ProvidesMagnet {
		t.Errorf("zero-value Caps = %+v, want every field false", c)
	}
}

func TestFakeIndexerSatisfiesContract(t *testing.T) {
	t.Parallel()

	var idx Indexer = fakeIndexer{}
	results, err := idx.Search(context.Background(), Query{Mode: ModeSearch, Text: "dataset"})
	if err != nil {
		t.Fatalf("Search() = %v, want nil", err)
	}
	if len(results) != 1 {
		t.Fatalf("Search() returned %d results, want 1", len(results))
	}
	if err := results[0].Validate(); err != nil {
		t.Errorf("returned result failed Validate(): %v", err)
	}

	// Resolve must be a no-op returning r unchanged when already resolved.
	got, err := idx.Resolve(context.Background(), results[0])
	if err != nil {
		t.Fatalf("Resolve() = %v, want nil", err)
	}
	// Result carries a map field, so it is not comparable with ==.
	if !reflect.DeepEqual(got, results[0]) {
		t.Errorf("Resolve() on an already-resolved result = %+v, want it unchanged", got)
	}
}
