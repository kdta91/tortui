package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// rowOrder is the row ids of m's table, top first.
func rowOrder(m resultsModel) []string {
	ids := make([]string, 0, len(m.table.Rows()))
	for _, r := range m.table.Rows() {
		ids = append(ids, r.ID)
	}

	return ids
}

func equalIDs(a, b []string) bool { return strings.Join(a, ",") == strings.Join(b, ",") }

// undatedResults has one result with no Published date between two dated ones.
func undatedResults(now time.Time) []indexer.Result {
	return []indexer.Result{
		{IndexerID: "alpha", ID: "old", Title: "old", Seeders: 1, Published: now.Add(-72 * time.Hour)},
		{IndexerID: "alpha", ID: "undated", Title: "undated", Seeders: 9},
		{IndexerID: "alpha", ID: "new", Title: "new", Seeders: 5, Published: now.Add(-time.Hour)},
	}
}

// TestUndatedResultsSortLastInBothDirections pins T-957: a result with no
// Published date is neither the newest nor the oldest, so it ends the Age
// column whichever way it is sorted, the Latest default included.
func TestUndatedResultsSortLastInBothDirections(t *testing.T) {
	now := time.Now()
	m := newResultsModel().setResults(undatedResults(now), indexer.ModeLatest, now, theme.UnicodeGlyphs)

	if got, want := rowOrder(m), []string{"alpha|new", "alpha|old", "alpha|undated"}; !equalIDs(got, want) {
		t.Fatalf("Latest default (age ascending) = %v, want %v", got, want)
	}

	m = m.reverseSort()
	if m.table.SortAscending() {
		t.Fatal("reverseSort left the age sort ascending")
	}

	if got, want := rowOrder(m), []string{"alpha|old", "alpha|new", "alpha|undated"}; !equalIDs(got, want) {
		t.Fatalf("age descending = %v, want %v", got, want)
	}
}

// TestFormatSizeNeverExceedsTheSizeColumn pins T-958: no size, near a unit
// boundary or at the extremes, renders wider than its 8-cell column.
func TestFormatSizeNeverExceedsTheSizeColumn(t *testing.T) {
	width := 0

	for _, c := range resultsColumns() {
		if c.Key == "size" {
			width = c.Width
		}
	}

	if width == 0 {
		t.Fatal("no size column")
	}

	cases := map[int64]string{
		1023:                        "1023 B",
		1024*1024 - 1:               "1023 KB",
		1000 * 1024:                 "1000 KB",
		1024*1024*1024 - 1:          "1023 MB",
		1023*1024*1024 + 100*1024:   "1023 MB",
		999*1024*1024 + 1024*1024/2: "999.5 MB",
		1024 * 1024:                 "1.0 MB",
		1024 * 1024 * 1024:          "1.0 GB",
		1024*1024*1024*1024 - 1:     "1023 GB",
	}

	for n, want := range cases {
		if got := formatSize(n); got != want {
			t.Errorf("formatSize(%d) = %q, want %q", n, got, want)
		}
	}

	for _, n := range []int64{0, 1, 1023, 1024, 1<<20 - 1, 1000 << 20, 1<<30 - 1, 1<<40 - 1, 1 << 50, 1<<62 + 5, 1<<63 - 1} {
		if got := formatSize(n); theme.Width(got) > width {
			t.Errorf("formatSize(%d) = %q is %d cells, over the %d-cell column", n, got, theme.Width(got), width)
		}
	}
}

// TestTrustColumnIsAccentColoured pins T-960: the Trust badge stays in the
// accent colour on an unselected row (components.Column.Accent).
func TestTrustColumnIsAccentColoured(t *testing.T) {
	for _, c := range resultsColumns() {
		if c.Key == "trust" {
			if !c.Accent {
				t.Fatal("trust column does not set Accent")
			}

			return
		}
	}

	t.Fatal("no trust column")
}

// TestFirstSortOntoTrustIsDescending pins T-962 (DEC-179): s from the
// default sort lands on Trust with the most trusted rows first, and S flips it.
func TestFirstSortOntoTrustIsDescending(t *testing.T) {
	now := time.Now()
	m := newResultsModel().setResults(sampleResultsForTrust(now), indexer.ModeSearch, now, theme.UnicodeGlyphs)

	m = m.cycleSort()
	if m.table.SortColumn() != colTrust || m.table.SortAscending() {
		t.Fatalf("first s: column %d ascending=%v, want Trust descending", m.table.SortColumn(), m.table.SortAscending())
	}

	if got := trustOrderOf(m.table.Rows()[0]); got != int(indexer.TrustVIP) {
		t.Fatalf("top row trust = %d, want VIP first", got)
	}

	m = m.reverseSort()
	if !m.table.SortAscending() {
		t.Fatal("S did not flip Trust to ascending")
	}

	// Other columns still start ascending.
	m = m.cycleSort()
	if m.table.SortColumn() != colAge || !m.table.SortAscending() {
		t.Fatalf("next s: column %d ascending=%v, want Age ascending", m.table.SortColumn(), m.table.SortAscending())
	}
}

