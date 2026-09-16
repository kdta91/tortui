package components

import (
	"sort"
	"strings"

	"github.com/kdta91/tortui/internal/tui/theme"
)

// Align is the horizontal alignment of a column's cell text within its
// rendered width.
type Align int

const (
	// AlignLeft left-aligns cell text (the default for text columns).
	AlignLeft Align = iota
	// AlignRight right-aligns cell text (typical for numeric columns
	// such as size or seeder/leecher counts).
	AlignRight
)

// Column describes one column of a Table. Table itself knows nothing
// about torrents, indexers, or the engine — AGENT.md §4 restricts
// internal/tui to importing indexer/engine interfaces only, and this
// package imports neither — so a screen wires up whatever columns and
// cell text it needs. T-061's results screen is the first caller,
// configuring the exact "Title · Size · S/L · Trust · Age · Source" set
// AGENT.md §7 specifies; this package proves the mechanism generically
// instead of hardcoding that particular set.
type Column struct {
	// Key uniquely identifies the column, independent of Title, so a
	// caller can change header text without disturbing a stored sort
	// preference.
	Key string
	// Title is the header text.
	Title string
	// Width is the fixed rendered width in terminal columns
	// (theme.Width), used when Flex is false.
	Width int
	// Flex marks the column that absorbs whatever width is left over
	// after every fixed-width and dropped column is accounted for. At
	// most one column should set Flex; if several do, only the first
	// (left to right, among the columns still visible) actually flexes.
	Flex bool
	// Priority controls drop order as the table narrows. Zero means the
	// column is never dropped — AGENT.md §7 requires Title, Size, and
	// S/L to remain visible at 80 columns. Positive values are dropped
	// lowest-first: a caller reproducing AGENT.md §7's documented policy
	// ("hide columns right-to-left: Source → Age → Trust") gives Source
	// the lowest positive priority and Trust the highest, so Source
	// disappears first and Trust last.
	Priority int
	// Align controls horizontal alignment of this column's cell text.
	Align Align
	// MinWidth is the floor a Flex column will not shrink below while
	// computing whether the column set fits (see layout); it is ignored
	// for non-Flex columns. Zero falls back to minFlexWidth, which is
	// usually too narrow to be legible — a caller should set a real
	// value (e.g. a torrent title needs more than 3 columns to mean
	// anything).
	MinWidth int
	// Less compares two cells of this column for sorting. A nil Less
	// falls back to a plain string comparison of the displayed cell
	// text, which is wrong for anything meant to sort numerically (size,
	// seeders, age) — a caller with that need supplies its own Less over
	// whatever the cell text encodes.
	Less func(a, b string) bool
}

// Row is one line of a Table. ID is a stable identity independent of
// position: Table tracks the selected row by ID, never by slice index,
// so selection survives a re-sort even though the row's index changes
// (T-053 acceptance: selection is preserved across a re-sort).
type Row struct {
	ID    string
	Cells []string
}

// minFlexWidth is the floor a Flex column is allowed to shrink to before
// the table simply renders narrower than requested — it never goes
// negative, and Truncate/Pad still produce well-formed output at this
// width.
const minFlexWidth = 3

// Table is a domain-agnostic, responsive table: a header row, sortable
// columns that drop by priority as available width shrinks, keyboard
// scrolling over a viewport, and a selection tracked by row identity
// rather than position. It is plain state plus a pure View — no I/O, no
// bubbletea import, no engine or indexer import (AGENT.md §4, §6.8) — so
// it is testable with no running program at all.
//
// The zero value is not ready to use; construct with NewTable.
type Table struct {
	// Columns is the column set, in display order. Table never mutates
	// it.
	Columns []Column

	rows       []Row
	sortCol    int // index into Columns; -1 means unsorted (insertion order)
	sortAsc    bool
	selectedID string
}

// NewTable returns a Table with the given columns, no rows, and no sort
// applied.
func NewTable(columns []Column) Table {
	return Table{Columns: columns, sortCol: -1}
}

// SetRows replaces the table's rows. If a row with the previously
// selected ID is still present after the current sort is re-applied, it
// stays selected (by identity, not position); otherwise the first row
// (post-sort) is selected, or nothing is selected when rows is empty.
func (t Table) SetRows(rows []Row) Table {
	t.rows = append([]Row(nil), rows...)
	t.applySort()
	t.ensureSelection()

	return t
}

// Rows returns the table's current rows in their current (sorted) order.
func (t Table) Rows() []Row {
	return t.rows
}

