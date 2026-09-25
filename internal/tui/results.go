// Results screen (T-061): the sortable table of merged search results —
// AGENT.md §7's "Title (flex) · Size · S/L · Trust · Age · Source" — plus
// the header line naming the mode/query that produced them and the empty
// state shown when a completed search returned nothing. search.go's
// handleSearchResult is what feeds this screen: every completed dispatch
// (enter, L, or R via ActionRefresh in root.go) calls resultsModel.setResults
// with the same []indexer.Result root.go already captures in m.lastResults,
// so this file owns none of the fan-out itself — only turning its outcome
// into rows, a default sort, and a render.
package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/tui/components"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// Column indices into resultsColumns(), used by cycleSort/reverseSort and
// by applyModeDefault to name a column without restating its position.
const (
	colTitle = iota
	colSize
	colSL
	colTrust
	colAge
	colSource
)

// resultsColumns is exactly the column set AGENT.md §7 documents for the
// results table, with the same drop-order priorities T-053's own
// resultColumns test helper established ("hide columns right-to-left:
// Source → Age → Trust") — this package builds its own copy rather than
// importing components' test-only helper, since a caller configuring real
// columns for real data is exactly what T-053 left for this task to do.
func resultsColumns() []components.Column {
	return []components.Column{
		{Key: "title", Title: "Title", Flex: true, MinWidth: 20, Align: components.AlignLeft},
		{Key: "size", Title: "Size", Width: 8, Align: components.AlignRight, Less: sizeLess},
		{Key: "sl", Title: "S/L", Width: 9, Align: components.AlignRight, Less: seedersLess},
		{
			Key: "trust", Title: "Trust", Width: 6, Align: components.AlignLeft, Priority: 3,
			Less: trustLess, SortMissingLast: trustSortMissing, Accent: true,
		},
		{Key: "age", Title: "Age", Width: 6, Align: components.AlignRight, Priority: 2, Less: ageLess},
		{Key: "source", Title: "Source", Width: 16, Align: components.AlignLeft, Priority: 1},
	}
}

// resultsModel is ScreenResults' own state: the responsive table (T-053)
// holding the most recently completed search's rows. Like searchModel, every
// method has a value receiver and returns an updated copy.
type resultsModel struct {
	table components.Table

	// allRows is every row the most recent setResults produced, before
	// trustFilter is applied — the table itself only ever holds the
	// (possibly filtered) subset, so toggling the filter off needs
	// somewhere to recover the rows it hid rather than re-running the
	// search.
	allRows []components.Row
	// trustFilter is T-062's "filter toggle restricts to TrustTrusted and
	// above" (the 't' key, ActionToggleTrustFilter): true restricts the
	// table to rows whose Trust is TrustTrusted or TrustVIP.
	trustFilter bool
}

// newResultsModel returns a resultsModel with resultsColumns() and no rows.
func newResultsModel() resultsModel {
	return resultsModel{table: components.NewTable(resultsColumns())}
}

// setResults replaces the table's rows from results and applies mode's
// default sort (T-061 acceptance: "Default sort follows the mode: seeders
// desc for Search, age ascending ... for Latest") — every completed search
// re-establishes the default, superseding any sort a previous set of
// results left in place via cycleSort/reverseSort, since a brand new result
// set is exactly the "never mistaken for a stale search result" moment the
// header text next to it also exists for. The trust filter (T-062), if on,
// carries over to the new result set exactly like the sort column does.
func (m resultsModel) setResults(results []indexer.Result, mode indexer.Mode, now time.Time) resultsModel {
	rows := make([]components.Row, 0, len(results))
	for _, r := range results {
		rows = append(rows, resultRow(r, now))
	}

	m.allRows = rows
	m = m.applyTrustFilter()

	return m.applyModeDefault(mode)
}

// toggleTrustFilter implements the "t" key (ActionToggleTrustFilter): T-062
// acceptance "a filter toggle restricts to TrustTrusted and above".
func (m resultsModel) toggleTrustFilter() resultsModel {
	m.trustFilter = !m.trustFilter
	return m.applyTrustFilter()
}

// applyTrustFilter re-derives the table's row set from allRows: every row
// when trustFilter is off, or only TrustTrusted-and-above rows when it's
// on. Table.SetRows re-applies whatever sort is already active, so this
// never disturbs the current sort column/direction — only which rows are
// visible under it.
func (m resultsModel) applyTrustFilter() resultsModel {
	if !m.trustFilter {
		m.table = m.table.SetRows(m.allRows)
		return m
	}

	filtered := make([]components.Row, 0, len(m.allRows))

	for _, r := range m.allRows {
		if trustOrderOf(r) >= int(indexer.TrustTrusted) {
			filtered = append(filtered, r)
		}
	}

	m.table = m.table.SetRows(filtered)

	return m
}

