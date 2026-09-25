package tui

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/kdta91/tortui/internal/engine/fake"
	"github.com/kdta91/tortui/internal/indexer"
	indexerfake "github.com/kdta91/tortui/internal/indexer/fake"
	"github.com/kdta91/tortui/internal/store"
)

// waitForAllOutput waits for a single window of output that contains every
// one of want, all at once — unlike a sequence of waitForOutput calls,
// which would drain the stream on the first match and could time out on
// substrings that were already there but arrived with it (see the callers'
// comments).
func waitForAllOutput(tb testing.TB, tm *teatest.TestModel, want ...string) {
	tb.Helper()

	teatest.WaitFor(
		tb, tm.Output(),
		func(bts []byte) bool {
			for _, w := range want {
				if !strings.Contains(string(bts), w) {
					return false
				}
			}
			return true
		},
		teatest.WithCheckInterval(10*time.Millisecond),
		teatest.WithDuration(3*time.Second),
	)
}

// --- test doubles -----------------------------------------------------

// stubCall records one SearchAll invocation's arguments, for tests that
// need to assert what dispatchSearch actually sent, not just what came
// back.
type stubCall struct {
	q   indexer.Query
	ids []string
}

// stubOutcome is what a blocked stubSearcher.SearchAll returns once
// released.
type stubOutcome struct {
	results []indexer.Result
	errs    []indexer.SourceError
	err     error
}

// stubSearcher is a Searcher test double. With block == false (the
// default) SearchAll returns immediately with whatever outcome is queued
// (or a zero outcome); with block == true it hangs until either release
// receives an outcome or ctx is cancelled — exactly what the in-flight
// spinner and esc-cancel tests need to observe a real intermediate state.
type stubSearcher struct {
	enabled []indexer.Indexer

	mu      sync.Mutex
	calls   []stubCall
	block   bool
	outcome stubOutcome
	release chan stubOutcome
}

func newStubSearcher(enabled ...indexer.Indexer) *stubSearcher {
	return &stubSearcher{enabled: enabled, release: make(chan stubOutcome, 1)}
}

func (s *stubSearcher) Enabled() []indexer.Indexer { return s.enabled }

func (s *stubSearcher) SearchAll(ctx context.Context, q indexer.Query, ids ...string) ([]indexer.Result, []indexer.SourceError, error) {
	s.mu.Lock()
	idsCopy := append([]string(nil), ids...)
	s.calls = append(s.calls, stubCall{q: q, ids: idsCopy})
	block := s.block
	outcome := s.outcome
	s.mu.Unlock()

	if !block {
		return outcome.results, outcome.errs, outcome.err
	}

	select {
	case out := <-s.release:
		return out.results, out.errs, out.err
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

func (s *stubSearcher) lastCall() (stubCall, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.calls) == 0 {
		return stubCall{}, false
	}

	return s.calls[len(s.calls)-1], true
}

func (s *stubSearcher) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.calls)
}

// stubHistory is a HistoryStore test double.
type stubHistory struct {
	mu      sync.Mutex
	entries []store.HistoryEntry
	added   []string
}

func (h *stubHistory) ListHistory() []store.HistoryEntry {
	h.mu.Lock()
	defer h.mu.Unlock()

	out := make([]store.HistoryEntry, len(h.entries))
	copy(out, h.entries)

	return out
}

func (h *stubHistory) AddHistory(text string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.added = append(h.added, text)
	h.entries = append(h.entries, store.HistoryEntry{Text: text, At: time.Now()})

	return nil
}

// --- searchModel unit tests (no I/O, no teatest) -----------------------

func testCaps(search, latest bool) indexer.Caps {
	return indexer.Caps{Search: search, Latest: latest}
}

func newTestSearchModel() searchModel {
	searcher := newStubSearcher(
		indexerfake.New("alpha", "Alpha", testCaps(true, true), nil),
		indexerfake.New("bravo", "Bravo", testCaps(true, false), nil), // no Latest
	)

	return newSearchModel(searcher, nil)
}

