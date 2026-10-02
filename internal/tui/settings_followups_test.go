package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/config"
)

// TestSourceWindowIsEmptyForNoRows (T-9105): no rows is the empty range
// 0..0, never first > last.
func TestSourceWindowIsEmptyForNoRows(t *testing.T) {
	for _, height := range []int{0, 24} {
		m := New(newTestEngine(t), testTheme())
		m.height = height

		first, last := m.sourceWindow(0, 3)
		if first != 0 || last != 0 {
			t.Fatalf("height %d: sourceWindow(0, 3) = (%d, %d), want (0, 0)", height, first, last)
		}
	}
}

// TestSourceWindowClampsAStrayCursor (T-9104): a cursor past the last row
// still yields a window that ends at the last row and is not inverted.
func TestSourceWindowClampsAStrayCursor(t *testing.T) {
	m := New(newTestEngine(t), testTheme())
	m.height = 12
	m.settings.cursor = 99

	first, last := m.sourceWindow(5, 2)
	if last != 5 || first >= last {
		t.Fatalf("sourceWindow(5, 2) with cursor 99 = (%d, %d), want a non-empty window ending at 5", first, last)
	}

	m.settings.cursor = -4

	first, last = m.sourceWindow(5, 2)
	if first != 0 || last <= first {
		t.Fatalf("sourceWindow(5, 2) with cursor -4 = (%d, %d), want a window starting at 0", first, last)
	}
}

func threeBuiltins() []BuiltinSource {
	return []BuiltinSource{
		{ID: "b1", Name: "One", Enabled: true},
		{ID: "b2", Name: "Two", Enabled: true},
		{ID: "b3", Name: "Three", Enabled: true},
	}
}

// TestRefreshKeepsTheCursorOnTheSameSource (T-9108): rows ahead of the
// cursor disappearing moves the index, not the source under the cursor.
func TestRefreshKeepsTheCursorOnTheSameSource(t *testing.T) {
	m := settingsModelFor(t, &fakeSourceManager{sources: []config.Indexer{
		{ID: "cfg", Name: "Cfg", Type: "torznab", URL: "https://example.org/a", Enabled: true},
	}})
	m.builtinSnapshot = threeBuiltins()
	m.settings.cursor = 2 // "b2"

	m = m.applyBuiltins(builtinRefresh{rows: threeBuiltins()[1:], ok: true, seq: 1})

	if id, _, ok := m.selectedRow(); !ok || id != "b2" {
		t.Fatalf("cursor on %q (ok=%v) after b1 went, want b2", id, ok)
	}

	// The cursor's own row going falls back to a clamp onto a real row.
	m.settings.cursor = 2 // "b3"
	m = m.applyBuiltins(builtinRefresh{rows: threeBuiltins()[1:2], ok: true, seq: 2})

	if _, _, ok := m.selectedRow(); !ok || m.settings.cursor != 1 {
		t.Fatalf("cursor = %d after its row went, want 1 (clamped)", m.settings.cursor)
	}

	// A configured row under the cursor stays put when built-ins change.
	m.settings.cursor = 0
	m = m.applyBuiltins(builtinRefresh{rows: nil, ok: true, seq: 3})

	if id, _, ok := m.selectedRow(); !ok || id != "cfg" {
		t.Fatalf("cursor on %q, want cfg", id)
	}
}

func builtinRows(aOn, bOn bool) []BuiltinSource {
	return []BuiltinSource{{ID: "a", Name: "A", Enabled: aOn}, {ID: "b", Name: "B", Enabled: bOn}}
}

func builtinEnabled(m Model, id string) (enabled, found bool) {
	for _, b := range m.builtinSnapshot {
		if b.ID == id {
			return b.Enabled, true
		}
	}

	return false, false
}