// applySort reorders t.rows in place per the current sortCol/sortAsc,
// using a stable sort so rows that compare equal keep their relative
// order (important once sort criteria repeat, e.g. re-sorting by the
// same column twice).
func (t *Table) applySort() {
	if t.sortCol < 0 || t.sortCol >= len(t.Columns) {
		return
	}

	col := t.Columns[t.sortCol]
	less := col.Less
	if less == nil {
		less = func(a, b string) bool { return a < b }
	}

	idx := t.sortCol

	sort.SliceStable(t.rows, func(i, j int) bool {
		a, b := cellAt(t.rows[i], idx), cellAt(t.rows[j], idx)
		if t.sortAsc {
			return less(a, b)
		}

		return less(b, a)
	})
}

func cellAt(r Row, idx int) string {
	if idx < 0 || idx >= len(r.Cells) {
		return ""
	}

	return r.Cells[idx]
}

// ensureSelection keeps the current selection if its row still exists
// and picks the first row otherwise (including when nothing was ever
// selected).
func (t *Table) ensureSelection() {
	if len(t.rows) == 0 {
		t.selectedID = ""
		return
	}

	for _, r := range t.rows {
		if r.ID == t.selectedID {
			return
		}
	}

	t.selectedID = t.rows[0].ID
}

// SortBy sorts the table by the column at colIndex: sorting by a new
// column starts ascending, and sorting by the already-active column
// toggles direction. Selection is preserved by identity — the row that
// was selected before the sort is still selected after it, even though
// its index generally changes (T-053 acceptance).
func (t Table) SortBy(colIndex int) Table {
	if colIndex < 0 || colIndex >= len(t.Columns) {
		return t
	}

	if t.sortCol == colIndex {
		t.sortAsc = !t.sortAsc
	} else {
		t.sortCol = colIndex
		t.sortAsc = true
	}

	t.applySort()
	t.ensureSelection()

	return t
}

// SortColumn returns the index of the column currently sorted on, or -1
// if no sort has been applied.
func (t Table) SortColumn() int { return t.sortCol }

// SortAscending reports whether the current sort is ascending. Its value
// is meaningless when SortColumn is -1.
func (t Table) SortAscending() bool { return t.sortAsc }

// SelectedID returns the identity of the currently selected row, or ""
// when the table has no rows.
func (t Table) SelectedID() string { return t.selectedID }

// SelectedIndex returns the position of the selected row in the table's
// current order, or -1 when nothing is selected.
func (t Table) SelectedIndex() int {
	for i, r := range t.rows {
		if r.ID == t.selectedID {
			return i
		}
	}

	return -1
}

// MoveDown moves the selection one row down, stopping at the last row.
func (t Table) MoveDown() Table {
	idx := t.SelectedIndex()
	if idx < 0 || idx >= len(t.rows)-1 {
		return t
	}

	t.selectedID = t.rows[idx+1].ID

	return t
}

// MoveUp moves the selection one row up, stopping at the first row.
func (t Table) MoveUp() Table {
	idx := t.SelectedIndex()
	if idx <= 0 {
		return t
	}

	t.selectedID = t.rows[idx-1].ID

	return t
}

// layout picks which columns are visible at width and each visible
// column's rendered width. It is called fresh from every View — nothing
// about layout is cached — because a table component caching widths
// across a resize is exactly the garbled-output hazard AGENT.md §13
// warns about.
//
// Columns with Priority 0 are never dropped. Droppable columns (Priority
// > 0) are dropped lowest-priority-first until the remaining columns'
// minimum widths (plus one separator space between each) fit within
// width, or no droppable column remains.
func layout(columns []Column, width int) ([]Column, []int) {
	visible := append([]Column(nil), columns...)

	for !fits(visible, width) {
		if !dropOne(&visible) {
			break
		}
	}

	return visible, widthsFor(visible, width)
}

// fits reports whether cols' minimum total width (every fixed column at
// its declared width, any flex column at its floor) plus separators fits
// within width.
func fits(cols []Column, width int) bool {
	if len(cols) == 0 {
		return true
	}

	total := len(cols) - 1 // one separator space between each column

	for _, c := range cols {
		if c.Flex {
			total += flexFloor(c)
		} else {
			total += c.Width
		}
	}

	return total <= width
}

// flexFloor is the minimum width a Flex column is allowed to shrink to:
// its own MinWidth when set, otherwise the package-wide minFlexWidth.
func flexFloor(c Column) int {
	if c.MinWidth > 0 {
		return c.MinWidth
	}

	return minFlexWidth
}