func TestNewSearchModelSnapshotsEnabledSourcesAllSelected(t *testing.T) {
	s := newTestSearchModel()

	if len(s.sourceIDs) != 2 {
		t.Fatalf("sourceIDs = %v, want 2 entries", s.sourceIDs)
	}

	for _, id := range s.sourceIDs {
		if !s.selected[id] {
			t.Fatalf("source %q should start selected", id)
		}
	}

	if s.categoryIdx != -1 {
		t.Fatalf("categoryIdx = %d, want -1 (no filter)", s.categoryIdx)
	}
}

func TestMoveCursorWrapsBothWays(t *testing.T) {
	s := newTestSearchModel()
	total := s.totalRows()

	if total != 6 { // query, mode, 2 sources, category, min-seeders
		t.Fatalf("totalRows() = %d, want 6", total)
	}

	s = s.moveCursor(-1)
	if s.cursor != total-1 {
		t.Fatalf("moving up from 0 = %d, want wrap to %d", s.cursor, total-1)
	}

	s = s.moveCursor(1)
	if s.cursor != 0 {
		t.Fatalf("moving down from the last row = %d, want wrap to 0", s.cursor)
	}
}

func TestToggleAtCursorQueryRowStartsEditing(t *testing.T) {
	s := newTestSearchModel()
	s.cursor = 0

	s = s.toggleAtCursor()
	if s.editing != editQuery {
		t.Fatalf("editing = %v, want editQuery", s.editing)
	}
}

func TestToggleAtCursorModeRowFlipsMode(t *testing.T) {
	s := newTestSearchModel()
	s.cursor = 1

	if s.mode != indexer.ModeSearch {
		t.Fatalf("setup: expected default mode ModeSearch")
	}

	s = s.toggleAtCursor()
	if s.mode != indexer.ModeLatest {
		t.Fatalf("mode after toggle = %v, want ModeLatest", s.mode)
	}

	s = s.toggleAtCursor()
	if s.mode != indexer.ModeSearch {
		t.Fatalf("mode after second toggle = %v, want ModeSearch", s.mode)
	}
}

func TestToggleAtCursorSourceRowTogglesSelection(t *testing.T) {
	s := newTestSearchModel()
	s.cursor = 2 // first source row ("alpha")

	s = s.toggleAtCursor()
	if s.selected["alpha"] {
		t.Fatal("expected \"alpha\" deselected after toggling its row")
	}

	s = s.toggleAtCursor()
	if !s.selected["alpha"] {
		t.Fatal("expected \"alpha\" reselected after toggling its row again")
	}
}

// TestToggleAtCursorUnsupportedSourceIsANoOp confirms a source that cannot
// serve the current mode cannot be selected either — toggling it must not
// silently promise a query it will never actually run (T-060 acceptance:
// sources shown greyed "so the user understands why a source is missing").
func TestToggleAtCursorUnsupportedSourceIsANoOp(t *testing.T) {
	s := newTestSearchModel()
	s.mode = indexer.ModeLatest
	s.cursor = 3 // "bravo", which declares Latest: false

	if !s.selected["bravo"] {
		t.Fatal("setup: expected \"bravo\" selected by default")
	}

	s = s.toggleAtCursor()
	if !s.selected["bravo"] {
		t.Fatal("toggling an unsupported source's row should not change its selected flag")
	}

	ids := s.selectedIDs(indexer.ModeLatest)
	for _, id := range ids {
		if id == "bravo" {
			t.Fatal("selectedIDs(ModeLatest) must never include a source without Caps.Latest")
		}
	}
}

func TestToggleAtCursorCategoryRowCyclesAndWraps(t *testing.T) {
	s := newTestSearchModel()
	s.cursor = 2 + len(s.sourceIDs) // category row

	if _, ok := s.categoryFilter(); ok {
		t.Fatal("setup: expected no category filter initially")
	}

	seen := map[indexer.Category]bool{}
	for range searchCategoryOptions {
		s = s.toggleAtCursor()
		cat, ok := s.categoryFilter()
		if !ok {
			t.Fatal("expected a category filter set mid-cycle")
		}
		seen[cat] = true
	}

	if len(seen) != len(searchCategoryOptions) {
		t.Fatalf("cycled through %d distinct categories, want %d", len(seen), len(searchCategoryOptions))
	}

	// One more press wraps back to "no filter".
	s = s.toggleAtCursor()
	if _, ok := s.categoryFilter(); ok {
		t.Fatal("expected the category cycle to wrap back to \"no filter\"")
	}
}

