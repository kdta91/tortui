// Search screen (T-060): query input, mode selector (Search / Latest),
// source multi-select, and optional category / min-seeders filters. This
// file owns everything ScreenSearch-specific — its own state (searchModel),
// its dispatch to the indexer fan-out, and its render — root.go only routes
// key presses and messages into it.
//
// internal/tui may import indexer's frozen domain types (Query, Result,
// Caps, ...) but never a concrete implementation (AGENT.md §4). Searcher
// below is the interface this screen actually needs — satisfied by
// *indexer.Registry in production and by a test double in search_test.go —
// so this package still never names a concrete indexer type. HistoryStore
// does the same thing for *store.Store.
package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/store"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// Searcher is the subset of *indexer.Registry the search screen needs:
// the enabled sources to offer in the multi-select, and the fan-out itself.
type Searcher interface {
	// Enabled returns the sources the multi-select should offer, in a
	// stable order. The search screen snapshots this once, at
	// construction (see newSearchModel) — rendering must stay pure
	// (AGENT.md §6.8), and every production caller already has a fully
	// registered registry by the time it builds the Model.
	Enabled() []indexer.Indexer

	// SearchAll runs q against ids (or every enabled source when ids is
	// empty) and reports the merged results plus one SourceError per
	// source that failed or was skipped, exactly as
	// (*indexer.Registry).SearchAll documents.
	SearchAll(ctx context.Context, q indexer.Query, ids ...string) ([]indexer.Result, []indexer.SourceError, error)
}

// HistoryStore is the subset of *store.Store the search screen reads recent
// queries from and records new ones to.
type HistoryStore interface {
	ListHistory() []store.HistoryEntry
	AddHistory(text string) error
}

// maxRecentSuggestions caps how many recent queries are offered as
// suggestions — the store already caps how many it keeps (T-041's
// maxHistoryEntries), this is a separate, much smaller cap on how many are
// worth showing on one screen.
const maxRecentSuggestions = 5

// searchSpinnerFrames is the in-flight indicator's animation. Plain ASCII
// on purpose: unlike the download progress bars (which go through
// theme.GlyphSet, AGENT.md §7), a spinner has no "block glyph" fallback
// question to answer, and these four characters render identically at
// every Capability this app supports (T-050/T-055), so there is nothing to
// degrade.
var searchSpinnerFrames = []string{"|", "/", "-", "\\"}

// searchSpinnerInterval is how often the spinner frame advances while a
// query is in flight.
const searchSpinnerInterval = 120 * time.Millisecond

// searchCategoryOptions is the category filter's cycle order. CategoryOther
// is deliberately not offered here: it is Category's zero value, so
// "no filter selected" and "filtering for Other specifically" would be
// indistinguishable in the UI if it were in the cycle. A result classified
// CategoryOther is still returned under "no filter" (the default) — this
// only affects what the explicit filter can be set to.
var searchCategoryOptions = []indexer.Category{
	indexer.CategoryAudio,
	indexer.CategoryVideo,
	indexer.CategoryImage,
	indexer.CategoryText,
	indexer.CategorySoftware,
	indexer.CategoryData,
}

// editTarget is which field, if any, is currently consuming raw key input
// instead of the global keymap (root.go's handleKey / handleSearchTyping).
type editTarget int

const (
	// editNone means no field is being typed into: j/k/space and the rest
	// of the keymap behave normally on the search screen.
	editNone editTarget = iota
	// editQuery means runes/backspace/space append to or trim query.
	editQuery
	// editMinSeeders means runes/backspace/space append to or trim
	// minSeedersText, filtered to digits only (see searchModel.appendText).
	editMinSeeders
)

