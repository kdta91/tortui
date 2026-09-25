package tui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/kdta91/tortui/internal/engine/fake"
	"github.com/kdta91/tortui/internal/indexer"
	indexerfake "github.com/kdta91/tortui/internal/indexer/fake"
)

// --- formatting and parsing (no I/O, no teatest) ------------------------

func TestFormatSizeHumanReadable(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}

	for _, c := range cases {
		if got := formatSize(c.n); got != c.want {
			t.Errorf("formatSize(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// TestSizeLessRoundTripsFormatSize confirms sizeLess compares the numeric
// value formatSize renders, not the display text lexically ("1.0 KB" would
// sort after "512 B" as plain strings, which is backwards).
func TestSizeLessRoundTripsFormatSize(t *testing.T) {
	small, big := formatSize(500), formatSize(5_000_000_000)

	if !sizeLess(small, big) {
		t.Fatalf("sizeLess(%q, %q) = false, want true", small, big)
	}

	if sizeLess(big, small) {
		t.Fatalf("sizeLess(%q, %q) = true, want false", big, small)
	}
}

func TestSeedersLessOrdersByLeadingCount(t *testing.T) {
	if !seedersLess("5/1", "50/1") {
		t.Fatal("seedersLess(\"5/1\", \"50/1\") = false, want true")
	}

	if seedersLess("50/1", "5/1") {
		t.Fatal("seedersLess(\"50/1\", \"5/1\") = true, want false")
	}
}

// TestFormatAgeRelativeBuckets pins T-061's acceptance examples ("3h",
// "2d", "1y") plus the zero-time case a source that publishes no date
// produces.
func TestFormatAgeRelativeBuckets(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		at   time.Time
		want string
	}{
		{"zero time", time.Time{}, "-"},
		{"30 seconds", now.Add(-30 * time.Second), "30s"},
		{"5 minutes", now.Add(-5 * time.Minute), "5m"},
		{"3 hours", now.Add(-3 * time.Hour), "3h"},
		{"2 days", now.Add(-48 * time.Hour), "2d"},
		{"1 year", now.AddDate(-1, 0, 0), "1y"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatAge(now, c.at); got != c.want {
				t.Errorf("formatAge(now, %v) = %q, want %q", c.at, got, c.want)
			}
		})
	}
}

// TestAgeLessOrdersByRecency confirms ageLess treats a smaller (more
// recent) age as "less" — the ordering applyModeDefault's ascending Latest
// sort relies on to put the newest item first.
func TestAgeLessOrdersByRecency(t *testing.T) {
	order := []string{"30s", "5m", "3h", "2d", "1y"}

	for i := 0; i < len(order)-1; i++ {
		if !ageLess(order[i], order[i+1]) {
			t.Errorf("ageLess(%q, %q) = false, want true (more recent sorts first)", order[i], order[i+1])
		}

		if ageLess(order[i+1], order[i]) {
			t.Errorf("ageLess(%q, %q) = true, want false", order[i+1], order[i])
		}
	}
}

// TestCacheSummaryAggregatesResults pins cacheSummary's three-way outcome
// (T-061 acceptance: "the status bar shows when results came from cache
// rather than a fresh fetch") against indexer.ExtraKeyCacheHit, the tag
// indexer.Registry's SearchAll actually writes (see registry_test.go's
// TestSearchAllTagsCacheHitOnResults for the registry half of this
// contract).
func TestCacheSummaryAggregatesResults(t *testing.T) {
	cases := []struct {
		name string
		hits []string // "true"/"false" per result's Extra[ExtraKeyCacheHit]
		want string
	}{
		{"no results", nil, ""},
		{"all fresh", []string{"false", "false"}, "fresh"},
		{"all cached", []string{"true", "true"}, "cached"},
		{"mixed", []string{"true", "false"}, "partial cache"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var results []indexer.Result

			for i, v := range c.hits {
				results = append(results, indexer.Result{
					IndexerID: "alpha", ID: strconv.Itoa(i),
					Extra: map[string]string{indexer.ExtraKeyCacheHit: v},
				})
			}

			if got := cacheSummary(results); got != c.want {
				t.Errorf("cacheSummary(%v) = %q, want %q", c.hits, got, c.want)
			}
		})
	}
}

