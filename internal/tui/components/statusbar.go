// Package components holds the shared, reusable pieces of the TUI that no
// single screen owns: the status bar today, the responsive table (T-053)
// and the generic confirm dialog (T-054) later. Everything here is plain
// state plus pure rendering — no I/O, no network, no engine or indexer
// import — so each component is testable on its own without a running
// bubbletea program (AGENT.md §4, §6.8).
package components

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/tui/theme"
)

// DefaultTransientTimeout is how long a transient status-bar message stays
// on screen before the next queued one (or nothing) takes its place
// (T-052 acceptance: "transient messages with a 4s timeout").
const DefaultTransientTimeout = 4 * time.Second

// TickMsg is sent when the currently displayed transient message's timeout
// elapses. StatusBar.Update consumes it to advance the queue. The root
// model must route every TickMsg it receives to StatusBar.Update and
// nowhere else — nothing outside this package should construct or interpret
// one.
type TickMsg struct{}

// StatusBar is the single-line footer AGENT.md §7 specifies: current
// screen, active download count, aggregate transfer rate, a source-error
// indicator, and a queue of transient messages. It is plain state plus a
// pure View — the transient-message timeout is driven entirely by
// tea.Cmd/tea.Tick, never a sleep or a channel receive, so nothing here can
// block a bubbletea Update (AGENT.md §6.1).
//
// The zero value (or New()) is ready to use: no active downloads, no
// rates, no source failures, no transient message.
type StatusBar struct {
	// ActiveDownloads is the count shown as "N active".
	ActiveDownloads int
	// DownRate and UpRate are the aggregate transfer rates across every
	// tracked torrent, in bytes/sec.
	DownRate int64
	UpRate   int64

	// SourcesTotal is how many sources the most recent search fan-out
	// queried. Zero means "no search has run yet" and hides the
	// source-error indicator entirely, even if FailedSources is
	// (impossibly) non-empty.
	SourcesTotal int
	// FailedSources holds the indexer id of every source that failed (or
	// was skipped) in the most recent fan-out, in the registry's own
	// order — this is exactly what the expanded detail view lists.
	FailedSources []string

	// Timeout overrides DefaultTransientTimeout when positive. Tests use
	// this to avoid a real 4-second wait; production code leaves it zero.
	Timeout time.Duration

	current string
	queue   []string
}

// New returns a StatusBar with no active downloads, no rates, no source
// failures, and no transient message — identical to the zero value, spelled
// out for readability at call sites.
func New() StatusBar {
	return StatusBar{}
}

// Message returns the transient message currently on screen, or "" when
// none is showing.
func (s StatusBar) Message() string { return s.current }

// Pending returns the transient messages waiting behind the one currently
// showing, oldest first. It exists for tests that need to assert queuing
// behaviour without waiting out a timeout.
func (s StatusBar) Pending() []string { return s.queue }

func (s StatusBar) timeout() time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}

	return DefaultTransientTimeout
}

// Push queues text as a transient message. A message already on screen is
// never overwritten (T-052 acceptance: "queued rather than overwritten") —
// text is appended behind it and shown once every earlier message's own
// timeout has elapsed. The returned tea.Cmd starts a fresh timeout only
// when text becomes the message actually shown; queuing behind an existing
// one returns a nil Cmd, because exactly one timeout is ever in flight at a
// time and the current message's timeout already covers it.
func (s StatusBar) Push(text string) (StatusBar, tea.Cmd) {
	if s.current == "" {
		s.current = text
		return s, tickCmd(s.timeout())
	}

	s.queue = append(s.queue, text)

	return s, nil
}

// Update advances the transient-message queue on TickMsg: the message that
// just timed out is dropped, and the next queued message (if any) takes
// over with its own fresh timeout. Any other message type is ignored.
func (s StatusBar) Update(msg tea.Msg) (StatusBar, tea.Cmd) {
	if _, ok := msg.(TickMsg); !ok {
		return s, nil
	}

	if len(s.queue) == 0 {
		s.current = ""
		return s, nil
	}

	s.current, s.queue = s.queue[0], s.queue[1:]

	return s, tickCmd(s.timeout())
}

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return TickMsg{} })
}

// DetailLines renders one line per failed/skipped source, for the expanded
// error-detail view opened from the indicator (root.go's
// ActionToggleErrorDetail; see DEC-092).
func (s StatusBar) DetailLines() []string {
	lines := make([]string, 0, len(s.FailedSources))
	for _, id := range s.FailedSources {
		lines = append(lines, "- "+id)
	}

	return lines
}

// View renders the status bar as a single line naming screen, truncated to
// fit width columns (T-052 acceptance: "truncates gracefully at 80
// columns"). It is pure: no I/O, no mutation, safe to call every render.
func (s StatusBar) View(width int, screen string, th theme.Theme) string {
	parts := []string{
		th.Foreground.Render(strings.ToUpper(screen[:1]) + screen[1:]),
		th.Muted.Render(fmt.Sprintf("%d active", s.ActiveDownloads)),
		th.Muted.Render(fmt.Sprintf("down %s up %s", formatRate(s.DownRate), formatRate(s.UpRate))),
	}

	if failed := len(s.FailedSources); s.SourcesTotal > 0 && failed > 0 {
		parts = append(parts, th.Error.Render(fmt.Sprintf("%d/%d sources failed (e to view)", failed, s.SourcesTotal)))
	}

	if s.current != "" {
		parts = append(parts, th.Accent.Render(s.current))
	}

	return theme.Truncate(strings.Join(parts, "  "), width)
}

// formatRate renders bps as a compact human-readable rate. It never returns
// a value longer than "999.9 GB/s" so callers can budget width for it.
func formatRate(bps int64) string {
	const unit = 1024

	switch {
	case bps >= unit*unit*unit:
		return fmt.Sprintf("%.1f GB/s", float64(bps)/float64(unit*unit*unit))
	case bps >= unit*unit:
		return fmt.Sprintf("%.1f MB/s", float64(bps)/float64(unit*unit))
	case bps >= unit:
		return fmt.Sprintf("%.0f KB/s", float64(bps)/float64(unit))
	default:
		return fmt.Sprintf("%d B/s", bps)
	}
}
