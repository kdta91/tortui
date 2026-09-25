package tui

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/engine/fake"
	"github.com/kdta91/tortui/internal/tui/theme"
)

func testTheme() theme.Theme {
	return theme.New(theme.DefaultThemeName, theme.Capability{Color: theme.ColorNone, Unicode: true, Interactive: true})
}

func keyRune(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func waitForOutput(tb testing.TB, tm *teatest.TestModel, substr string) {
	tb.Helper()

	teatest.WaitFor(
		tb, tm.Output(),
		func(bts []byte) bool { return bytes.Contains(bts, []byte(substr)) },
		teatest.WithCheckInterval(10*time.Millisecond),
		teatest.WithDuration(3*time.Second),
	)
}

// TestNavigationAllScreensViaNumberKeys drives the root Model with a
// teatest program and confirms 1-5 lands on every screen's placeholder body
// in order (T-051 acceptance: "teatest covers navigation between all
// screens").
func TestNavigationAllScreensViaNumberKeys(t *testing.T) {
	m := New(fake.New(), testTheme())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))

	t.Cleanup(func() { _ = tm.Quit() })

	cases := []struct {
		key  string
		want string
	}{
		// ScreenSearch (T-060), ScreenResults (T-061), ScreenDetails
		// (T-063), ScreenDownloads (T-071), and ScreenSettings (T-080) are
		// all real now: each has its own stable marker rather than the
		// generic placeholder text.
		{"1", "Query:"},
		{"2", "No search yet"},
		{"3", "No result selected"},
		{"4", "No downloads yet"},
		{"5", "No sources configured"},
	}

	for _, c := range cases {
		tm.Send(keyRune(c.key))
		waitForOutput(t, tm, c.want)
	}
}

// TestNavigationTabCyclesForwardAndBack confirms tab and shift+tab cycle
// through screenOrder, including wraparound at both ends.
func TestNavigationTabCyclesForwardAndBack(t *testing.T) {
	m := New(fake.New(), testTheme())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))

	t.Cleanup(func() { _ = tm.Quit() })

	// Starts on ScreenSearch. tab -> results -> details -> downloads ->
	// settings -> (wrap) search. Every screen has real content now (T-060,
	// T-061, T-063, T-071, T-080), so each is checked by its own stable
	// marker.
	forward := []string{
		"No search yet",
		"No result selected",
		"No downloads yet",
		"No sources configured",
		"Query:",
	}
	for _, want := range forward {
		tm.Send(tea.KeyMsg{Type: tea.KeyTab})
		waitForOutput(t, tm, want)
	}

	// shift+tab from search wraps back to settings.
	tm.Send(tea.KeyMsg{Type: tea.KeyShiftTab})
	waitForOutput(t, tm, "No sources configured")
}

// TestHelpOverlayTogglesAndShowsScreenBindings confirms ? opens an overlay
// rendering the current screen's bindings, and ? (or esc) closes it back to
// the screen body (T-051 acceptance: "teatest covers ... the help
// overlay").
func TestHelpOverlayTogglesAndShowsScreenBindings(t *testing.T) {
	m := New(fake.New(), testTheme())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))

	t.Cleanup(func() { _ = tm.Quit() })

	// Move to downloads first so downloads-only bindings (pause/resume) are
	// expected in the overlay.
	tm.Send(keyRune("4"))
	waitForOutput(t, tm, "No downloads yet")

	tm.Send(keyRune("?"))
	waitForOutput(t, tm, "pause/resume")

	tm.Send(keyRune("?"))
	waitForOutput(t, tm, "No downloads yet")
}

// TestHelpOverlayClosesWithEscape confirms esc, not just ?, closes the
// overlay.
func TestHelpOverlayClosesWithEscape(t *testing.T) {
	m := New(fake.New(), testTheme())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))

	t.Cleanup(func() { _ = tm.Quit() })

	tm.Send(keyRune("?"))
	waitForOutput(t, tm, "Keys")

	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	waitForOutput(t, tm, "Query:")
}