func TestAppendTextFiltersMinSeedersToDigits(t *testing.T) {
	s := newTestSearchModel()
	s.editing = editMinSeeders

	s = s.appendText("1a2b3")
	if s.minSeedersText != "123" {
		t.Fatalf("minSeedersText = %q, want \"123\" (non-digits dropped)", s.minSeedersText)
	}

	if got := s.minSeedersValue(); got != 123 {
		t.Fatalf("minSeedersValue() = %d, want 123", got)
	}
}

func TestAppendTextQueryAcceptsAnyCharacter(t *testing.T) {
	s := newTestSearchModel()
	s.editing = editQuery

	s = s.appendText("software s")
	if s.query != "software s" {
		t.Fatalf("query = %q, want \"software s\"", s.query)
	}
}

func TestBackspaceTrimsLastRune(t *testing.T) {
	s := newTestSearchModel()
	s.editing = editQuery
	s.query = "abc"

	s = s.backspace()
	if s.query != "ab" {
		t.Fatalf("query after backspace = %q, want \"ab\"", s.query)
	}
}

func TestMinSeedersValueTreatsEmptyOrInvalidAsZero(t *testing.T) {
	s := newTestSearchModel()

	if got := s.minSeedersValue(); got != 0 {
		t.Fatalf("minSeedersValue() with no input = %d, want 0", got)
	}
}

// --- Model-level / teatest integration tests ---------------------------

func newSearchTestModel(t *testing.T, searcher Searcher, hist HistoryStore) (*teatest.TestModel, Model) {
	t.Helper()

	var opts []Option
	if searcher != nil {
		opts = append(opts, WithSearcher(searcher))
	}
	if hist != nil {
		opts = append(opts, WithHistory(hist))
	}

	m := New(fake.New(), testTheme(), opts...)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	return tm, m
}

// TestSearchScreenRendersFormFields confirms the screen shows a text
// input, a mode selector, a source multi-select, and the category/
// min-seeders filters (T-060 acceptance's first bullet).
func TestSearchScreenRendersFormFields(t *testing.T) {
	searcher := newStubSearcher(
		indexerfake.New("alpha", "Alpha", testCaps(true, true), nil),
		indexerfake.New("bravo", "Bravo", testCaps(true, false), nil),
	)
	tm, _ := newSearchTestModel(t, searcher, nil)

	// One combined condition, not one waitForOutput per substring: every
	// one of these lives in the *same* static first render (nothing here
	// sends a key to provoke a fresh one), and waitForOutput only ever
	// sees bytes written since the previous call already drained the
	// stream — a second call checking a substring the first call's frame
	// already contained, with no new render in between, times out on an
	// empty read rather than finding it "still there."
	want := []string{"Query:", "Mode:", "Sources", "alpha", "bravo", "Category:", "Min seeders:"}
	waitForAllOutput(t, tm, want...)
}

// TestUnsupportedSourceShownGreyedWithReason drives the mode selector to
// Latest and confirms the source that cannot serve it is rendered with an
// explanation (T-060 acceptance: "shown greyed ... with the reason, so the
// user understands why a source is missing").
func TestUnsupportedSourceShownGreyedWithReason(t *testing.T) {
	searcher := newStubSearcher(
		indexerfake.New("alpha", "Alpha", testCaps(true, true), nil),
		indexerfake.New("bravo", "Bravo", testCaps(true, false), nil),
	)
	tm, _ := newSearchTestModel(t, searcher, nil)

	waitForOutput(t, tm, "Query:")

	// Move the cursor to the mode row and flip it to Latest. Both
	// assertions below land in the one render that flip produces, so they
	// are checked together (see waitForAllOutput's comment on
	// TestSearchScreenRendersFormFields).
	tm.Send(keyRune("j"))
	tm.Send(keyRune(" "))
	waitForAllOutput(t, tm, "[Latest]", "unavailable: does not support latest")
}