// applyModeDefault sorts by seeders descending for ModeSearch, or age
// ascending (newest first) for ModeLatest, regardless of the table's prior
// sort state.
func (m resultsModel) applyModeDefault(mode indexer.Mode) resultsModel {
	if mode == indexer.ModeLatest {
		return m.sortAscBy(colAge)
	}

	return m.sortDescBy(colSL)
}

// sortAscBy sorts by col and guarantees ascending order regardless of the
// table's previous sort column/direction — components.Table.SortBy only
// promises ascending for a *different* column, and toggles when col is
// already the active one, so this checks the actual outcome and corrects it
// with one more call rather than assuming.
func (m resultsModel) sortAscBy(col int) resultsModel {
	m.table = m.table.SortBy(col)
	if !m.table.SortAscending() {
		m.table = m.table.SortBy(col)
	}

	return m
}

// sortDescBy is sortAscBy's descending counterpart.
func (m resultsModel) sortDescBy(col int) resultsModel {
	m.table = m.table.SortBy(col)
	if m.table.SortAscending() {
		m.table = m.table.SortBy(col)
	}

	return m
}

// cycleSort implements the "s" key (ActionSortCycle): move to the next
// column in display order, wrapping, always starting that column ascending
// (components.Table.SortBy's own rule for "a different column").
func (m resultsModel) cycleSort() resultsModel {
	n := len(m.table.Columns)
	if n == 0 {
		return m
	}

	next := (m.table.SortColumn() + 1) % n
	if next < 0 {
		next = 0
	}

	m.table = m.table.SortBy(next)

	return m
}

// reverseSort implements the "S" key (ActionSortReverse): toggle the
// currently sorted column's direction. A no-op when nothing is sorted yet.
func (m resultsModel) reverseSort() resultsModel {
	col := m.table.SortColumn()
	if col < 0 {
		return m
	}

	m.table = m.table.SortBy(col)

	return m
}

// resultRowID identifies one row by the indexer/result id pair that
// survived merging (indexer.Registry.mergeResults picks the highest-seeder
// copy), so selection tracks the same result across a re-sort exactly the
// way components.Table already guarantees by ID.
func resultRowID(r indexer.Result) string {
	return r.IndexerID + "|" + r.ID
}

// resultRow builds one table row from a merged search result. now is the
// reference point formatAge measures Published against — passed in rather
// than read from time.Now() here, so a caller (a test, or setResults with a
// fixed instant) controls it directly instead of racing a real clock.
func resultRow(r indexer.Result, now time.Time) components.Row {
	return components.Row{
		ID: resultRowID(r),
		Cells: []string{
			r.Title,
			formatSize(r.SizeBytes),
			fmt.Sprintf("%d/%d", r.Seeders, r.Leechers),
			r.Trust.Badge(),
			formatAge(now, r.Published),
			resultSource(r),
		},
		// SortKey[colTrust] carries the real indexer.Trust order behind
		// the badge (T-062 acceptance: sortable by trust, TrustUnknown
		// sorts last) — Badge() renders TrustUnknown and TrustNone as the
		// identical blank text, so trustLess/trustSortMissing could never
		// tell them apart from Cells alone. Every other index is left
		// empty, falling back to that column's own Cells text.
		SortKey: []string{"", "", "", strconv.Itoa(int(r.Trust)), "", ""},
	}
}

// parseTrustOrder reads the indexer.Trust order back off a trust column
// sort value — always Row.SortKey's entry (resultRow always sets one), so
// this is the plain reverse of strconv.Itoa(int(r.Trust)), not a Badge()
// parser. Unparseable input reads as TrustUnknown (0) rather than
// panicking.
func parseTrustOrder(cell string) int {
	n, _ := strconv.Atoi(cell)
	return n
}

// trustLess orders the trust column by the real indexer.Trust value (least
// to most trusted — AGENT.md §5's documented enum ordering), not the
// three-way-ambiguous badge text.
func trustLess(a, b string) bool { return parseTrustOrder(a) < parseTrustOrder(b) }