// TestResultsHeaderTextNamesModeAndQuery pins T-061's "Header states the
// current mode and query" acceptance directly.
func TestResultsHeaderTextNamesModeAndQuery(t *testing.T) {
	if got := resultsHeaderText(indexer.Query{Mode: indexer.ModeSearch, Text: "ubuntu"}); !strings.Contains(got, "ubuntu") {
		t.Errorf("resultsHeaderText(Search, %q) = %q, want it to name the query text", "ubuntu", got)
	}

	if got := resultsHeaderText(indexer.Query{Mode: indexer.ModeLatest}); !strings.Contains(strings.ToLower(got), "latest") {
		t.Errorf("resultsHeaderText(Latest) = %q, want it to name Latest mode", got)
	}
}

func sampleResultsForSort(now time.Time) []indexer.Result {
	return []indexer.Result{
		{IndexerID: "alpha", ID: "low", Title: "low seeders", Seeders: 5, Published: now.Add(-72 * time.Hour)},
		{IndexerID: "alpha", ID: "high", Title: "high seeders", Seeders: 50, Published: now.Add(-1 * time.Hour)},
		{IndexerID: "alpha", ID: "mid", Title: "mid seeders", Seeders: 20, Published: now.Add(-24 * time.Hour)},
	}
}

// TestApplyModeDefaultSearchSortsSeedersDescending pins T-061's "Default
// sort follows the mode: seeders desc for Search".
func TestApplyModeDefaultSearchSortsSeedersDescending(t *testing.T) {
	now := time.Now()
	m := newResultsModel().setResults(sampleResultsForSort(now), indexer.ModeSearch, now)

	rows := m.table.Rows()
	if rows[0].ID != resultRowID(sampleResultsForSort(now)[1]) { // "high"
		t.Fatalf("first row = %+v, want the highest-seeder result first", rows[0])
	}

	if rows[len(rows)-1].ID != resultRowID(sampleResultsForSort(now)[0]) { // "low"
		t.Fatalf("last row = %+v, want the lowest-seeder result last", rows[len(rows)-1])
	}
}

// TestApplyModeDefaultLatestSortsAgeAscendingNewestFirst pins T-061's
// "... age ascending (newest first) for Latest".
func TestApplyModeDefaultLatestSortsAgeAscendingNewestFirst(t *testing.T) {
	now := time.Now()
	m := newResultsModel().setResults(sampleResultsForSort(now), indexer.ModeLatest, now)

	rows := m.table.Rows()
	if rows[0].ID != "alpha|high" { // published 1h ago, the most recent
		t.Fatalf("first row = %+v, want the most recently published result first", rows[0])
	}

	if rows[len(rows)-1].ID != "alpha|low" { // published 72h ago, the oldest
		t.Fatalf("last row = %+v, want the oldest result last", rows[len(rows)-1])
	}
}

// TestCycleSortWrapsThroughAllColumns confirms cycleSort visits every
// column exactly once before wrapping back to where it started.
func TestCycleSortWrapsThroughAllColumns(t *testing.T) {
	now := time.Now()
	m := newResultsModel().setResults(sampleResultsForSort(now), indexer.ModeSearch, now)

	start := m.table.SortColumn()
	seen := map[int]bool{start: true}

	for i := 0; i < len(m.table.Columns)-1; i++ {
		m = m.cycleSort()
		seen[m.table.SortColumn()] = true
	}

	if len(seen) != len(m.table.Columns) {
		t.Fatalf("cycled through %d distinct columns, want %d: saw %v", len(seen), len(m.table.Columns), seen)
	}

	m = m.cycleSort() // one more press wraps back to the start
	if m.table.SortColumn() != start {
		t.Fatalf("SortColumn after a full cycle = %d, want wrap back to %d", m.table.SortColumn(), start)
	}
}

// TestReverseSortTogglesDirection confirms "S" flips the current column's
// direction and flips it back on a second press.
func TestReverseSortTogglesDirection(t *testing.T) {
	now := time.Now()
	m := newResultsModel().setResults(sampleResultsForSort(now), indexer.ModeSearch, now)

	initial := m.table.SortAscending()

	m = m.reverseSort()
	if m.table.SortAscending() == initial {
		t.Fatal("reverseSort did not toggle direction")
	}

	m = m.reverseSort()
	if m.table.SortAscending() != initial {
		t.Fatal("second reverseSort did not toggle back to the original direction")
	}
}

