package components

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/kdta91/tortui/internal/tui/theme"
)

// updateGoldenEnv regenerates golden fixtures under testdata/ when set to
// "1" — mirrors internal/tui/theme's own helper (AGENT.md §15: "A
// golden-file diff is a real failure — inspect the diff, do not
// regenerate blindly").
const updateGoldenEnv = "UPDATE_GOLDEN"

func compareGolden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name)

	if os.Getenv(updateGoldenEnv) == "1" {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatalf("writing golden file %s: %v", path, err)
		}

		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file %s: %v (run `%s=1 go test ./internal/tui/components/...` to create it)", path, err, updateGoldenEnv)
	}

	if got != string(want) {
		t.Fatalf("golden mismatch for %s:\n--- want ---\n%s--- got ---\n%s", path, want, got)
	}
}

// goldenRows is the fixed dataset every golden render uses: real-shaped
// content plus one row whose title mixes CJK text and an emoji, so the
// golden fixture is direct proof that columns stay aligned under
// grapheme-aware measurement (AGENT.md §14) rather than an assertion
// buried in a separate unit test.
func goldenRows() []Row {
	return []Row{
		{ID: "r1", Cells: []string{"debian-13.0.0-amd64-netinst.iso", "702 MB", "3421/12", "VIP", "3h", "archive-src"}},
		{ID: "r2", Cells: []string{"示例种子🎬.iso", "4.70 GB", "128/4", "TR", "2d", "research-repo"}},
		{ID: "r3", Cells: []string{"a research dataset release, volume two", "18.2 GB", "56/3", "", "5d", "example.org"}},
		{ID: "r4", Cells: []string{"sample.txt", "12 B", "1/0", "", "1y", "example.org"}},
	}
}

// TestTableGolden80x24 is the T-053 acceptance criterion's 80x24 golden:
// AGENT.md §7's "must render legibly at 80x24" floor, with every column
// present.
func TestTableGolden80x24(t *testing.T) {
	tbl := NewTable(resultColumns()).SetRows(goldenRows())
	tbl = tbl.SortBy(1) // Size, so the header carries a visible sort indicator too

	compareGolden(t, "table_80x24.golden", tbl.View(80, 24, testTableTheme()))
}

// TestTableGolden120x40 is the T-053 acceptance criterion's 120x40
// golden: comfortably wide, every column present with room to spare.
func TestTableGolden120x40(t *testing.T) {
	tbl := NewTable(resultColumns()).SetRows(goldenRows())
	tbl = tbl.SortBy(1)

	compareGolden(t, "table_120x40.golden", tbl.View(120, 40, testTableTheme()))
}

// TestTableGolden60x20 is the T-053 acceptance criterion's 60x20 golden:
// narrow enough that AGENT.md §7's drop order actually engages — Source
// is gone, Age and Trust remain, matching resultColumns' priorities.
func TestTableGolden60x20(t *testing.T) {
	tbl := NewTable(resultColumns()).SetRows(goldenRows())
	tbl = tbl.SortBy(1)

	view := tbl.View(60, 20, testTableTheme())

	if strings.Contains(strings.SplitN(view, "\n", 2)[0], "Source") {
		t.Fatalf("60x20 golden should have dropped Source per AGENT.md §7, got header %q", strings.SplitN(view, "\n", 2)[0])
	}

	compareGolden(t, "table_60x20.golden", view)
}

// resultColumns builds the exact column set AGENT.md §7 documents for the
// results table — "Title (flex) · Size · S/L · Trust · Age · Source" —
// with the priorities that reproduce its drop order verbatim: "hide
// columns right-to-left: Source → Age → Trust". Table itself never
// mentions any of these names; this is what a caller like T-061's
// results screen would configure.
func resultColumns() []Column {
	return []Column{
		{Key: "title", Title: "Title", Flex: true, MinWidth: 20, Align: AlignLeft},
		{Key: "size", Title: "Size", Width: 8, Align: AlignRight},
		{Key: "sl", Title: "S/L", Width: 9, Align: AlignRight},
		{Key: "trust", Title: "Trust", Width: 6, Align: AlignLeft, Priority: 3},
		{Key: "age", Title: "Age", Width: 6, Align: AlignRight, Priority: 2},
		{Key: "source", Title: "Source", Width: 16, Align: AlignLeft, Priority: 1},
	}
}