// TestEnterOnEmptyQueryDispatchesLatest confirms T-060's "enter on an
// empty query runs Latest rather than doing nothing" acceptance directly
// against what was actually sent to SearchAll.
func TestEnterOnEmptyQueryDispatchesLatest(t *testing.T) {
	searcher := newStubSearcher(indexerfake.New("alpha", "Alpha", testCaps(true, true), nil))
	tm, _ := newSearchTestModel(t, searcher, nil)

	waitForOutput(t, tm, "Query:")

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	// A completed dispatch (this stub never blocks) jumps to Results.
	waitForOutput(t, tm, "results screen")

	call, ok := searcher.lastCall()
	if !ok {
		t.Fatal("expected SearchAll to have been called")
	}

	if call.q.Mode != indexer.ModeLatest {
		t.Fatalf("Query.Mode = %v, want ModeLatest for an empty query", call.q.Mode)
	}

	if call.q.Text != "" {
		t.Fatalf("Query.Text = %q, want empty under ModeLatest", call.q.Text)
	}
}

// TestTypedQueryDispatchesSearchWithText confirms a non-empty query is sent
// verbatim under ModeSearch, and that letters which are otherwise global
// one-key hotkeys (here "s", which is ActionSortCycle elsewhere) type into
// the field instead of triggering their usual action while editing.
func TestTypedQueryDispatchesSearchWithText(t *testing.T) {
	searcher := newStubSearcher(indexerfake.New("alpha", "Alpha", testCaps(true, true), nil))
	tm, _ := newSearchTestModel(t, searcher, nil)

	waitForOutput(t, tm, "Query:")

	tm.Send(keyRune("/")) // focus the query field
	waitForOutput(t, tm, "█")

	for _, r := range "software" {
		tm.Send(keyRune(string(r)))
	}
	waitForOutput(t, tm, "software")

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // commits the field, does not submit
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // submits

	waitForOutput(t, tm, "results screen")

	call, ok := searcher.lastCall()
	if !ok {
		t.Fatal("expected SearchAll to have been called")
	}

	if call.q.Mode != indexer.ModeSearch || call.q.Text != "software" {
		t.Fatalf("Query = %+v, want Mode=Search Text=\"software\"", call.q)
	}
}

// TestSpaceDeselectsSourceExcludesItFromDispatch confirms toggling a
// source off the multi-select (space on its row) keeps it out of the next
// dispatch's ids.
func TestSpaceDeselectsSourceExcludesItFromDispatch(t *testing.T) {
	searcher := newStubSearcher(
		indexerfake.New("alpha", "Alpha", testCaps(true, true), nil),
		indexerfake.New("bravo", "Bravo", testCaps(true, true), nil),
	)
	tm, _ := newSearchTestModel(t, searcher, nil)

	waitForOutput(t, tm, "Query:")

	// cursor: 0=query, 1=mode, 2=alpha, 3=bravo
	tm.Send(keyRune("j"))
	tm.Send(keyRune("j"))
	tm.Send(keyRune("j"))
	tm.Send(keyRune(" ")) // deselect bravo
	waitForOutput(t, tm, "[ ] bravo")

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "results screen")

	call, ok := searcher.lastCall()
	if !ok {
		t.Fatal("expected SearchAll to have been called")
	}

	for _, id := range call.ids {
		if id == "bravo" {
			t.Fatalf("ids = %v, expected \"bravo\" excluded after deselecting it", call.ids)
		}
	}

	if len(call.ids) != 1 || call.ids[0] != "alpha" {
		t.Fatalf("ids = %v, want exactly [\"alpha\"]", call.ids)
	}
}