// TestReverseSortNoopWhenUnsorted confirms "S" before any results have ever
// been set (SortColumn is -1) does nothing rather than panicking.
func TestReverseSortNoopWhenUnsorted(t *testing.T) {
	m := newResultsModel()

	got := m.reverseSort()
	if got.table.SortColumn() != -1 {
		t.Fatalf("SortColumn = %d, want -1 (still unsorted)", got.table.SortColumn())
	}
}

// --- teatest integration ------------------------------------------------

// dispatchNonEmptySearch types text into the query field and submits it —
// the shared setup every teatest case below that needs ModeSearch's default
// sort (seeders descending) rather than the empty-query ModeLatest path
// T-060's own tests already cover.
func dispatchNonEmptySearch(tm *teatest.TestModel, text string) {
	tm.Send(keyRune("/"))
	for _, r := range text {
		tm.Send(keyRune(string(r)))
	}
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // commits the field
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // submits
}

// TestResultsScreenRendersColumnsAndHeader is T-061's "teatest covers
// render" acceptance: every column header, the mode/query header line, and
// the row content actually appear once a search completes.
func TestResultsScreenRendersColumnsAndHeader(t *testing.T) {
	searcher := newStubSearcher(indexerfake.New("alpha", "Alpha", testCaps(true, true), nil))
	searcher.outcome = stubOutcome{results: []indexer.Result{
		{
			IndexerID: "alpha", ID: "1", Title: "debian-13.0.0-amd64-netinst.iso",
			SizeBytes: 700 * 1024 * 1024, Seeders: 42, Leechers: 3,
			Trust: indexer.TrustVIP, Published: time.Now().Add(-3 * time.Hour),
		},
	}}

	tm, _ := newSearchTestModel(t, searcher, nil)
	waitForOutput(t, tm, "Query:")

	dispatchNonEmptySearch(tm, "debian")

	waitForAllOutput(t, tm,
		`Search: "debian"`,
		"Title", "Size", "S/L", "Trust", "Age", "Source",
		// The Title column is a fixed 30 columns wide at this terminal
		// width (80 - the five fixed columns - separators), so the full
		// 32-character filename is exactly what theme.Truncate cuts to —
		// checking the un-truncated prefix rather than the whole title.
		"debian-13.0.0-amd64-netinst", "700.0 MB", "42/3", "VIP", "3h", "alpha",
	)
}

// TestResultsScreenSortCyclingChangesSortIndicator is T-061's "teatest
// covers ... sort cycling" acceptance: "s" moves the sort indicator to the
// next column (starting it ascending) and "S" reverses the current one —
// driven through the real running program, not resultsModel directly.
func TestResultsScreenSortCyclingChangesSortIndicator(t *testing.T) {
	searcher := newStubSearcher(indexerfake.New("alpha", "Alpha", testCaps(true, true), nil))
	searcher.outcome = stubOutcome{results: []indexer.Result{
		{IndexerID: "alpha", ID: "1", Title: "b-title", Seeders: 5},
		{IndexerID: "alpha", ID: "2", Title: "a-title", Seeders: 50},
	}}

	tm, _ := newSearchTestModel(t, searcher, nil)
	waitForOutput(t, tm, "Query:")

	dispatchNonEmptySearch(tm, "x")

	// Default for a Search-mode result set: seeders (S/L) descending.
	waitForOutput(t, tm, "S/L v")

	// The Trust column is only 6 wide, too narrow to ever show "Trust ^"
	// (its own header text plus the indicator overflows and gets
	// ellipsised — a real, if incidental, consequence of AGENT.md §7's
	// column widths, not a defect this test needs to chase) — so the
	// second "s" is sent immediately rather than waiting on that
	// intermediate frame, landing on Age, which does fit its indicator.
	tm.Send(keyRune("s")) // -> Trust, ascending (not independently observable at this width)
	tm.Send(keyRune("s")) // -> Age, ascending
	waitForOutput(t, tm, "Age ^")

	tm.Send(keyRune("S")) // reverse the current column (Age)
	waitForOutput(t, tm, "Age v")
}