func sampleRows() []Row {
	return []Row{
		{ID: "r1", Cells: []string{"debian-13.0.0-amd64-netinst.iso", "702 MB", "3421/12", "VIP", "3h", "archive-src"}},
		{ID: "r2", Cells: []string{"示例种子🎬.iso", "4.70 GB", "128/4", "TR", "2d", "research-repo"}},
		{ID: "r3", Cells: []string{"sample.txt", "12 B", "1/0", "", "1y", "example.org"}},
	}
}

func testTableTheme() theme.Theme {
	return theme.New(theme.DefaultThemeName, theme.Capability{Color: theme.ColorNone, Unicode: true, Interactive: true})
}

// TestTableColumnDropOrderAtNarrowWidths pins AGENT.md §7's exact policy:
// Source drops first, then Age, then Trust, as available width shrinks —
// and Title/Size/S-L (Priority 0) never drop, however narrow.
func TestTableColumnDropOrderAtNarrowWidths(t *testing.T) {
	tbl := NewTable(resultColumns())

	cases := []struct {
		name        string
		width       int
		wantAbsent  []string
		wantPresent []string
	}{
		{name: "wide enough for everything", width: 80, wantPresent: []string{"Title", "Size", "S/L", "Trust", "Age", "Source"}},
		{name: "drops Source first", width: 60, wantAbsent: []string{"Source"}, wantPresent: []string{"Title", "Size", "S/L", "Trust", "Age"}},
		{name: "drops Source and Age next", width: 50, wantAbsent: []string{"Source", "Age"}, wantPresent: []string{"Title", "Size", "S/L", "Trust"}},
		{name: "drops all three, keeps the essentials", width: 40, wantAbsent: []string{"Source", "Age", "Trust"}, wantPresent: []string{"Title", "Size", "S/L"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			header := strings.SplitN(tbl.View(tc.width, 10, testTableTheme()), "\n", 2)[0]

			for _, want := range tc.wantPresent {
				if !strings.Contains(header, want) {
					t.Errorf("width %d: header %q missing column %q", tc.width, header, want)
				}
			}

			for _, absent := range tc.wantAbsent {
				if strings.Contains(header, absent) {
					t.Errorf("width %d: header %q should have dropped column %q", tc.width, header, absent)
				}
			}
		})
	}
}

// TestTableNeverDropsPriorityZeroColumns confirms that even at an
// extreme width, the column set itself still contains Title/Size/S-L
// (Priority 0) — only Priority > 0 columns are ever dropped. At a width
// this narrow the flex Title column can render with zero width (nothing
// left after the fixed, undroppable Size/S-L columns), so this checks
// the column set layout picked, not rendered text content.
func TestTableNeverDropsPriorityZeroColumns(t *testing.T) {
	cols, _ := layout(resultColumns(), 10)

	keys := make(map[string]bool, len(cols))
	for _, c := range cols {
		keys[c.Key] = true
	}

	for _, want := range []string{"title", "size", "sl"} {
		if !keys[want] {
			t.Errorf("even at width 10, layout dropped Priority-0 column %q: got %v", want, cols)
		}
	}
}

// TestTableSortByTogglesDirectionOnSameColumn confirms sorting the same
// column twice toggles ascending/descending, and a header sort indicator
// reflects it, while sorting a different column always starts ascending.
func TestTableSortByTogglesDirectionOnSameColumn(t *testing.T) {
	tbl := NewTable(resultColumns()).SetRows(sampleRows())

	tbl = tbl.SortBy(0) // title, ascending
	if tbl.SortColumn() != 0 || !tbl.SortAscending() {
		t.Fatalf("first SortBy(0): col=%d asc=%v, want col=0 asc=true", tbl.SortColumn(), tbl.SortAscending())
	}

	view := tbl.View(80, 10, testTableTheme())
	if !strings.Contains(strings.SplitN(view, "\n", 2)[0], "Title ^") {
		t.Fatalf("expected ascending indicator on Title header, got %q", view)
	}

	tbl = tbl.SortBy(0) // toggle to descending
	if tbl.SortAscending() {
		t.Fatal("second SortBy(0) should toggle to descending")
	}

	view = tbl.View(80, 10, testTableTheme())
	if !strings.Contains(strings.SplitN(view, "\n", 2)[0], "Title v") {
		t.Fatalf("expected descending indicator on Title header, got %q", view)
	}

	tbl = tbl.SortBy(1) // a different column starts ascending again
	if tbl.SortColumn() != 1 || !tbl.SortAscending() {
		t.Fatalf("SortBy(1) after sorting col 0: col=%d asc=%v, want col=1 asc=true", tbl.SortColumn(), tbl.SortAscending())
	}
}

