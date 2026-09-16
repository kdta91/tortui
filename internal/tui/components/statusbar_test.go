package components

import (
	"strings"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/tui/theme"
)

func testTheme() theme.Theme {
	return theme.New(theme.DefaultThemeName, theme.Capability{Color: theme.ColorNone, Unicode: true, Interactive: true})
}

// TestStatusBarPushShowsFirstMessageImmediately confirms the first Push
// while nothing is showing both displays the message right away and returns
// a non-nil tea.Cmd to start its timeout.
func TestStatusBarPushShowsFirstMessageImmediately(t *testing.T) {
	sb := New()

	sb, cmd := sb.Push("added ubuntu-24.04.iso")
	if sb.Message() != "added ubuntu-24.04.iso" {
		t.Fatalf("Message() = %q, want the pushed text", sb.Message())
	}

	if cmd == nil {
		t.Fatal("expected a non-nil tea.Cmd to start the timeout")
	}
}

// TestStatusBarPushQueuesRatherThanOverwrites is the T-052 acceptance
// criterion by name: a second Push while a message is already showing must
// not replace it, and must not start a second, redundant timeout.
func TestStatusBarPushQueuesRatherThanOverwrites(t *testing.T) {
	sb := New()

	sb, _ = sb.Push("first")

	sb, cmd := sb.Push("second")
	if sb.Message() != "first" {
		t.Fatalf("second Push overwrote the current message: Message() = %q, want %q", sb.Message(), "first")
	}

	if cmd != nil {
		t.Fatal("expected a nil cmd: a timeout for the current message is already in flight")
	}

	if pending := sb.Pending(); len(pending) != 1 || pending[0] != "second" {
		t.Fatalf("Pending() = %v, want [\"second\"]", pending)
	}
}

// TestStatusBarTickAdvancesQueue confirms a TickMsg (the timeout firing)
// drops the expired message and promotes the next queued one, and that
// running out of queue clears the display entirely.
func TestStatusBarTickAdvancesQueue(t *testing.T) {
	sb := New()

	sb, _ = sb.Push("first")
	sb, _ = sb.Push("second")

	sb, cmd := sb.Update(TickMsg{})
	if sb.Message() != "second" {
		t.Fatalf("after one tick, Message() = %q, want %q", sb.Message(), "second")
	}

	if cmd == nil {
		t.Fatal("expected a fresh timeout cmd for the newly-promoted message")
	}

	if len(sb.Pending()) != 0 {
		t.Fatalf("expected an empty queue after promoting its only entry, got %v", sb.Pending())
	}

	sb, cmd = sb.Update(TickMsg{})
	if sb.Message() != "" {
		t.Fatalf("after the queue drains, Message() = %q, want empty", sb.Message())
	}

	if cmd != nil {
		t.Fatal("expected no further timeout once nothing is showing")
	}
}

// TestStatusBarUpdateIgnoresOtherMessages confirms Update only reacts to
// TickMsg, never mutating on an unrelated message type.
func TestStatusBarUpdateIgnoresOtherMessages(t *testing.T) {
	sb := New()
	sb, _ = sb.Push("first")

	sb2, cmd := sb.Update("not a tick")
	if sb2.Message() != "first" || cmd != nil {
		t.Fatalf("non-TickMsg mutated the bar: Message()=%q cmd=%v", sb2.Message(), cmd)
	}
}

// TestStatusBarDefaultTimeoutIsFourSeconds pins the constant the acceptance
// criterion names explicitly, so a change to it is a deliberate, reviewable
// edit rather than an incidental one.
func TestStatusBarDefaultTimeoutIsFourSeconds(t *testing.T) {
	if DefaultTransientTimeout != 4*time.Second {
		t.Fatalf("DefaultTransientTimeout = %v, want 4s", DefaultTransientTimeout)
	}

	sb := New()
	if got := sb.timeout(); got != 4*time.Second {
		t.Fatalf("zero-value StatusBar.timeout() = %v, want the 4s default", got)
	}

	sb.Timeout = 10 * time.Millisecond
	if got := sb.timeout(); got != 10*time.Millisecond {
		t.Fatalf("StatusBar.timeout() with an override = %v, want the override", got)
	}
}

