package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/engine/fake"
)

// TestStatusBarShowsScreenAndActiveDownloads confirms the footer names the
// current screen and the same active-download count the quit-confirmation
// prompt uses (AGENT.md §7: "current screen, active download count").
func TestStatusBarShowsScreenAndActiveDownloads(t *testing.T) {
	eng := fake.New()
	if _, err := eng.Add(context.Background(), engine.AddSource{Magnet: "magnet:?xt=urn:btih:deadbeef"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	m := New(eng, testTheme())
	m.width, m.height = 80, 24

	updated, _ := m.Update(engineUpdateMsg{statuses: eng.List()})
	mm := updated.(Model)

	view := mm.View()
	if want := "Search"; !strings.Contains(view, want) {
		t.Fatalf("View() = %q, missing screen name %q", view, want)
	}

	if !strings.Contains(view, "1 active") {
		t.Fatalf("View() = %q, expected \"1 active\"", view)
	}
}

// TestStatusBarAggregatesRatesAcrossTorrents confirms the footer's
// down/up figures are the *sum* across every tracked torrent, not just one.
func TestStatusBarAggregatesRatesAcrossTorrents(t *testing.T) {
	m := New(fake.New(), testTheme())
	m.width, m.height = 80, 24

	statuses := []engine.TorrentStatus{
		{ID: "a", DownRate: 1024, UpRate: 512},
		{ID: "b", DownRate: 2048, UpRate: 512},
	}

	updated, _ := m.Update(engineUpdateMsg{statuses: statuses})
	mm := updated.(Model)

	if mm.statusBar.DownRate != 3072 || mm.statusBar.UpRate != 1024 {
		t.Fatalf("aggregate rates = down %d up %d, want down 3072 up 1024",
			mm.statusBar.DownRate, mm.statusBar.UpRate)
	}
}

// TestSourceErrorIndicatorAppearsAndExpands drives sourceStatusMsg directly
// (no screen sends one yet — see root.go's doc comment on the type) and
// confirms the T-052/§6.3 acceptance text verbatim, then that "e" opens the
// detail panel and tab collapses it again (DEC-092).
func TestSourceErrorIndicatorAppearsAndExpands(t *testing.T) {
	m := New(fake.New(), testTheme())
	m.width, m.height = 80, 24

	updated, _ := m.Update(sourceStatusMsg{total: 4, failed: []string{"alpha", "bravo"}})
	mm := updated.(Model)

	view := mm.View()
	if !strings.Contains(view, "2/4 sources failed") {
		t.Fatalf("View() = %q, want the source-error indicator", view)
	}

	updated, _ = mm.Update(keyRune("e"))
	mm = updated.(Model)

	if mm.context() != ContextErrorDetail {
		t.Fatalf("context() = %v, want ContextErrorDetail after \"e\"", mm.context())
	}

	detail := mm.View()
	if !strings.Contains(detail, "alpha") || !strings.Contains(detail, "bravo") {
		t.Fatalf("detail view = %q, want both failed source ids listed", detail)
	}

	// tab collapses the panel — a different action than tab's screen-cycle
	// meaning everywhere else, since ContextErrorDetail is a distinct
	// context (DEC-092). It must not have advanced the screen.
	updated, _ = mm.Update(tea.KeyMsg{Type: tea.KeyTab})
	mm = updated.(Model)

	if mm.context() == ContextErrorDetail {
		t.Fatal("tab should have collapsed the error-detail panel")
	}

	if mm.screen != ScreenSearch {
		t.Fatalf("screen = %v, want unchanged ScreenSearch (tab in ContextErrorDetail must not cycle screens)", mm.screen)
	}
}

// TestErrorIndicatorHiddenWithNoFailures confirms "e" is a no-op when
// nothing has failed — there is nothing to expand into.
func TestErrorIndicatorHiddenWithNoFailures(t *testing.T) {
	m := New(fake.New(), testTheme())
	m.width, m.height = 80, 24

	view := m.View()
	if strings.Contains(view, "sources failed") {
		t.Fatalf("View() = %q, expected no source-error indicator with no search yet", view)
	}

	updated, _ := m.Update(keyRune("e"))
	mm := updated.(Model)

	if mm.errorDetail {
		t.Fatal("\"e\" should be a no-op when no source has failed")
	}
}

// TestErrorIndicatorClearsWhenFailuresResolve confirms a follow-up
// sourceStatusMsg reporting zero failures both hides the indicator and
// force-closes an already-open detail panel, so a stale panel can never
// outlive the condition that opened it.
func TestErrorIndicatorClearsWhenFailuresResolve(t *testing.T) {
	m := New(fake.New(), testTheme())
	m.width, m.height = 80, 24

	updated, _ := m.Update(sourceStatusMsg{total: 2, failed: []string{"alpha"}})
	mm := updated.(Model)

	updated, _ = mm.Update(keyRune("e"))
	mm = updated.(Model)

	if !mm.errorDetail {
		t.Fatal("expected the detail panel to open")
	}

	updated, _ = mm.Update(sourceStatusMsg{total: 2, failed: nil})
	mm = updated.(Model)

	if mm.errorDetail {
		t.Fatal("expected the detail panel to force-close once failures resolve")
	}

	if strings.Contains(mm.View(), "sources failed") {
		t.Fatalf("View() = %q, expected the indicator gone once failures resolve", mm.View())
	}
}

// TestTransientMessagePushedAndQueued confirms transientMessageMsg reaches
// components.StatusBar (T-052's "queued rather than overwritten"), that it
// renders in the footer, and that a second push while the first is showing
// does not replace it.
func TestTransientMessagePushedAndQueued(t *testing.T) {
	m := New(fake.New(), testTheme())
	m.width, m.height = 80, 24

	updated, cmd := m.Update(transientMessageMsg{text: "added ubuntu-24.04.iso"})
	mm := updated.(Model)

	if cmd == nil {
		t.Fatal("expected a non-nil tea.Cmd to start the transient-message timeout")
	}

	if !strings.Contains(mm.View(), "added ubuntu-24.04.iso") {
		t.Fatalf("View() = %q, want the transient message", mm.View())
	}

	updated, cmd = mm.Update(transientMessageMsg{text: "second message"})
	mm2 := updated.(Model)

	if cmd != nil {
		t.Fatal("expected a nil cmd: the first message's timeout is already in flight")
	}

	if strings.Contains(mm2.View(), "second message") {
		t.Fatal("second transient message must be queued, not shown immediately")
	}

	if !strings.Contains(mm2.View(), "added ubuntu-24.04.iso") {
		t.Fatal("first transient message must still be showing")
	}
}

// TestTransientMessageTimeoutIsACommandNeverASleep confirms the timeout is
// driven by a components.TickMsg round-trip through Update, never by
// blocking inside Update itself (AGENT.md §6.1).
func TestTransientMessageTimeoutIsACommandNeverASleep(t *testing.T) {
	m := New(fake.New(), testTheme())
	m.width, m.height = 80, 24
	// A short override so this test doesn't burn the real 4s default
	// waiting on tea.Tick's own timer; the timeout *value* is pinned
	// separately by components.TestStatusBarDefaultTimeoutIsFourSeconds.
	m.statusBar.Timeout = time.Millisecond

	updated, cmd := m.Update(transientMessageMsg{text: "first"})
	mm := updated.(Model)

	if cmd == nil {
		t.Fatal("expected a tea.Cmd, not an inline sleep")
	}

	msg := cmd()

	updated, _ = mm.Update(msg)
	mm = updated.(Model)

	if strings.Contains(mm.View(), "first") {
		t.Fatalf("expected the message gone after its TickMsg fired, view = %q", mm.View())
	}
}