// dropOne removes the lowest-priority droppable (Priority > 0) column
// from cols, in place, and reports whether it found one to drop.
func dropOne(cols *[]Column) bool {
	dropAt := -1

	for i, c := range *cols {
		if c.Priority <= 0 {
			continue
		}

		if dropAt == -1 || (*cols)[dropAt].Priority > c.Priority {
			dropAt = i
		}
	}

	if dropAt == -1 {
		return false
	}

	*cols = append((*cols)[:dropAt], (*cols)[dropAt+1:]...)

	return true
}

// widthsFor computes each visible column's rendered width at the given
// total width: fixed columns get their declared Width, and the first
// Flex column absorbs whatever remains (never below minFlexWidth, and
// never negative).
func widthsFor(cols []Column, width int) []int {
	widths := make([]int, len(cols))

	if len(cols) == 0 {
		return widths
	}

	remaining := width - (len(cols) - 1)
	flexAt := -1

	for i, c := range cols {
		if c.Flex && flexAt == -1 {
			flexAt = i
			continue
		}

		widths[i] = c.Width
		remaining -= c.Width
	}

	if flexAt != -1 {
		// Unlike fits' use of flexFloor to decide whether more columns
		// should be dropped, the actual rendered width here is whatever
		// space is left, clamped to zero — never negative, and never
		// padded back up past floor at the cost of exceeding the
		// requested total width. A request narrower than fixed columns
		// plus their floor (an extreme case fits already tried to avoid
		// by dropping everything droppable) still renders, just with an
		// unusably thin flex column, rather than overflowing width.
		w := remaining
		if w < 0 {
			w = 0
		}

		widths[flexAt] = w
	}

	return widths
}

// visibleWindow picks the slice of row indices [start, end) to render
// given the current selection, row count, and how many data rows fit —
// centring the selection in the viewport when the table is longer than
// the viewport, and recomputed on every call rather than carried as
// stored scroll state, for the same resize-safety reason layout is.
func visibleWindow(rowCount, visibleRows, selected int) (int, int) {
	if visibleRows <= 0 || rowCount <= visibleRows {
		return 0, rowCount
	}

	start := selected - visibleRows/2
	if start < 0 {
		start = 0
	}

	if start > rowCount-visibleRows {
		start = rowCount - visibleRows
	}

	return start, start + visibleRows
}

// View renders the table at width columns and height rows (one of which
// is the header), using th for styling. It is pure: no I/O, no mutation,
// safe to call on every render (AGENT.md §6.8).
func (t Table) View(width, height int, th theme.Theme) string {
	cols, widths := layout(t.Columns, width)

	keyIndex := make(map[string]int, len(t.Columns))
	for i, c := range t.Columns {
		keyIndex[c.Key] = i
	}

	sortKey := ""
	if t.sortCol >= 0 && t.sortCol < len(t.Columns) {
		sortKey = t.Columns[t.sortCol].Key
	}

	var b strings.Builder

	b.WriteString(th.Muted.Render(headerLine(cols, widths, sortKey, t.sortAsc)))

	visibleRows := height - 1
	start, end := visibleWindow(len(t.rows), visibleRows, t.SelectedIndex())

	for i := start; i < end; i++ {
		b.WriteString("\n")
		b.WriteString(rowLine(t.rows[i], cols, widths, keyIndex, t.rows[i].ID == t.selectedID, th))
	}

	return b.String()
}

func headerLine(cols []Column, widths []int, sortKey string, sortAsc bool) string {
	cells := make([]string, len(cols))

	for i, c := range cols {
		title := c.Title
		if sortKey != "" && c.Key == sortKey {
			if sortAsc {
				title += " ^"
			} else {
				title += " v"
			}
		}

		cells[i] = alignCell(title, widths[i], c.Align)
	}

	return strings.Join(cells, " ")
}

func rowLine(r Row, cols []Column, widths []int, keyIndex map[string]int, selected bool, th theme.Theme) string {
	cells := make([]string, len(cols))

	for i, c := range cols {
		cells[i] = alignCell(cellAt(r, keyIndex[c.Key]), widths[i], c.Align)
	}

	line := strings.Join(cells, " ")
	if selected {
		return th.Accent.Render(line)
	}

	return th.Foreground.Render(line)
}

func alignCell(text string, width int, align Align) string {
	truncated := theme.Truncate(text, width)
	if align == AlignRight {
		return padLeft(truncated, width)
	}

	return theme.Pad(truncated, width)
}

// padLeft left-pads s with spaces until it measures w terminal columns
// wide (via theme.Width), the right-align counterpart to theme.Pad. Like
// theme.Pad it never truncates.
func padLeft(s string, w int) string {
	n := theme.Width(s)
	if n >= w {
		return s
	}

	return strings.Repeat(" ", w-n) + s
}