// TestStatusBarViewIncludesCoreFields confirms the one-line footer names the
// screen, the active-download count, and the aggregate rate, per AGENT.md
// §7's "current screen, active download count, aggregate down/up rate".
func TestStatusBarViewIncludesCoreFields(t *testing.T) {
	sb := StatusBar{ActiveDownloads: 3, DownRate: 12_400_000, UpRate: 880 * 1024}

	view := sb.View(120, "downloads", testTheme())

	for _, want := range []string{"Downloads", "3 active", "down 11.8 MB/s", "up 880 KB/s"} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() = %q, missing %q", view, want)
		}
	}
}

// TestStatusBarViewHidesIndicatorWithNoSearchYet confirms the source-error
// indicator only appears once a search fan-out has actually run
// (SourcesTotal > 0) — before that, showing "0/0 sources failed" would be
// meaningless noise on every screen.
func TestStatusBarViewHidesIndicatorWithNoSearchYet(t *testing.T) {
	sb := StatusBar{}

	if view := sb.View(80, "search", testTheme()); strings.Contains(view, "sources failed") {
		t.Fatalf("expected no source-error indicator before any search, got %q", view)
	}
}

// TestStatusBarViewShowsSourceErrorIndicator is AGENT.md §6.3's exact
// wording, adapted for the actual key binding this task chose (DEC-092):
// "2/4 sources failed (e to view)".
func TestStatusBarViewShowsSourceErrorIndicator(t *testing.T) {
	sb := StatusBar{SourcesTotal: 4, FailedSources: []string{"alpha", "bravo"}}

	view := sb.View(80, "results", testTheme())
	if !strings.Contains(view, "2/4 sources failed (e to view)") {
		t.Fatalf("View() = %q, want the source-error indicator", view)
	}
}

// TestStatusBarDetailLinesListsEveryFailedSource confirms the expanded view
// names every failed source, not just the count.
func TestStatusBarDetailLinesListsEveryFailedSource(t *testing.T) {
	sb := StatusBar{SourcesTotal: 3, FailedSources: []string{"alpha", "bravo"}}

	lines := sb.DetailLines()
	if len(lines) != 2 || !strings.Contains(lines[0], "alpha") || !strings.Contains(lines[1], "bravo") {
		t.Fatalf("DetailLines() = %v, want one line per failed source", lines)
	}
}

// TestStatusBarViewTruncatesAt80Columns is the third T-052 acceptance
// criterion by name: an overloaded line (long transient message, several
// failed sources) never renders wider than the requested width, and a
// genuinely-fitting line is left alone.
func TestStatusBarViewTruncatesAt80Columns(t *testing.T) {
	sb := StatusBar{
		ActiveDownloads: 2,
		DownRate:        1024,
		UpRate:          1024,
		SourcesTotal:    5,
		FailedSources:   []string{"alpha", "bravo", "charlie"},
		current:         strings.Repeat("a very long transient message indeed ", 5),
	}

	view := sb.View(80, "downloads", testTheme())
	if w := theme.Width(view); w > 80 {
		t.Fatalf("View() width = %d, want <= 80: %q", w, view)
	}

	if !strings.Contains(view, "...") {
		t.Fatalf("expected an overflowing line to end in an ellipsis, got %q", view)
	}

	short := StatusBar{ActiveDownloads: 1}
	shortView := short.View(80, "search", testTheme())

	if strings.Contains(shortView, "...") {
		t.Fatalf("a line well under 80 columns should not be truncated: %q", shortView)
	}

	if w := theme.Width(shortView); w > 80 {
		t.Fatalf("short View() width = %d, want <= 80", w)
	}
}