// searchModel is ScreenSearch's own state: everything root.Model's generic
// screen-agnostic fields (selection, and so on) are not, per root.go's own
// note that a screen is "free to replace it with its own bounded,
// data-backed cursor." Every method has a value receiver and returns an
// updated copy, the same immutable-update style components.Dialog uses.
type searchModel struct {
	// sourceIDs is the fixed, ordered snapshot of Searcher.Enabled() taken
	// at construction (see Searcher.Enabled's doc comment for why this is
	// a snapshot, not a live read). sourceCaps and selected are keyed by
	// the same ids.
	sourceIDs  []string
	sourceCaps map[string]indexer.Caps
	// selected is which sources are included in the next dispatch. Every
	// snapshot source starts selected — "multi-select of enabled sources"
	// reads most naturally as "everything enabled, unless you opt out."
	selected map[string]bool

	// query is the keyword text, edited in place while editing == editQuery.
	query string
	// mode is the mode selector's current setting. dispatchSearch may
	// still send ModeLatest for a particular query without changing this
	// (an empty query, or the L hotkey) — see dispatchSearch.
	mode indexer.Mode
	// categoryIdx indexes searchCategoryOptions, or -1 for "no filter"
	// (the default).
	categoryIdx int
	// minSeedersText is the min-seeders filter's digit buffer, edited in
	// place while editing == editMinSeeders. See minSeedersValue for how
	// it is parsed.
	minSeedersText string

	// cursor is the flat, keyboard-navigable row index: 0 is the query
	// row, 1 is the mode row, 2..2+len(sourceIDs)-1 are the source rows,
	// then category, then min-seeders. See totalRows.
	cursor int
	// editing is which field (if any) is currently consuming raw key
	// input; see editTarget.
	editing editTarget

	// inFlight is true from dispatch until the matching searchResultMsg
	// (or a cancellation) resolves it.
	inFlight bool
	// generation increments on every dispatch and on every cancellation.
	// A searchResultMsg or searchTickMsg whose gen no longer matches is
	// stale — superseded by a newer dispatch, or explicitly cancelled —
	// and is silently dropped (handleSearchResult, handleSearchTick).
	generation int
	// cancel stops the in-flight SearchAll call (context.WithCancel's
	// CancelFunc from dispatchSearch). nil while nothing is in flight.
	cancel context.CancelFunc
	// spinnerFrame indexes searchSpinnerFrames.
	spinnerFrame int

	// recent is the recent-query suggestions loaded from HistoryStore at
	// construction and refreshed after every dispatch that records one.
	recent []string
}

// newSearchModel snapshots searcher's enabled sources (nil-safe: a nil
// Searcher yields no sources rather than panicking) and hist's recent
// queries (nil-safe likewise), with every source selected and no category
// filter — the screen's starting state (see New/Option in root.go).
func newSearchModel(searcher Searcher, hist HistoryStore) searchModel {
	s := searchModel{
		sourceCaps:  make(map[string]indexer.Caps),
		selected:    make(map[string]bool),
		categoryIdx: -1,
	}

	if searcher != nil {
		for _, ix := range searcher.Enabled() {
			id := ix.ID()
			s.sourceIDs = append(s.sourceIDs, id)
			s.sourceCaps[id] = ix.Caps()
			s.selected[id] = true
		}
	}

	if hist != nil {
		s.recent = loadRecentQueries(hist)
	}

	return s
}

// loadRecentQueries returns up to maxRecentSuggestions non-empty query
// texts from hist, most recent first. A Latest-mode entry (HistoryEntry.Text
// == "") is skipped — there is no keyword to suggest re-running.
func loadRecentQueries(hist HistoryStore) []string {
	entries := hist.ListHistory()

	out := make([]string, 0, maxRecentSuggestions)
	for _, e := range entries {
		if e.Text == "" {
			continue
		}

		out = append(out, e.Text)
		if len(out) == maxRecentSuggestions {
			break
		}
	}

	return out
}

// supportsSearchMode reports whether caps allows mode — the same check
// indexer.Registry.SearchAll makes internally, duplicated here (rather than
// exported from indexer for one boolean) so the multi-select can grey out
// an unsupported source and explain why before ever dispatching.
func supportsSearchMode(caps indexer.Caps, mode indexer.Mode) bool {
	if mode == indexer.ModeLatest {
		return caps.Latest
	}

	return caps.Search
}

// totalRows is the flat cursor's range: query, mode, one row per source,
// category, min-seeders.
func (s searchModel) totalRows() int {
	return 4 + len(s.sourceIDs)
}

// moveCursor moves the flat cursor by delta rows, wrapping at both ends.
func (s searchModel) moveCursor(delta int) searchModel {
	total := s.totalRows()
	if total <= 0 {
		return s
	}

	s.cursor = ((s.cursor+delta)%total + total) % total

	return s
}