// TestResultsScreenPartialFailureShowsResultsAndStatusBar is T-061's
// "teatest covers ... the partial-failure case" acceptance: one source
// failing must not hide the results the other source returned, and the
// status bar reports the failure count (AGENT.md §6.3).
func TestResultsScreenPartialFailureShowsResultsAndStatusBar(t *testing.T) {
	searcher := newStubSearcher(
		indexerfake.New("alpha", "Alpha", testCaps(true, true), nil),
		indexerfake.New("bravo", "Bravo", testCaps(true, true), nil),
	)
	searcher.outcome = stubOutcome{
		results: []indexer.Result{{IndexerID: "alpha", ID: "1", Title: "still-here.iso", Seeders: 10}},
		errs:    []indexer.SourceError{{IndexerID: "bravo", Err: indexerfake.ErrDemoSourceDown}},
	}

	tm, _ := newSearchTestModel(t, searcher, nil)
	waitForOutput(t, tm, "Query:")

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // empty query -> Latest, both sources queried

	waitForAllOutput(t, tm, "still-here.iso", "1/2 sources failed")
}

// TestResultsScreenEmptyStateNamesSourcesAndOffersLatest pins T-061's
// "Zero results shows an explicit empty state naming which sources were
// queried, and in Search mode offers Latest as a next step".
func TestResultsScreenEmptyStateNamesSourcesAndOffersLatest(t *testing.T) {
	searcher := newStubSearcher(indexerfake.New("alpha", "Alpha", testCaps(true, true), nil))

	tm, _ := newSearchTestModel(t, searcher, nil)
	waitForOutput(t, tm, "Query:")

	dispatchNonEmptySearch(tm, "x")

	waitForAllOutput(t, tm, "No results. Sources queried: alpha", "Press L for Latest")
}

// TestResultsScreenRefreshShowsCacheHintOnSecondFetch drives R (ActionRefresh)
// against a real *indexer.Registry (not a stub) end to end: the first
// dispatch is a fresh fetch, and R re-running the identical query inside
// the cache TTL is served from the registry's own cache (AGENT.md's T-012
// cache, DefaultCacheTTL) — proving T-061's "R refreshes, honouring the
// T-012 cache ... the status bar shows when results came from cache rather
// than a fresh fetch" end to end, not just at the registry or the status
// bar in isolation.
func TestResultsScreenRefreshShowsCacheHintOnSecondFetch(t *testing.T) {
	reg := indexer.NewRegistry(indexer.Config{})
	ix := indexerfake.New("alpha", "Alpha", indexer.Caps{Search: true, Latest: true}, []indexer.Result{
		{IndexerID: "alpha", ID: "1", Title: "cache-me.iso", Seeders: 5},
	})

	if err := reg.Register(ix); err != nil {
		t.Fatalf("Register: %v", err)
	}

	m := New(fake.New(), testTheme(), WithSearcher(reg))
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	waitForOutput(t, tm, "Query:")

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // empty query -> Latest
	waitForAllOutput(t, tm, "cache-me.iso", "fresh")

	tm.Send(keyRune("R"))
	waitForOutput(t, tm, "cached")
}

// waitForCallCount blocks until searcher has recorded at least n calls, or
// fails the test after a generous deadline — SearchAll resolves inside a
// tea.Cmd on bubbletea's own goroutine, not synchronously with Send, so a
// bare callCount() check right after Send would race it.
func waitForCallCount(t *testing.T, searcher *stubSearcher, n int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for searcher.callCount() < n {
		if time.Now().After(deadline) {
			t.Fatalf("callCount() = %d after 2s, want at least %d", searcher.callCount(), n)
		}

		time.Sleep(5 * time.Millisecond)
	}
}