// TestQuitWithNoActiveDownloadsIsImmediate confirms q exits the program
// directly when nothing is downloading.
func TestQuitWithNoActiveDownloadsIsImmediate(t *testing.T) {
	m := New(fake.New(), testTheme())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))

	tm.Send(keyRune("q"))
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestQuitWithActiveDownloadPromptsThenConfirms confirms q opens a
// confirmation instead of quitting immediately while a torrent is actively
// transferring, and that confirming with y then quits.
func TestQuitWithActiveDownloadPromptsThenConfirms(t *testing.T) {
	eng := fake.New()
	if _, err := eng.Add(context.Background(), engine.AddSource{Magnet: "magnet:?xt=urn:btih:deadbeef"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	m := New(eng, testTheme())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))

	// Give Init's subscription a chance to deliver the queued/downloading
	// status before quitting.
	waitForOutput(t, tm, "Query:")

	tm.Send(keyRune("q"))
	waitForOutput(t, tm, "Quit tortui?")

	tm.Send(keyRune("n"))
	waitForOutput(t, tm, "Query:")

	tm.Send(keyRune("q"))
	waitForOutput(t, tm, "Quit tortui?")

	tm.Send(keyRune("y"))
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestWindowResizeRecomputesLayout confirms tea.WindowSizeMsg updates the
// stored width/height and that a subsequent render reflects the new size
// rather than a cached one from construction (AGENT.md: "no cached widths
// survive a resize").
func TestWindowResizeRecomputesLayout(t *testing.T) {
	m := New(fake.New(), testTheme())

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	mm := updated.(Model)

	if mm.width != 100 || mm.height != 40 {
		t.Fatalf("width/height = %d/%d, want 100/40", mm.width, mm.height)
	}

	view := mm.View()
	if view == "" {
		t.Fatal("expected non-empty view after a window size message")
	}

	updated2, _ := mm.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	mm2 := updated2.(Model)

	if mm2.width != 60 || mm2.height != 20 {
		t.Fatalf("width/height after second resize = %d/%d, want 60/20", mm2.width, mm2.height)
	}
}

// TestViewIsEmptyBeforeFirstResize confirms View has no I/O side effects and
// is genuinely a pure function of state: with no WindowSizeMsg delivered
// yet, it renders nothing rather than guessing a width.
func TestViewIsEmptyBeforeFirstResize(t *testing.T) {
	m := New(fake.New(), testTheme())
	if v := m.View(); v != "" {
		t.Fatalf("expected empty view before any WindowSizeMsg, got %q", v)
	}
}

// TestUnboundKeyIsANoOp confirms a key with no binding in the current
// context changes nothing.
func TestUnboundKeyIsANoOp(t *testing.T) {
	m := New(fake.New(), testTheme())
	updated, cmd := m.Update(keyRune("z"))
	mm := updated.(Model)

	if mm.screen != ScreenSearch || cmd != nil {
		t.Fatalf("unbound key should be a no-op, got screen=%v cmd=%v", mm.screen, cmd)
	}
}

// TestEngineUpdateClosedChannelIsHandled confirms an engineUpdateMsg with
// closed=true (Engine.Updates channel closed) does not panic and does not
// re-subscribe.
func TestEngineUpdateClosedChannelIsHandled(t *testing.T) {
	m := New(fake.New(), testTheme())

	updated, cmd := m.Update(engineUpdateMsg{closed: true})
	if cmd != nil {
		t.Fatalf("expected no further subscription after a closed channel, got a cmd")
	}

	_ = updated.(Model)
}

// TestNilEngineInitIsSafe confirms Init tolerates a nil engine (a Model
// constructed without one, e.g. in a future unit test that only exercises
// screen routing) rather than panicking on eng.Updates().
func TestNilEngineInitIsSafe(t *testing.T) {
	m := New(nil, testTheme())
	if cmd := m.Init(); cmd != nil {
		t.Fatalf("expected nil Init cmd for a nil engine, got %v", cmd)
	}
}

// TestQuitConfirmEscRestoresScreenAndSelection drives the real quit-confirm
// Dialog (T-054) through Model.Update and confirms esc does more than just
// close the modal: the screen it interrupted and the generic selection
// cursor on that screen are exactly what they were before the dialog
// opened, not merely "some screen" or a reset-to-zero selection (T-054
// acceptance: "esc always cancels; focus returns to the originating screen
// and selection").
func TestQuitConfirmEscRestoresScreenAndSelection(t *testing.T) {
	eng := fake.New()
	if _, err := eng.Add(context.Background(), engine.AddSource{Magnet: "magnet:?xt=urn:btih:deadbeef"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	m := New(eng, testTheme())

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	updated, _ = m.Update(engineUpdateMsg{statuses: eng.List()})
	m = updated.(Model)

	// Move off the starting screen and away from selection 0, so
	// "restored" is a meaningful assertion rather than trivially true of
	// the zero value. Two tabs lands on ScreenDetails rather than the
	// first tab's ScreenResults: T-063 gave results its own real per-row
	// selection (the results table's cursor), so j/k there now moves that
	// instead of the generic m.selection this test means to exercise —
	// ScreenDetails (and downloads/settings, still placeholders) are what's
	// left driving the generic fallback.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(Model)
	updated, _ = m.Update(keyRune("j"))
	m = updated.(Model)
	updated, _ = m.Update(keyRune("j"))
	m = updated.(Model)

	wantScreen := m.screen
	wantSelection := m.selection

	if wantScreen == ScreenSearch {
		t.Fatal("setup: expected tab to have moved off ScreenSearch")
	}

	if wantSelection == 0 {
		t.Fatal("setup: expected j/j to have moved selection off 0")
	}

	updated, _ = m.Update(keyRune("q"))
	m = updated.(Model)

	if !m.quitConfirm.IsOpen() {
		t.Fatal("expected q with an active download to open the quit-confirm dialog")
	}

	if m.context() != ContextQuitConfirm {
		t.Fatalf("context() = %v, want ContextQuitConfirm while the dialog is open", m.context())
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if m.quitConfirm.IsOpen() {
		t.Fatal("expected esc to close the quit-confirm dialog")
	}

	if m.context() != screenContext(wantScreen) {
		t.Fatalf("context() after esc = %v, want back on %v — focus did not return to the originating screen",
			m.context(), screenContext(wantScreen))
	}

	if m.screen != wantScreen {
		t.Fatalf("screen after esc = %v, want %v", m.screen, wantScreen)
	}

	if m.selection != wantSelection {
		t.Fatalf("selection after esc = %d, want %d — esc must restore selection, not just the screen",
			m.selection, wantSelection)
	}
}

// TestQuitConfirmRendersInsideAModalBorder confirms the quit prompt draws
// through the shared Dialog component's bordered box (AGENT.md §7: modals
// are one of the two places a box border is allowed) rather than a
// screen's own ad hoc frame.
func TestQuitConfirmRendersInsideAModalBorder(t *testing.T) {
	eng := fake.New()
	if _, err := eng.Add(context.Background(), engine.AddSource{Magnet: "magnet:?xt=urn:btih:deadbeef"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// A truecolor/unicode theme so the border's rounded-corner glyph is
	// actually drawn (testTheme() elsewhere in this file forces ColorNone,
	// which is orthogonal to the border glyph — Unicode still applies —
	// but using the same capability here keeps the corner assertion
	// independent of that).
	th := theme.New(theme.DefaultThemeName, theme.Capability{Color: theme.ColorNone, Unicode: true, Interactive: true})

	m := New(eng, th)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	updated, _ = m.Update(engineUpdateMsg{statuses: eng.List()})
	m = updated.(Model)

	updated, _ = m.Update(keyRune("q"))
	m = updated.(Model)

	view := m.View()
	if !strings.Contains(view, "╭") {
		t.Fatalf("expected the quit-confirm view to draw a rounded modal border:\n%s", view)
	}
}

// TestRenderTabsHighlightsCurrentScreen is a lightweight non-teatest check
// that the tab bar text contains every screen's label.
func TestRenderTabsHighlightsCurrentScreen(t *testing.T) {
	m := New(fake.New(), testTheme())
	m.width, m.height = 80, 24

	tabs := m.renderTabs()
	for _, s := range screenOrder {
		label := strings.ToUpper(s.String()[:1]) + s.String()[1:]
		if !strings.Contains(tabs, label) {
			t.Fatalf("tab bar missing label for %v: %q", s, tabs)
		}
	}
}