// toggleAtCursor applies the space-bar action for whichever row the cursor
// is on: start editing (query, min-seeders), flip the mode, toggle a
// source's inclusion (only when it supports the current mode — toggling an
// unsupported, greyed-out source is a no-op, not a silent lie about what
// will be queried), or advance the category filter.
func (s searchModel) toggleAtCursor() searchModel {
	n := len(s.sourceIDs)

	switch {
	case s.cursor == 0:
		s.editing = editQuery
	case s.cursor == 1:
		if s.mode == indexer.ModeSearch {
			s.mode = indexer.ModeLatest
		} else {
			s.mode = indexer.ModeSearch
		}
	case s.cursor >= 2 && s.cursor < 2+n:
		id := s.sourceIDs[s.cursor-2]
		if supportsSearchMode(s.sourceCaps[id], s.mode) {
			s.selected[id] = !s.selected[id]
		}
	case s.cursor == 2+n:
		s.categoryIdx++
		if s.categoryIdx >= len(searchCategoryOptions) {
			s.categoryIdx = -1
		}
	default:
		// The min-seeders row — the only one left once query, mode, every
		// source, and category are accounted for.
		s.editing = editMinSeeders
	}

	return s
}

// appendText adds text to whichever field is being edited, digit-filtered
// for min-seeders (anything else typed there is silently dropped — there is
// no error state for "not a digit," it just doesn't appear) and unfiltered
// for the query. A no-op when editing == editNone.
func (s searchModel) appendText(text string) searchModel {
	switch s.editing {
	case editQuery:
		s.query += text
	case editMinSeeders:
		for _, r := range text {
			if r >= '0' && r <= '9' {
				s.minSeedersText += string(r)
			}
		}
	case editNone:
		// Nothing is focused; ignore.
	}

	return s
}

// backspace removes the last rune of whichever field is being edited. A
// no-op when editing == editNone or the field is already empty.
func (s searchModel) backspace() searchModel {
	switch s.editing {
	case editQuery:
		s.query = trimLastRune(s.query)
	case editMinSeeders:
		s.minSeedersText = trimLastRune(s.minSeedersText)
	case editNone:
		// Nothing is focused; ignore.
	}

	return s
}

// trimLastRune drops the last rune of s, leaving s unchanged if it is
// already empty.
func trimLastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}

	return string(r[:len(r)-1])
}

// selectedIDs returns the source ids to query for mode: selected, and
// capable of serving mode. Order follows sourceIDs, which is registration
// order, the same determinism SearchAll's own docs rely on.
func (s searchModel) selectedIDs(mode indexer.Mode) []string {
	var ids []string

	for _, id := range s.sourceIDs {
		if !s.selected[id] {
			continue
		}

		if !supportsSearchMode(s.sourceCaps[id], mode) {
			continue
		}

		ids = append(ids, id)
	}

	return ids
}

// categoryFilter returns the active category filter, or ok == false when
// none is set (categoryIdx == -1, the default).
func (s searchModel) categoryFilter() (cat indexer.Category, ok bool) {
	if s.categoryIdx < 0 || s.categoryIdx >= len(searchCategoryOptions) {
		return indexer.CategoryOther, false
	}

	return searchCategoryOptions[s.categoryIdx], true
}

// minSeedersValue parses minSeedersText, treating anything empty, invalid,
// or negative as "no minimum" (0) — the same meaning Query.MinSeeders == 0
// already carries, so there is no separate error state to render.
func (s searchModel) minSeedersValue() int {
	n, err := strconv.Atoi(strings.TrimSpace(s.minSeedersText))
	if err != nil || n < 0 {
		return 0
	}

	return n
}

// searchResultMsg carries one dispatch's outcome back into Update
// (root.go's handleSearchResult). gen ties it to the dispatch that produced
// it, so a result superseded by a newer dispatch or an explicit
// cancellation (searchModel.generation) is recognisably stale.
type searchResultMsg struct {
	gen        int
	query      indexer.Query
	results    []indexer.Result
	sourceErrs []indexer.SourceError
	// queried is how many sources dispatchSearch actually asked — the
	// status bar's "N/M sources failed" needs M even when every one of
	// them failed and so contributed nothing to results.
	queried int
	err     error
}

// searchTickMsg advances the in-flight spinner. Same gen-guard as
// searchResultMsg, and the same "reissue only while still relevant"
// pattern components.StatusBar's TickMsg already established.
type searchTickMsg struct{ gen int }