// TestRefreshReDispatchesLastQueryNotTheSearchForm is the regression test
// for the T-061 review finding: R must re-run the query that produced the
// results on screen (m.lastQuery/m.lastQueriedIDs), never the search
// form's own live contents, which are independent state a user can edit
// without submitting. Repro: commit "x" into the query field without
// submitting it, press L (dispatches and shows Latest), then press R — the
// pre-fix code called dispatchSearch(false), which reads m.search.mode
// (still ModeSearch, never touched by L) and m.search.query (still "x"),
// so it would have re-dispatched Mode=Search Text="x" instead of staying
// on Latest. Verified against the pre-fix code: reverting handleRefresh to
// `return m.dispatchSearch(false)` makes this test fail exactly that way.
func TestRefreshReDispatchesLastQueryNotTheSearchForm(t *testing.T) {
	searcher := newStubSearcher(indexerfake.New("alpha", "Alpha", testCaps(true, true), nil))
	tm, _ := newSearchTestModel(t, searcher, nil)

	waitForOutput(t, tm, "Query:")

	// Commit "x" into the query field without submitting it.
	tm.Send(keyRune("/"))
	tm.Send(keyRune("x"))
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // commits the field, does not submit
	waitForOutput(t, tm, "x")

	tm.Send(keyRune("L")) // dispatches Latest, independent of the form's own mode/text
	waitForOutput(t, tm, "Sources queried")
	waitForCallCount(t, searcher, 1)

	firstCall, ok := searcher.lastCall()
	if !ok || firstCall.q.Mode != indexer.ModeLatest {
		t.Fatalf("setup: L dispatched %+v, want Mode=Latest", firstCall.q)
	}

	tm.Send(keyRune("R"))
	waitForCallCount(t, searcher, 2)

	call, ok := searcher.lastCall()
	if !ok {
		t.Fatal("expected a second SearchAll call from R")
	}

	if call.q.Mode != indexer.ModeLatest {
		t.Fatalf("R dispatched Mode=%v, want ModeLatest — the query that produced the results on screen, not the search form's", call.q.Mode)
	}

	if call.q.Text != "" {
		t.Fatalf("R dispatched Text=%q, want empty — it must not pick up the uncommitted \"x\" left in the search form", call.q.Text)
	}

	if got := searcher.callCount(); got != 2 {
		t.Fatalf("SearchAll called %d times, want exactly 2 (L, then R)", got)
	}
}

// TestRefreshDoesNotRecordHistory confirms R does not call AddHistory a
// second time for the query it re-dispatches — only enter/L (a genuinely
// new, user-typed search) records one (T-061 review finding #4: fixing the
// refresh-target bug also removes the duplicate AddHistory call, since R
// now has its own dispatch path that never calls it).
func TestRefreshDoesNotRecordHistory(t *testing.T) {
	hist := &stubHistory{}
	searcher := newStubSearcher(indexerfake.New("alpha", "Alpha", testCaps(true, true), nil))
	tm, _ := newSearchTestModel(t, searcher, hist)

	waitForOutput(t, tm, "Query:")

	tm.Send(keyRune("/"))
	for _, r := range "brand-new" {
		tm.Send(keyRune(string(r)))
	}
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "Sources queried")
	waitForCallCount(t, searcher, 1)

	tm.Send(keyRune("R"))
	waitForCallCount(t, searcher, 2)

	hist.mu.Lock()
	added := append([]string(nil), hist.added...)
	hist.mu.Unlock()

	if len(added) != 1 || added[0] != "brand-new" {
		t.Fatalf("AddHistory calls = %v, want exactly [\"brand-new\"] — R must not record a second entry", added)
	}
}

// TestHandleRefreshWithNoPriorSearchPushesHint confirms R before any
// search has ever completed (m.lastQueriedIDs empty) does nothing but push
// a status-bar hint, rather than dispatching an empty/zero-value query.
func TestHandleRefreshWithNoPriorSearchPushesHint(t *testing.T) {
	searcher := newStubSearcher(indexerfake.New("alpha", "Alpha", testCaps(true, true), nil))
	m := New(fake.New(), testTheme(), WithSearcher(searcher))

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	updatedModel, cmd := m.handleRefresh()

	if updatedModel.search.inFlight {
		t.Fatal("handleRefresh with nothing searched yet should not dispatch anything")
	}

	if cmd == nil {
		t.Fatal("expected a tea.Cmd pushing a status-bar hint")
	}

	if searcher.callCount() != 0 {
		t.Fatalf("SearchAll called %d times, want 0", searcher.callCount())
	}
}