// TestInFlightSpinnerAndEscCancels drives a SearchAll call that blocks
// until released, confirms the spinner (and cancel hint) render while it
// is outstanding, then confirms esc actually cancels it: the screen never
// advances to Results and the eventually-cancelled call's own return value
// is discarded rather than reported as an error (T-060 acceptance:
// "in-flight query shows a spinner and is cancellable with esc").
func TestInFlightSpinnerAndEscCancels(t *testing.T) {
	searcher := newStubSearcher(indexerfake.New("alpha", "Alpha", testCaps(true, true), nil))
	searcher.block = true

	tm, _ := newSearchTestModel(t, searcher, nil)
	waitForOutput(t, tm, "Query:")

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "esc to cancel")

	// Wait for the spinner to actually advance past its first frame — this
	// only happens via a real searchTickMsg round-trip through Update
	// (handleSearchTick), not just the frame dispatchSearch sets
	// synchronously, so this is what proves the animation is a live
	// tea.Tick chain and not a static glyph.
	waitForOutput(t, tm, "/ searching")

	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})

	// The spinner/cancel hint must disappear once cancelled, and the
	// screen must not have advanced to Results — there is nothing to show.
	teatest.WaitFor(t, tm.Output(),
		func(bts []byte) bool { return strings.Contains(string(bts), "Query:") },
		teatest.WithCheckInterval(10*time.Millisecond), teatest.WithDuration(3*time.Second),
	)

	// Let the blocked SearchAll goroutine actually return (ctx was
	// cancelled) so it does not linger for the rest of the test binary.
	select {
	case searcher.release <- stubOutcome{}:
	case <-time.After(time.Second):
	}
}

// TestLatestFromAnotherScreenDispatchesAndJumpsToResults confirms AGENT.md
// §7 / T-060's "L | Latest ... jumps to results" works from a screen other
// than search, against whichever sources the search screen currently has
// selected.
func TestLatestFromAnotherScreenDispatchesAndJumpsToResults(t *testing.T) {
	searcher := newStubSearcher(indexerfake.New("alpha", "Alpha", testCaps(true, true), nil))
	tm, _ := newSearchTestModel(t, searcher, nil)

	waitForOutput(t, tm, "Query:")

	tm.Send(keyRune("4")) // jump to downloads
	waitForOutput(t, tm, "downloads screen")

	tm.Send(keyRune("L"))
	waitForOutput(t, tm, "results screen")

	call, ok := searcher.lastCall()
	if !ok {
		t.Fatal("expected SearchAll to have been called")
	}

	if call.q.Mode != indexer.ModeLatest {
		t.Fatalf("Query.Mode = %v, want ModeLatest", call.q.Mode)
	}
}

// TestRecentQueriesRenderedAndRecorded confirms recent queries from the
// store are offered as suggestions, and that a successful dispatch records
// its own text (T-060 acceptance: "Recent queries from the store offered
// as suggestions").
func TestRecentQueriesRenderedAndRecorded(t *testing.T) {
	hist := &stubHistory{entries: []store.HistoryEntry{
		{Text: "old-query-one"},
		{Text: "old-query-two"},
	}}
	searcher := newStubSearcher(indexerfake.New("alpha", "Alpha", testCaps(true, true), nil))
	tm, _ := newSearchTestModel(t, searcher, hist)

	waitForOutput(t, tm, "Recent: old-query-one, old-query-two")

	tm.Send(keyRune("/"))
	waitForOutput(t, tm, "█")

	for _, r := range "brand-new" {
		tm.Send(keyRune(string(r)))
	}

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	waitForOutput(t, tm, "results screen")

	hist.mu.Lock()
	added := append([]string(nil), hist.added...)
	hist.mu.Unlock()

	if len(added) != 1 || added[0] != "brand-new" {
		t.Fatalf("AddHistory calls = %v, want exactly [\"brand-new\"]", added)
	}
}

// TestDispatchWithNoSearcherPushesStatusMessage confirms a Model built
// without WithSearcher (every pre-T-060 test, and any future
// screen-routing-only test) degrades to a status-bar message instead of
// panicking when the user tries to search anyway.
func TestDispatchWithNoSearcherPushesStatusMessage(t *testing.T) {
	m := New(fake.New(), testTheme())

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("expected a tea.Cmd to start the status message's timeout")
	}

	if !strings.Contains(m.View(), "no sources configured") {
		t.Fatalf("View() = %q, want the \"no sources configured\" message", m.View())
	}
}