// TestBuiltinToggleResultCarriesItsOwnRefresh (T-9109): a configured-source
// save that read the rows before the toggle landed delivers stale rows
// while the toggle is in flight; the toggle's result must put the new value
// back, and a stale result arriving after it must not undo it.
func TestBuiltinToggleResultCarriesItsOwnRefresh(t *testing.T) {
	sm := newBuiltinFake()
	sm.builtins = builtinRows(true, true)
	m := settingsModelFor(t, sm)
	m.settings.cursor = 0 // row "a"

	next, cmd := m.toggleBuiltin(m.builtinSnapshot[0])
	m = next.(Model)

	if on, _ := builtinEnabled(m, "a"); on {
		t.Fatal("optimistic flip did not turn a off")
	}

	// The Cmd reads the rows itself, after the save.
	res, ok := cmd().(builtinToggleResultMsg)
	if !ok || !res.builtins.ok || len(res.builtins.rows) != 2 || res.builtins.rows[0].Enabled {
		t.Fatalf("toggle result = %+v, want a refresh with a off", res)
	}

	// Stale save result (read before the toggle) lands first.
	stale := builtinRefresh{rows: builtinRows(true, true), ok: true, seq: res.builtins.seq - 1}
	m = drive(t, m, sourcesSaveResultMsg{builtins: stale})

	// The toggle's result lands second and wins.
	m = drive(t, m, res)
	if on, _ := builtinEnabled(m, "a"); on {
		t.Fatal("a is on after the toggle result, want off")
	}

	// A stale result delivered after the newer one is dropped.
	m = drive(t, m, sourcesSaveResultMsg{builtins: stale})
	if on, _ := builtinEnabled(m, "a"); on {
		t.Fatal("a stale refresh overwrote a newer one")
	}
}

// TestFailedBuiltinToggleRevertsOnlyItsRow (T-9109): the failure handler
// must not restore an older whole snapshot.
func TestFailedBuiltinToggleRevertsOnlyItsRow(t *testing.T) {
	sm := newBuiltinFake()
	sm.builtins = builtinRows(true, true)
	m := settingsModelFor(t, sm)
	m.settings.cursor = 0

	next, _ := m.toggleBuiltin(m.builtinSnapshot[0]) // a: off, optimistic
	m = next.(Model)

	// Meanwhile a configured entry took over "b": a save refreshed the rows.
	m = drive(t, m, sourcesSaveResultMsg{builtins: builtinRefresh{
		rows: []BuiltinSource{{ID: "a", Name: "A", Enabled: false}}, ok: true, seq: 50,
	}})

	m = drive(t, m, builtinToggleResultMsg{err: errors.New("disk full"), id: "a", enabled: false})

	if len(m.builtinSnapshot) != 1 {
		t.Fatalf("rows = %v, want only a (b stays removed)", m.builtinSnapshot)
	}

	if on, _ := builtinEnabled(m, "a"); !on {
		t.Fatal("a was not put back on after the failed save")
	}
}

// TestAggregatorImportWithABundledIDDropsTheBuiltinRow (T-9107).
func TestAggregatorImportWithABundledIDDropsTheBuiltinRow(t *testing.T) {
	sm := newOverrideFake()
	m := settingsModelFor(t, sm)

	if len(m.builtinSnapshot) != 1 {
		t.Fatalf("built-in rows at start = %v, want 1", m.builtinSnapshot)
	}

	imported := config.Indexer{ID: "archive-src", Name: "Imported", Type: "torznab", URL: "https://example.org/a", Enabled: true}
	m.sourcesSnapshot = []config.Indexer{imported} // the wizard's optimistic update
	m = drive(t, m, saveAggregatorImportCmd(sm, []config.Indexer{imported}, nil, 1)())

	if len(m.builtinSnapshot) != 0 {
		t.Fatalf("built-in rows after the import = %v, want none", m.builtinSnapshot)
	}

	if out := view(m); strings.Contains(out, "Archive Source") || !strings.Contains(out, "Imported") {
		t.Fatalf("Settings shows the stale built-in row:\n%s", out)
	}
}

// TestSearchEmptyStateDoesNotBlameDisabledSourcesForAFailedRegistration
// (T-9110): a source that is on but absent from the registry is not "turned
// off".
func TestSearchEmptyStateDoesNotBlameDisabledSourcesForAFailedRegistration(t *testing.T) {
	cases := map[string]SourceManager{
		"configured": &fakeSourceManager{sources: []config.Indexer{
			{ID: "x", Name: "X", Type: "torznab", URL: "https://example.org/a", Enabled: true},
		}},
		"built-in": newBuiltinFake(),
	}

	for name, sm := range cases {
		m := New(newTestEngine(t), testTheme(), WithSourceManager(sm), WithSearcher(&dynamicSearcher{}))
		m = drive(t, m, tea.WindowSizeMsg{Width: 120, Height: 24})

		out := view(m)
		if strings.Contains(out, "turned off") || !strings.Contains(out, "doctor") {
			t.Fatalf("%s: empty state wrong:\n%s", name, out)
		}
	}
}