// trustSortMissing reports whether cell represents TrustUnknown — "the
// source reports no trust information at all" — which components.Table's
// SortMissingLast pins to the end of the trust column regardless of sort
// direction (T-062 acceptance: "TrustUnknown sorts last"). TrustNone (the
// source tracks trust and this upload has none) is a real, known value and
// sorts normally alongside Verified/Trusted/VIP, even though it renders
// the same blank badge as Unknown.
func trustSortMissing(cell string) bool { return parseTrustOrder(cell) == int(indexer.TrustUnknown) }

// trustOrderOf reads a table row's underlying trust order back out of its
// SortKey, for applyTrustFilter — the same value trustLess/trustSortMissing
// compare, so filtering and sorting always agree on what a row's trust
// actually is.
func trustOrderOf(r components.Row) int {
	if colTrust >= len(r.SortKey) {
		return int(indexer.TrustUnknown)
	}

	return parseTrustOrder(r.SortKey[colTrust])
}

// resultSource renders the Source column: every contributing indexer id
// (indexer.ExtraKeySources, set by the registry's fan-out merge) when
// present, or the winning copy's own IndexerID otherwise — a result that
// never went through SearchAll's merge (not expected in production, but
// cheap to handle) still renders something.
func resultSource(r indexer.Result) string {
	if s := r.Extra[indexer.ExtraKeySources]; s != "" {
		return s
	}

	return r.IndexerID
}

// cacheSummary aggregates indexer.ExtraKeyCacheHit across results into the
// status bar's cache indicator (T-061 acceptance: "the status bar shows
// when results came from cache rather than a fresh fetch"): "cached" when
// every result's surviving copy came from the registry's cache, "fresh"
// when none did, "partial cache" when it's a mix, and "" when there is
// nothing to characterise (no results — the empty state already explains
// why, without a cache claim attached to zero rows).
func cacheSummary(results []indexer.Result) string {
	if len(results) == 0 {
		return ""
	}

	cached, fresh := 0, 0

	for _, r := range results {
		if r.Extra[indexer.ExtraKeyCacheHit] == "true" {
			cached++
		} else {
			fresh++
		}
	}

	switch {
	case cached == 0:
		return "fresh"
	case fresh == 0:
		return "cached"
	default:
		return "partial cache"
	}
}

// --- formatting and their matching Less parsers -----------------------
//
// components.Column.Less compares two rows' already-*rendered* cell text
// (components.Table has no notion of a separate sort key), so every
// formatter below has a matching parser that reconstructs the numeric value
// well enough to order correctly — symmetric by construction, since both
// halves live in this file and agree on the one format in between.

// sizeUnits are formatSize's units, smallest first — index order doubles as
// parseSize's exponent.
var sizeUnits = []string{"B", "KB", "MB", "GB", "TB", "PB"}

// formatSize renders n bytes as a human-readable size (T-061 acceptance:
// "Sizes human-readable"), matching components.StatusBar's formatRate style
// for the same reason: one significant decimal above the byte scale, none
// below it, and never a value long enough to threaten the Size column's
// fixed 8-column width.
func formatSize(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}

	f := float64(n)
	unit := 0

	for f >= 1024 && unit < len(sizeUnits)-1 {
		f /= 1024
		unit++
	}

	return fmt.Sprintf("%.1f %s", f, sizeUnits[unit])
}

// parseSize reverses formatSize well enough for sizeLess to compare two
// cells numerically; a cell it cannot parse (never produced by formatSize
// itself) sorts as 0 rather than panicking.
func parseSize(cell string) float64 {
	fields := strings.Fields(cell)
	if len(fields) != 2 {
		return 0
	}

	val, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}

	for i, u := range sizeUnits {
		if u == fields[1] {
			for range i {
				val *= 1024
			}

			return val
		}
	}

	return 0
}

func sizeLess(a, b string) bool { return parseSize(a) < parseSize(b) }

// parseSeeders reads the seeder count back off an "S/L" cell (e.g.
// "3421/12"), for seedersLess — the S/L column's sort key is seeders, the
// same field AGENT.md §7 makes the results table's own default sort.
func parseSeeders(cell string) int {
	n, _ := strconv.Atoi(strings.SplitN(cell, "/", 2)[0])
	return n
}

func seedersLess(a, b string) bool { return parseSeeders(a) < parseSeeders(b) }

// formatAge renders how long ago at was, relative to now, as a compact
// token (T-061 acceptance: "ages relative (3h, 2d, 1y)"): seconds, minutes,
// hours, days, or years, whichever is the largest unit that produces a
// nonzero count. The zero time (a result whose source published no date)
// renders as "-" rather than a nonsensical multi-decade age.
func formatAge(now, at time.Time) string {
	if at.IsZero() {
		return "-"
	}

	d := now.Sub(at)
	if d < 0 {
		d = 0
	}

	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	}
}