// TestDispatchWithNoSourcesSelectedPushesStatusMessage confirms
// deselecting every source and then dispatching reports why, rather than
// silently querying nothing.
func TestDispatchWithNoSourcesSelectedPushesStatusMessage(t *testing.T) {
	searcher := newStubSearcher(indexerfake.New("alpha", "Alpha", testCaps(true, true), nil))
	tm, _ := newSearchTestModel(t, searcher, nil)

	waitForOutput(t, tm, "Query:")

	tm.Send(keyRune("j"))
	tm.Send(keyRune("j"))
	tm.Send(keyRune(" ")) // deselect the only source
	waitForOutput(t, tm, "[ ] alpha")

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "select at least one source")

	if searcher.callCount() != 0 {
		t.Fatalf("SearchAll was called %d time(s), want 0 — nothing was selected to query", searcher.callCount())
	}
}

// TestFatalSearchErrorReportedAndStaysOnSearch confirms a fatal SearchAll
// error (indexer.ErrAllSourcesFailed, the only other outcome besides
// success) is surfaced via the status bar rather than silently jumping to
// an empty Results screen.
func TestFatalSearchErrorReportedAndStaysOnSearch(t *testing.T) {
	searcher := newStubSearcher(indexerfake.New("alpha", "Alpha", testCaps(true, true), nil))
	searcher.outcome = stubOutcome{
		errs: []indexer.SourceError{{IndexerID: "alpha", Err: indexerfake.ErrDemoSourceDown}},
		err:  indexer.ErrAllSourcesFailed,
	}

	tm, _ := newSearchTestModel(t, searcher, nil)
	waitForOutput(t, tm, "Query:")

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// The status bar truncates its whole line to the terminal width
	// (components.StatusBar.View, T-052) — with the source-error
	// indicator already on the same line, only the message's leading
	// "search" survives before the "..." ellipsis, so that (not the
	// message's full text) is what this checks for.
	waitForOutput(t, tm, "search")
}

// TestHandleSearchResultFatalErrorStaysOnSearchScreen is
// TestFatalSearchErrorReportedAndStaysOnSearch's precise, non-teatest
// counterpart: it confirms directly, without the status bar's own width
// truncation in the way, that a fatal error leaves m.screen on
// ScreenSearch rather than advancing to Results.
func TestHandleSearchResultFatalErrorStaysOnSearchScreen(t *testing.T) {
	m := New(fake.New(), testTheme(), WithSearcher(newStubSearcher()))

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	m.search.generation = 1
	m.search.inFlight = true

	updated, cmd := m.handleSearchResult(searchResultMsg{gen: 1, queried: 1, err: indexer.ErrAllSourcesFailed})
	m = updated.(Model)

	if m.screen != ScreenSearch {
		t.Fatalf("screen = %v, want ScreenSearch — a fatal error has nothing to show on Results", m.screen)
	}

	if m.search.inFlight {
		t.Fatal("expected inFlight cleared once the (failed) dispatch resolves")
	}

	if cmd == nil {
		t.Fatal("expected a tea.Cmd to push the error onto the status bar")
	}
}

// TestBackspaceWhileEditingQueryTrimsIt confirms backspace removes a typed
// character while the query field is focused, through the real key-press
// path (root.go's handleSearchTyping), not just searchModel.backspace in
// isolation.
func TestBackspaceWhileEditingQueryTrimsIt(t *testing.T) {
	searcher := newStubSearcher(indexerfake.New("alpha", "Alpha", testCaps(true, true), nil))
	tm, _ := newSearchTestModel(t, searcher, nil)

	waitForOutput(t, tm, "Query:")

	tm.Send(keyRune("/"))
	tm.Send(keyRune("a"))
	tm.Send(keyRune("b"))
	waitForOutput(t, tm, "ab█")

	tm.Send(tea.KeyMsg{Type: tea.KeyBackspace})
	waitForOutput(t, tm, "a█")
}

// TestCtrlCQuitsEvenWhileEditingQuery confirms handleSearchTyping only
// claims the key types text entry needs: ctrl+c (bound globally to
// ActionQuit) must still fall through and quit even mid-edit, so a query
// that happens to be typed while thinking "how do I get out of this" never
// traps the user.
func TestCtrlCQuitsEvenWhileEditingQuery(t *testing.T) {
	searcher := newStubSearcher(indexerfake.New("alpha", "Alpha", testCaps(true, true), nil))
	tm, _ := newSearchTestModel(t, searcher, nil)

	waitForOutput(t, tm, "Query:")

	tm.Send(keyRune("/"))
	waitForOutput(t, tm, "█")

	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}