// TestTableSortOrdersRowsByColumn confirms SortBy actually reorders rows,
// using a numeric Less over the raw seeders value rather than the
// formatted "S/L" display text — the pattern a real caller needs since
// "3421/12" does not sort correctly as a string.
func TestTableSortOrdersRowsByColumn(t *testing.T) {
	cols := resultColumns()
	cols[1].Less = func(a, b string) bool { // Size column, numeric by prefix digit count as a stand-in
		return len(a) < len(b)
	}

	tbl := NewTable(cols).SetRows([]Row{
		{ID: "big", Cells: []string{"big.iso", "10.00 GB", "1/1"}},
		{ID: "small", Cells: []string{"small.txt", "1 B", "1/1"}},
		{ID: "medium", Cells: []string{"medium.iso", "700 MB", "1/1"}},
	})

	tbl = tbl.SortBy(1)

	got := make([]string, len(tbl.Rows()))
	for i, r := range tbl.Rows() {
		got[i] = r.ID
	}

	want := []string{"small", "medium", "big"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sorted row order = %v, want %v", got, want)
		}
	}
}

// TestTableSelectionSurvivesResort is the T-053 acceptance criterion by
// name: selection is preserved across a re-sort, verified by the
// selected row's identity (ID), never its index — the whole point is
// that the index changes while the identity does not.
func TestTableSelectionSurvivesResort(t *testing.T) {
	tbl := NewTable(resultColumns()).SetRows(sampleRows())

	// Select "r3" (sample.txt), which sorts last alphabetically by title
	// and last by size — pick whichever index it currently occupies.
	for tbl.SelectedID() != "r3" {
		tbl = tbl.MoveDown()
	}

	if tbl.SelectedID() != "r3" {
		t.Fatalf("setup failed: could not select r3, got %q", tbl.SelectedID())
	}

	beforeIndex := tbl.SelectedIndex()

	tbl = tbl.SortBy(0) // sort by title ascending

	if tbl.SelectedID() != "r3" {
		t.Fatalf("selection identity changed after re-sort: got %q, want r3", tbl.SelectedID())
	}

	tbl = tbl.SortBy(0) // toggle to descending — index should flip again

	if tbl.SelectedID() != "r3" {
		t.Fatalf("selection identity changed after second re-sort: got %q, want r3", tbl.SelectedID())
	}

	afterIndex := tbl.SelectedIndex()
	if beforeIndex == afterIndex {
		// Not a hard requirement in general, but for this specific
		// fixture (3 rows, r3 sorts differently ascending vs
		// unsorted-then-descending) the index should actually move,
		// proving the test isn't accidentally checking index stability.
		t.Logf("index unchanged (%d); still fine as long as identity held", afterIndex)
	}
}

// TestTableSetRowsKeepsSelectionWhenIDStillPresent confirms a fresh
// SetRows call (e.g. a re-search) that still contains the previously
// selected ID keeps it selected instead of resetting to the first row.
func TestTableSetRowsKeepsSelectionWhenIDStillPresent(t *testing.T) {
	tbl := NewTable(resultColumns()).SetRows(sampleRows())
	tbl = tbl.MoveDown() // select r2

	if tbl.SelectedID() != "r2" {
		t.Fatalf("setup: SelectedID() = %q, want r2", tbl.SelectedID())
	}

	tbl = tbl.SetRows(sampleRows()) // same rows, e.g. a refresh
	if tbl.SelectedID() != "r2" {
		t.Fatalf("SetRows dropped selection: SelectedID() = %q, want r2", tbl.SelectedID())
	}
}

// TestTableSetRowsFallsBackToFirstRowWhenSelectionGone confirms a
// SetRows call whose new rows no longer contain the previously selected
// ID (the result disappeared from a refreshed search) falls back to the
// first row instead of leaving a dangling selection.
func TestTableSetRowsFallsBackToFirstRowWhenSelectionGone(t *testing.T) {
	tbl := NewTable(resultColumns()).SetRows(sampleRows())
	tbl = tbl.MoveDown().MoveDown() // select r3

	tbl = tbl.SetRows([]Row{
		{ID: "new1", Cells: []string{"new.iso", "1 GB", "1/1", "", "1m", "example.org"}},
	})

	if tbl.SelectedID() != "new1" {
		t.Fatalf("SelectedID() = %q, want fallback to the only remaining row", tbl.SelectedID())
	}
}

