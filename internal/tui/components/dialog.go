package components

import (
	"log/slog"
	"strings"

	"github.com/kdta91/tortui/internal/tui/theme"
)

// DialogOption is one choice a Dialog offers. It is deliberately just a
// label — a Dialog knows nothing about torrents, indexers, or any other
// domain type (AGENT.md §4: internal/tui imports indexer/engine
// interfaces only, and a shared component stays domain-agnostic on top of
// that). The key(s) that pick an option directly (e.g. "y"/"n") are a
// screen's own keymap binding, routed to ActionConfirmYes/ActionConfirmNo
// or similar by whatever owns the dialog — Dialog itself only tracks which
// option is highlighted and which one a caller has committed to.
type DialogOption struct {
	// Label is the option's display text, e.g. "Keep data" or "Cancel".
	Label string
}

// Dialog is tortui's one generic modal mechanism (T-054): a title, a
// message, a set of configurable options, and a default. It is plain state
// plus a pure View, like StatusBar — no I/O, no bubbletea import, safe to
// unit test on its own (AGENT.md §6.8).
//
// AGENT.md §7 allows "never more than one modal deep." Dialog enforces its
// half of that itself: Open on an already-open Dialog is a programming
// error — the caller's own state machine let two modals overlap — and it
// is handled as a logged no-op rather than silently stacking a second
// prompt on top of the first (see Open).
type Dialog struct {
	// Title is the short heading shown at the top of the box.
	Title string
	// Message is the body text shown under Title.
	Message string
	// Options are the choices offered, in display order. Confirm and
	// SelectedOption index into this slice.
	Options []DialogOption
	// Default is the index into Options highlighted when the dialog opens
	// and restored whenever it closes (Cancel or Confirm), so a dialog
	// reopened later always starts fresh at Default rather than wherever
	// a previous invocation's cursor was left.
	Default int

	// Logger receives the warning Open emits when called on an
	// already-open Dialog. nil defaults to slog.Default() at call time —
	// the same convention internal/store and internal/indexer/httpx use —
	// so the record always reaches whatever sink log/slog's default
	// currently points at (the rotating file sink installed by
	// internal/logging in production) and never stdout/stderr, which
	// would corrupt the TUI (AGENT.md §3, §6.9).
	Logger *slog.Logger

	open     bool
	selected int
}

// NewDialog builds a Dialog with the given title, message, and options,
// highlighting def initially. An out-of-range def (including the zero
// value for an empty Options slice) falls back to 0 rather than panicking
// later inside View or Confirm.
func NewDialog(title, message string, options []DialogOption, def int) Dialog {
	if def < 0 || def >= len(options) {
		def = 0
	}

	return Dialog{
		Title:    title,
		Message:  message,
		Options:  options,
		Default:  def,
		selected: def,
	}
}

// IsOpen reports whether the dialog is currently showing.
func (d Dialog) IsOpen() bool { return d.open }

// SelectedIndex returns the index of the currently highlighted option.
// Meaningless (but safe — always in range when Options is non-empty)
// while the dialog is closed.
func (d Dialog) SelectedIndex() int { return d.selected }

// SelectedOption returns the currently highlighted option and true, or the
// zero DialogOption and false when Options is empty.
func (d Dialog) SelectedOption() (DialogOption, bool) {
	if len(d.Options) == 0 {
		return DialogOption{}, false
	}

	return d.Options[d.selected], true
}

// Open shows the dialog, resetting the highlighted option to Default. It
// is a no-op — the receiver is returned unchanged — when the dialog is
// already open, which is a programming error in the caller (its own state
// machine let a second modal try to open on top of a first one) rather
// than something a user action alone can trigger through tortui's own
// context-scoped keymap (internal/tui/keymap.go). That no-op is logged via
// log/slog at warn level, through Logger (or slog.Default()) — never
// swallowed with "_", and never printed to stdout/stderr, so it is visible
// in the rotating log file without corrupting the TUI (AGENT.md §6.9, §3).
func (d Dialog) Open() Dialog {
	if d.open {
		d.log().Warn("dialog: ignoring Open on an already-open dialog",
			slog.String("title", d.Title))

		return d
	}

	d.open = true
	d.selected = d.Default

	return d
}

// Cancel closes the dialog without committing to any option, restoring the
// highlighted option to Default so a later Open starts fresh rather than
// remembering this invocation's cursor position. Closing an already-closed
// dialog is a harmless no-op.
func (d Dialog) Cancel() Dialog {
	d.open = false
	d.selected = d.Default

	return d
}

// Confirm commits to whichever option is currently highlighted, closes the
// dialog (restoring the cursor to Default, same as Cancel), and returns
// the committed index alongside the updated Dialog. Calling Confirm on a
// closed dialog still returns a defined answer (the current — i.e.
// Default — selection) rather than an undefined one, since a caller
// mistakenly confirming a closed dialog is a bug worth a sane result, not
// worth a panic (AGENT.md §6.9: never panic outside main).
func (d Dialog) Confirm() (Dialog, int) {
	chosen := d.selected

	d.open = false
	d.selected = d.Default

	return d, chosen
}

// MoveNext highlights the next option, wrapping around. A no-op while
// closed or when there are fewer than two options to move between.
func (d Dialog) MoveNext() Dialog {
	if !d.open || len(d.Options) < 2 {
		return d
	}

	d.selected = (d.selected + 1) % len(d.Options)

	return d
}

// MovePrev highlights the previous option, wrapping around. A no-op while
// closed or when there are fewer than two options to move between.
func (d Dialog) MovePrev() Dialog {
	if !d.open || len(d.Options) < 2 {
		return d
	}

	d.selected = (d.selected - 1 + len(d.Options)) % len(d.Options)

	return d
}

func (d Dialog) log() *slog.Logger {
	if d.Logger != nil {
		return d.Logger
	}

	return slog.Default()
}

// View renders the dialog as a bordered box (AGENT.md §7: "no box borders
// except around the focused pane and modals") via th.Border, which already
// carries the palette's single accent colour and degrades to the ASCII
// border set under a non-Unicode capability — nothing here draws a raw
// escape sequence or a hardcoded border glyph. It is pure: no I/O, no
// mutation, safe to call on every render (AGENT.md §6.8).
//
// width bounds the box; a non-positive width falls back to an unbounded
// render so a caller that hasn't received a tea.WindowSizeMsg yet still
// gets sensible content instead of a clipped or empty string.
func (d Dialog) View(th theme.Theme, width int) string {
	var b strings.Builder

	if d.Title != "" {
		b.WriteString(th.Accent.Render(d.Title))
		b.WriteString("\n\n")
	}

	if d.Message != "" {
		b.WriteString(th.Foreground.Render(d.Message))
		b.WriteString("\n\n")
	}

	for i, opt := range d.Options {
		line := opt.Label
		if i == d.Default {
			line += " (default)"
		}

		if i == d.selected {
			b.WriteString(th.Accent.Render("> " + line))
		} else {
			b.WriteString(th.Muted.Render("  " + line))
		}

		if i < len(d.Options)-1 {
			b.WriteString("\n")
		}
	}

	box := th.Border
	if width > 0 {
		// -2 for the border's own left/right edges, -2 for its 1-column
		// padding on each side (Padding(0, 1) in theme.New) — the same
		// budget any bordered box needs so it fits inside width rather
		// than overflowing it by the frame's own size.
		content := width - 4
		if content < 1 {
			content = 1
		}

		box = box.Width(content)
	}

	return box.Render(b.String())
}