// parseAgeSeconds reverses formatAge's unit suffix into seconds, for
// ageLess. "-" (formatAge's zero-time case) and anything else unparseable
// sorts as 0 — indistinguishable from "just now" — rather than panicking;
// AGENT.md §7 does not ask the Age column to distinguish "unknown" from
// "brand new" the way T-062 must for the Trust column.
func parseAgeSeconds(cell string) int64 {
	if cell == "" || cell == "-" {
		return 0
	}

	unit := cell[len(cell)-1]

	n, err := strconv.ParseInt(cell[:len(cell)-1], 10, 64)
	if err != nil {
		return 0
	}

	switch unit {
	case 's':
		return n
	case 'm':
		return n * 60
	case 'h':
		return n * 3600
	case 'd':
		return n * 86400
	case 'y':
		return n * 365 * 86400
	default:
		return 0
	}
}

func ageLess(a, b string) bool { return parseAgeSeconds(a) < parseAgeSeconds(b) }

// --- rendering -----------------------------------------------------------

// resultsTableChromeLines is how many lines root.go's View wraps around
// this screen's own body before the table's rows: the tab bar, the blank
// line under it, this screen's own header line, the blank line under that,
// the blank line above the status bar, and the status bar itself.
const resultsTableChromeLines = 6

// resultsTableMinHeight is the floor resultsTableHeight clamps to: the
// header row plus at least two data rows, so a very short terminal still
// shows something rather than an empty table.
const resultsTableMinHeight = 3

// resultsTableHeight picks components.Table.View's height argument from the
// whole-screen height root.go last saw, reserving resultsTableChromeLines
// for everything else the screen renders around the table.
func resultsTableHeight(height int) int {
	h := height - resultsTableChromeLines
	if h < resultsTableMinHeight {
		h = resultsTableMinHeight
	}

	return h
}

// resultsHeaderText names the current mode and query (T-061 acceptance:
// "Header states the current mode and query, so a Latest view is never
// mistaken for a stale search result"), plus, when trustFilter is on
// (T-062), a suffix naming it — so a filtered-down table is never mistaken
// for "these are all the results that came back".
func resultsHeaderText(q indexer.Query, trustFilter bool) string {
	text := fmt.Sprintf("Search: %q", q.Text)
	if q.Mode == indexer.ModeLatest {
		text = "Latest — recent additions"
	}

	if trustFilter {
		text += " · trust filter: Trusted and above"
	}

	return text
}

// renderResultsScreen draws ScreenResults' real body: the mode/query
// header, then either the responsive table or an empty state. Pure: reads
// m and returns a string, no I/O, no mutation (AGENT.md §6.8).
func (m Model) renderResultsScreen() string {
	th := m.theme
	header := th.Accent.Render(theme.Truncate(resultsHeaderText(m.lastQuery, m.results.trustFilter), m.width))

	if len(m.lastResults) == 0 {
		return truncateLines(header+"\n\n"+m.renderEmptyResults(), m.width)
	}

	if len(m.results.table.Rows()) == 0 {
		// The search returned rows, but the trust filter (T-062) excluded
		// every one of them — a distinct message from "no results" so
		// pressing 't' is the obvious next step rather than a dead end.
		msg := th.Muted.Render("No results at Trusted trust or above. Press t to clear the filter.")
		return truncateLines(header+"\n\n"+msg, m.width)
	}

	var b strings.Builder

	b.WriteString(header)
	b.WriteString("\n\n")
	b.WriteString(m.results.table.View(m.width, resultsTableHeight(m.height), th))

	return b.String()
}

// renderEmptyResults draws T-061's empty state: before any search has ever
// completed it just points at how to start one; after a search that
// returned nothing it names every source that was actually queried (never
// just a count — the acceptance text is explicit: "naming which sources
// were queried") and, in Search mode, offers Latest as the next step, since
// an empty keyword search is exactly the moment "see what's actually out
// there" is the most useful next action.
func (m Model) renderEmptyResults() string {
	th := m.theme

	if len(m.lastQueriedIDs) == 0 {
		return th.Muted.Render("No search yet — press / to search or L for latest.")
	}

	var b strings.Builder

	b.WriteString(th.Muted.Render("No results. Sources queried: " + strings.Join(m.lastQueriedIDs, ", ")))

	if m.lastQuery.Mode == indexer.ModeSearch {
		b.WriteString("\n")
		b.WriteString(th.Muted.Render("Press L for Latest — recent additions across the same sources."))
	}

	return b.String()
}