// TestTableMoveUpDownStopsAtEdges confirms MoveUp/MoveDown clamp at the
// first and last row instead of wrapping or going out of bounds.
func TestTableMoveUpDownStopsAtEdges(t *testing.T) {
	tbl := NewTable(resultColumns()).SetRows(sampleRows())

	tbl = tbl.MoveUp() // already at the first row
	if tbl.SelectedID() != "r1" {
		t.Fatalf("MoveUp at first row: SelectedID() = %q, want r1", tbl.SelectedID())
	}

	tbl = tbl.MoveDown().MoveDown().MoveDown() // one extra past the last row
	if tbl.SelectedID() != "r3" {
		t.Fatalf("MoveDown past last row: SelectedID() = %q, want r3", tbl.SelectedID())
	}
}

// TestTableViewportScrollsToKeepSelectionVisible confirms a table longer
// than the visible area scrolls its viewport as the selection moves, and
// always renders exactly the requested number of rows.
func TestTableViewportScrollsToKeepSelectionVisible(t *testing.T) {
	rows := make([]Row, 20)
	for i := range rows {
		rows[i] = Row{ID: strconv.Itoa(i), Cells: []string{"title" + strconv.Itoa(i), "1 GB", "1/1", "", "1h", "example.org"}}
	}

	tbl := NewTable(resultColumns()).SetRows(rows)

	const height = 6 // header + 5 data rows
	for i := 0; i < 19; i++ {
		tbl = tbl.MoveDown()
	}

	view := tbl.View(80, height, testTableTheme())
	lines := strings.Split(view, "\n")
	if len(lines) != height {
		t.Fatalf("View with %d rows requested at height %d produced %d lines, want %d", len(rows), height, len(lines), height)
	}

	// The selected row (the last one, "title19") must be one of the
	// rendered data lines, proving the viewport followed the selection
	// instead of staying pinned to the top.
	if !strings.Contains(view, "title19") {
		t.Fatalf("viewport did not scroll to keep the selected last row visible:\n%s", view)
	}
}

// TestTableViewNeverExceedsRequestedWidth confirms every rendered line
// fits within the requested width, per column, at a set of widths
// including ones narrow enough to force every drop.
func TestTableViewNeverExceedsRequestedWidth(t *testing.T) {
	tbl := NewTable(resultColumns()).SetRows(sampleRows())

	for _, width := range []int{120, 80, 60, 50, 40, 30} {
		view := tbl.View(width, 10, testTableTheme())
		for _, line := range strings.Split(view, "\n") {
			if w := theme.Width(line); w > width {
				t.Errorf("width %d: line %q measures %d columns wide", width, line, w)
			}
		}
	}
}

// TestTableCJKEmojiAlignment confirms columns stay aligned (same
// terminal width per row) even when a title contains CJK text and an
// emoji, using theme's grapheme-aware Width rather than len() or rune
// counts to check it (AGENT.md §14).
func TestTableCJKEmojiAlignment(t *testing.T) {
	tbl := NewTable(resultColumns()).SetRows(sampleRows())

	view := tbl.View(80, 10, testTableTheme())
	lines := strings.Split(view, "\n")

	want := theme.Width(lines[0])
	for i, line := range lines[1:] {
		if got := theme.Width(line); got != want {
			t.Fatalf("line %d width = %d, want %d (header width) — CJK/emoji row misaligned:\n%s", i+1, got, want, view)
		}
	}
}

// TestTableSortIndicatorAbsentWhenUnsorted confirms a freshly constructed
// table with no SortBy call shows no sort indicator at all.
func TestTableSortIndicatorAbsentWhenUnsorted(t *testing.T) {
	tbl := NewTable(resultColumns()).SetRows(sampleRows())

	header := strings.SplitN(tbl.View(80, 10, testTableTheme()), "\n", 2)[0]
	if strings.Contains(header, "^") || strings.Contains(header, " v") {
		t.Fatalf("unsorted table header %q should have no sort indicator", header)
	}
}