// searchTickCmd returns a tea.Cmd that fires one searchTickMsg for gen
// after searchSpinnerInterval.
func searchTickCmd(gen int) tea.Cmd {
	return tea.Tick(searchSpinnerInterval, func(time.Time) tea.Msg {
		return searchTickMsg{gen: gen}
	})
}

// dispatchSearchCmd returns the tea.Cmd that actually calls
// searcher.SearchAll — off Update's own goroutine, per AGENT.md §6.1 — and
// reports the outcome as a searchResultMsg. Cancelling ctx (handleSearchCancel)
// makes SearchAll return promptly: it plumbs ctx straight down to each
// source's per-request context, which is exactly what makes esc a real
// cancel and not just a UI state change that leaves the fan-out running
// unseen.
func dispatchSearchCmd(searcher Searcher, ctx context.Context, q indexer.Query, ids []string, gen int) tea.Cmd {
	return func() tea.Msg {
		results, sourceErrs, err := searcher.SearchAll(ctx, q, ids...)
		return searchResultMsg{gen: gen, query: q, results: results, sourceErrs: sourceErrs, queried: len(ids), err: err}
	}
}

// dispatchSearch is enter's (ActionSelect) and L's (ActionLatest) shared
// implementation. forceLatest is true for L — AGENT.md §7: "L | Latest —
// recent additions across sources, no keyword needed" — and runs Latest
// against the currently selected sources regardless of the mode selector's
// own setting or which screen is current. Plain enter instead honours the
// mode selector, except that an empty query always runs Latest too (T-060
// acceptance: "enter on an empty query runs Latest rather than doing
// nothing").
func (m Model) dispatchSearch(forceLatest bool) (Model, tea.Cmd) {
	if m.searcher == nil {
		var cmd tea.Cmd
		m.statusBar, cmd = m.statusBar.Push("no sources configured")

		return m, cmd
	}

	// A second dispatch (L while a plain search is already running, or
	// vice versa) supersedes rather than queues: cancel whatever is in
	// flight first, exactly like handleSearchCancel, so its eventual
	// result is recognised as stale by the generation bump below.
	if m.search.inFlight && m.search.cancel != nil {
		m.search.cancel()
	}

	mode := m.search.mode
	if forceLatest {
		mode = indexer.ModeLatest
	}

	text := strings.TrimSpace(m.search.query)
	if text == "" {
		mode = indexer.ModeLatest
	}

	if mode == indexer.ModeLatest {
		// Query.Text's own contract: "empty when Mode is ModeLatest."
		text = ""
	}

	ids := m.search.selectedIDs(mode)
	if len(ids) == 0 {
		var cmd tea.Cmd
		m.statusBar, cmd = m.statusBar.Push("select at least one source")

		return m, cmd
	}

	if text != "" && m.history != nil {
		// Best-effort: a full disk or a closed store must not block
		// dispatching the search itself (AGENT.md §6.1 — no blocking
		// I/O in Update, and this runs inside Update, not the Cmd, only
		// because store writes are already just an in-memory append
		// behind a mutex — see internal/store's own doc comment).
		_ = m.history.AddHistory(text)
		m.search.recent = loadRecentQueries(m.history)
	}

	m.search.generation++
	gen := m.search.generation
	m.search.inFlight = true
	m.search.spinnerFrame = 0

	ctx, cancel := context.WithCancel(context.Background())
	m.search.cancel = cancel

	q := indexer.Query{Mode: mode, Text: text, MinSeeders: m.search.minSeedersValue()}
	if cat, ok := m.search.categoryFilter(); ok {
		q.Categories = []indexer.Category{cat}
	}

	return m, tea.Batch(dispatchSearchCmd(m.searcher, ctx, q, ids, gen), searchTickCmd(gen))
}