// TestSetResultsSelectsTheTopRowAfterTheDefaultSort pins T-963, for a first
// result set and for a replacement that still holds the old selection.
func TestSetResultsSelectsTheTopRowAfterTheDefaultSort(t *testing.T) {
	now := time.Now()
	results := sampleResultsForSort(now)

	m := newResultsModel().setResults(results, indexer.ModeSearch, now, theme.UnicodeGlyphs)
	if top := m.table.Rows()[0].ID; m.table.SelectedID() != top {
		t.Fatalf("first set: selected %q, want the top row %q", m.table.SelectedID(), top)
	}

	// Move off the top, then load the same results again: the old selection
	// survives by identity unless a fresh set resets it.
	m.table = m.table.MoveDown()
	if m.table.SelectedID() == m.table.Rows()[0].ID {
		t.Fatal("test setup: MoveDown did not leave the top row")
	}

	m = m.setResults(results, indexer.ModeLatest, now, theme.UnicodeGlyphs)
	if top := m.table.Rows()[0].ID; m.table.SelectedID() != top {
		t.Fatalf("replacement set: selected %q, want the top row %q", m.table.SelectedID(), top)
	}
}

// goldenResults is the dataset of the results goldens, run through the real
// setResults, resultRow and formatSwarm path (T-9023): an unreported swarm,
// an undated result, a size at a unit boundary, and a CJK and emoji title.
func goldenResults(now time.Time) []indexer.Result {
	hours := func(h int) time.Time { return now.Add(-time.Duration(h) * time.Hour) }

	return []indexer.Result{
		{
			IndexerID: "archive-src", ID: "1", Title: "debian-13.0.0-amd64-netinst.iso", SizeBytes: 702 << 20,
			Seeders: 3421, Leechers: 12, Category: indexer.CategorySoftware, Trust: indexer.TrustVIP, Published: hours(3),
		},
		{
			IndexerID: "research-repo", ID: "2", Title: "示例种子🎬.iso", SizeBytes: 4700 << 20,
			Seeders: 128, Leechers: 4, Category: indexer.CategoryVideo, Trust: indexer.TrustTrusted, Published: hours(48),
		},
		{
			IndexerID: "example.org", ID: "3", Title: "a research dataset release, volume two", SizeBytes: 18<<30 + 200<<20,
			Seeders: 56, Leechers: 3, Category: indexer.CategoryData, Trust: indexer.TrustVerified, Published: hours(120),
		},
		{
			IndexerID: "example.org", ID: "4", Title: "boundary-size.bin", SizeBytes: 1023<<20 + 900<<10,
			Seeders: 1, Category: indexer.CategoryOther, Published: hours(24 * 400),
		},
		{
			IndexerID: "example.org", ID: "5", Title: "unreported-swarm-undated.tar", SizeBytes: 3 << 20,
			Category: indexer.CategoryText,
			Extra:    map[string]string{indexer.ExtraKeySeedersUnknown: "1", indexer.ExtraKeyLeechersUnknown: "1"},
		},
	}
}

func compareResultsGolden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name)

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o750); err != nil {
			t.Fatalf("creating testdata: %v", err)
		}

		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatalf("writing golden file %s: %v", path, err)
		}

		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file %s: %v (UPDATE_GOLDEN=1 go test ./internal/tui creates it)", path, err)
	}

	if got != string(want) {
		t.Fatalf("golden mismatch for %s:\n--- want ---\n%s--- got ---\n%s", path, want, got)
	}
}

// TestResultsTableGoldens renders the real results model at the three
// documented sizes (T-9023).
func TestResultsTableGoldens(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

	for _, sz := range []struct{ w, h int }{{80, 24}, {120, 40}, {60, 20}} {
		m := newResultsModel().setResults(goldenResults(now), indexer.ModeSearch, now, theme.UnicodeGlyphs)
		compareResultsGolden(t, fmt.Sprintf("results_%dx%d.golden", sz.w, sz.h), m.table.View(sz.w, sz.h, testTheme()))
	}
}

// TestUndatedResultsKeepTheTieBreakOrder: undated rows all sort last, and
// among themselves in the table's total order (more seeders, then title, then
// id), in both directions (T-957).
func TestUndatedResultsKeepTheTieBreakOrder(t *testing.T) {
	now := time.Now()
	results := []indexer.Result{
		{IndexerID: "alpha", ID: "dated", Title: "dated", Seeders: 1, Published: now.Add(-time.Hour)},
		{IndexerID: "alpha", ID: "u-low", Title: "low", Seeders: 2},
		{IndexerID: "alpha", ID: "u-high", Title: "high", Seeders: 40},
		{IndexerID: "alpha", ID: "u-mid-b", Title: "Bravo", Seeders: 9},
		{IndexerID: "alpha", ID: "u-mid-a", Title: "alpha", Seeders: 9},
	}
	want := []string{"alpha|dated", "alpha|u-high", "alpha|u-mid-a", "alpha|u-mid-b", "alpha|u-low"}

	m := newResultsModel().setResults(results, indexer.ModeLatest, now, theme.UnicodeGlyphs)
	if got := rowOrder(m); !equalIDs(got, want) {
		t.Fatalf("age ascending = %v, want %v", got, want)
	}

	m = m.reverseSort()
	if got := rowOrder(m); !equalIDs(got, want) {
		t.Fatalf("age descending = %v, want %v", got, want)
	}
}