// handleSearchResult applies one dispatch's outcome (root.go's Update,
// searchResultMsg case). A stale gen (superseded or cancelled) is dropped
// without touching any state — the dispatch that owns the current
// generation already left things exactly as it should.
func (m Model) handleSearchResult(msg searchResultMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.search.generation {
		return m, nil
	}

	m.search.inFlight = false
	m.search.cancel = nil

	m.lastResults = msg.results
	m.lastSourceErrs = msg.sourceErrs
	m.lastQuery = msg.query

	failed := make([]string, 0, len(msg.sourceErrs))
	for _, se := range msg.sourceErrs {
		failed = append(failed, se.IndexerID)
	}

	// Reuse sourceStatusMsg's own handling (root.go) rather than
	// duplicating its errorDetail-reset logic here — this is exactly the
	// hand-off T-052 built that message type for (see its doc comment).
	updated, _ := m.Update(sourceStatusMsg{total: msg.queried, failed: failed})
	m = updated.(Model)

	if msg.err != nil {
		var cmd tea.Cmd
		m.statusBar, cmd = m.statusBar.Push(searchDispatchErrorMessage(msg.err))

		return m, cmd
	}

	// T-060 acceptance ("L ... jumps to results") and the natural reading
	// of enter's own dispatch: once there is something to show, go show
	// it. T-061 (not yet built) is what actually renders m.lastResults;
	// until then this lands on Results' placeholder body.
	m.screen = ScreenResults

	return m, nil
}

// searchDispatchErrorMessage turns a fatal SearchAll error (ErrNoSources or
// ErrAllSourcesFailed — the only two SearchAll ever returns) into the
// status bar's transient text.
func searchDispatchErrorMessage(err error) string {
	return fmt.Sprintf("search failed: %v", err)
}

// handleSearchTick advances the spinner (root.go's Update, searchTickMsg
// case) and reissues the tick — unless gen is stale or the query already
// resolved, in which case the chain simply stops rather than reissuing
// forever.
func (m Model) handleSearchTick(msg searchTickMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.search.generation || !m.search.inFlight {
		return m, nil
	}

	m.search.spinnerFrame = (m.search.spinnerFrame + 1) % len(searchSpinnerFrames)

	return m, searchTickCmd(msg.gen)
}

// handleSearchCancel implements esc on the search screen (root.go's
// handleKey, screenContext(ScreenSearch)'s own ActionCancel binding):
// cancel the in-flight query, if any, and bump generation so its eventual
// (now-cancelled) result is recognised as stale and dropped rather than
// shown as an error. A no-op when nothing is in flight.
func (m Model) handleSearchCancel() (tea.Model, tea.Cmd) {
	if !m.search.inFlight {
		return m, nil
	}

	if m.search.cancel != nil {
		m.search.cancel()
	}

	m.search.inFlight = false
	m.search.cancel = nil
	m.search.generation++

	return m, nil
}

// handleSearchTyping is root.go's handleKey's escape hatch while a field is
// being edited: it claims exactly the key types text entry needs and
// reports handled == false for everything else (ctrl+c, tab, the screen
// jump keys, ...), which lets those fall through to the normal keymap
// lookup instead of being swallowed.
func (m Model) handleSearchTyping(msg tea.KeyMsg) (handled bool, out tea.Model, cmd tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		// Commits the field without submitting the whole search — enter
		// outside edit mode is what submits (ActionSelect in root.go's
		// handleKey); this just stops that same keypress from also being
		// typed as a character.
		m.search.editing = editNone
		return true, m, nil
	case tea.KeyEsc:
		// Blurs without discarding what was typed — "esc always cancels"
		// (AGENT.md §7) means closing the field, not losing its content.
		m.search.editing = editNone
		return true, m, nil
	case tea.KeyBackspace:
		m.search = m.search.backspace()
		return true, m, nil
	case tea.KeyRunes:
		m.search = m.search.appendText(string(msg.Runes))
		return true, m, nil
	case tea.KeySpace:
		m.search = m.search.appendText(" ")
		return true, m, nil
	default:
		return false, m, nil
	}
}

// renderSearchScreen draws ScreenSearch's real body (root.go's
// renderScreenBody): the query row, mode selector, source multi-select
// (greyed with a reason when a source can't serve the current mode),
// category and min-seeders filters, recent-query suggestions, and — while a
// query is in flight — the spinner and cancel hint in place of all of it.
// Pure: reads m and returns a string, no I/O, no mutation (AGENT.md §6.8).
func (m Model) renderSearchScreen() string {
	th := m.theme
	s := m.search

	var b strings.Builder

	if s.inFlight {
		frame := searchSpinnerFrames[s.spinnerFrame%len(searchSpinnerFrames)]
		b.WriteString(th.Accent.Render(frame + " searching... (esc to cancel)"))
		b.WriteString("\n\n")
	}

	b.WriteString(m.searchRow(0, "Query: "+s.queryDisplay()))
	b.WriteString("\n")
	b.WriteString(m.searchRow(1, "Mode:  "+s.modeDisplay(th)))
	b.WriteString("\n\n")
	b.WriteString(th.Muted.Render("Sources"))
	b.WriteString("\n")

	if len(s.sourceIDs) == 0 {
		b.WriteString(th.Muted.Render("  (none configured)"))
		b.WriteString("\n")
	}

	for i, id := range s.sourceIDs {
		b.WriteString(m.searchRow(2+i, s.sourceDisplay(id, th)))
		b.WriteString("\n")
	}

	catRow := 2 + len(s.sourceIDs)
	b.WriteString("\n")
	b.WriteString(m.searchRow(catRow, "Category: "+s.categoryDisplay()))
	b.WriteString("\n")
	b.WriteString(m.searchRow(catRow+1, "Min seeders: "+s.minSeedersDisplay()))

	if len(s.recent) > 0 {
		b.WriteString("\n\n")
		b.WriteString(th.Muted.Render("Recent: " + strings.Join(s.recent, ", ")))
	}

	return truncateLines(b.String(), m.width)
}

// searchRow prefixes content with the focus cursor ("> ", accented) when
// row is the currently focused row and nothing is being edited, or two
// spaces otherwise — editing hides the cursor since queryDisplay/
// minSeedersDisplay already draw their own caret on the field itself.
func (m Model) searchRow(row int, content string) string {
	if m.search.cursor == row && m.search.editing == editNone {
		return m.theme.Accent.Render("> ") + content
	}

	return "  " + content
}

// queryDisplay renders the query field: the typed text with a block cursor
// appended while editing, or — when empty and not being edited — a hint
// that doubles as T-060's "enter on an empty query runs Latest" acceptance
// text made visible.
func (s searchModel) queryDisplay() string {
	if s.editing == editQuery {
		return s.query + "█"
	}

	if s.query == "" {
		return "(empty - enter runs Latest)"
	}

	return s.query
}

// modeDisplay renders the two-way mode selector with the active choice
// accented and bracketed, the inactive one muted.
func (s searchModel) modeDisplay(th theme.Theme) string {
	search, latest := "Search", "Latest"

	if s.mode == indexer.ModeSearch {
		return th.Accent.Render("["+search+"]") + "  " + th.Muted.Render(latest)
	}

	return th.Muted.Render(search) + "  " + th.Accent.Render("["+latest+"]")
}

// sourceDisplay renders one multi-select row: a checkbox and the source id,
// or — when the source cannot serve the current mode — the same line muted
// with the reason, per T-060's "shown greyed ... with the reason" acceptance.
func (s searchModel) sourceDisplay(id string, th theme.Theme) string {
	caps := s.sourceCaps[id]

	if !supportsSearchMode(caps, s.mode) {
		reason := "search"
		if s.mode == indexer.ModeLatest {
			reason = "latest"
		}

		return th.Muted.Render(fmt.Sprintf("[ ] %s (unavailable: does not support %s)", id, reason))
	}

	box := "[ ]"
	if s.selected[id] {
		box = "[x]"
	}

	return box + " " + id
}

// categoryDisplay renders the category filter: its String() token, or
// "any" when none is set.
func (s searchModel) categoryDisplay() string {
	if cat, ok := s.categoryFilter(); ok {
		return cat.String()
	}

	return "any"
}

// minSeedersDisplay renders the min-seeders field: the digits typed so far
// with a block cursor while editing, "0" when empty and not being edited
// (minSeedersValue's own "no minimum" meaning made visible), or the typed
// digits otherwise.
func (s searchModel) minSeedersDisplay() string {
	if s.editing == editMinSeeders {
		return s.minSeedersText + "█"
	}

	if s.minSeedersText == "" {
		return "0"
	}

	return s.minSeedersText
}

// truncateLines applies theme.Truncate to every line of s independently, so
// a multi-line screen body degrades the same way every single-line render
// in this package already does (AGENT.md §14), rather than truncating the
// whole block as if it were one line. A non-positive width returns s
// unchanged — the same "no WindowSizeMsg yet" tolerance View() itself
// already has.
func truncateLines(s string, width int) string {
	if width <= 0 {
		return s
	}

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = theme.Truncate(line, width)
	}

	return strings.Join(lines, "\n")
}
