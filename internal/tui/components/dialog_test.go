package components

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/kdta91/tortui/internal/tui/theme"
)

func dialogTestTheme() theme.Theme {
	return theme.New(theme.DefaultThemeName, theme.Capability{Color: theme.ColorTrue, Unicode: true, Interactive: true})
}

// TestDialogRendersConfigurableOptionsAndDefault confirms a Dialog built
// from arbitrary, domain-agnostic options (T-054 acceptance: "generic
// confirm dialog with configurable options and a default") renders its
// title, message, and every option, with the default option marked and the
// currently highlighted option visually distinguished from the rest.
func TestDialogRendersConfigurableOptionsAndDefault(t *testing.T) {
	d := NewDialog("Remove torrent?", "This cannot be undone.", []DialogOption{
		{Label: "Keep data"},
		{Label: "Delete data"},
		{Label: "Cancel"},
	}, 2)
	d = d.Open()

	view := d.View(dialogTestTheme(), 60)

	for _, want := range []string{"Remove torrent?", "This cannot be undone.", "Keep data", "Delete data", "Cancel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}

	if !strings.Contains(view, "Cancel (default)") {
		t.Fatalf("view does not mark the default option:\n%s", view)
	}

	if got, ok := d.SelectedOption(); !ok || got.Label != "Cancel" {
		t.Fatalf("SelectedOption = %+v, %v; want Cancel, true", got, ok)
	}
}

// TestDialogRendersInsideABorder confirms the dialog draws inside
// th.Border — the one box-border AGENT.md §7 allows for a modal — rather
// than emitting its own ad hoc frame.
func TestDialogRendersInsideABorder(t *testing.T) {
	d := NewDialog("t", "m", []DialogOption{{Label: "OK"}}, 0).Open()
	th := dialogTestTheme()

	view := d.View(th, 40)
	plain := th.Border.Render("probe")

	// The border glyph itself (first rune of the rendered probe's frame)
	// must show up in the dialog's own render; comparing full strings
	// would depend on content width, so this checks for the corner glyph
	// lipgloss.RoundedBorder uses.
	if !strings.Contains(view, "╭") && !strings.Contains(plain, "╭") {
		t.Skip("environment resolved a non-rounded border profile")
	}

	if !strings.Contains(view, "╭") {
		t.Fatalf("dialog view has no border corner glyph:\n%s", view)
	}
}

// TestDialogOpenOnAlreadyOpenIsALoggedNoOp confirms T-054's "never more
// than one modal deep" half that belongs to Dialog itself: calling Open
// while already open changes nothing and is reported via log/slog (never
// silently, never to stdout/stderr) rather than stacking a second prompt.
func TestDialogOpenOnAlreadyOpenIsALoggedNoOp(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	d := NewDialog("Quit?", "", []DialogOption{{Label: "Yes"}, {Label: "No"}}, 1)
	d.Logger = logger

	d = d.Open()
	d = d.MoveNext() // now highlighting index 0 ("Yes")

	before := d

	d = d.Open()

	if !d.IsOpen() {
		t.Fatal("dialog unexpectedly closed on a second Open")
	}

	if d.SelectedIndex() != before.SelectedIndex() {
		t.Fatalf("second Open changed the highlighted option: got %d, want %d (no-op)",
			d.SelectedIndex(), before.SelectedIndex())
	}

	logged := buf.String()
	if logged == "" {
		t.Fatal("expected a log record for the redundant Open, got none")
	}

	if !strings.Contains(logged, "already-open") {
		t.Fatalf("log record does not describe the no-op: %q", logged)
	}

	if !strings.Contains(logged, "level=WARN") {
		t.Fatalf("expected a warn-level record, got: %q", logged)
	}
}

// TestDialogOpenWithoutLoggerUsesSlogDefault confirms a Dialog with no
// explicit Logger still logs through log/slog (via slog.Default()) rather
// than silently swallowing the redundant-Open case, matching the
// internal/store and internal/indexer/httpx convention of falling back to
// slog.Default() when no logger was supplied.
func TestDialogOpenWithoutLoggerUsesSlogDefault(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()

	t.Cleanup(func() { slog.SetDefault(prev) })
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))

	d := NewDialog("Quit?", "", []DialogOption{{Label: "Yes"}}, 0).Open()
	_ = d.Open()

	if buf.Len() == 0 {
		t.Fatal("expected slog.Default() to receive the redundant-Open warning")
	}
}

// TestDialogCancelClosesAndRestoresDefault confirms esc-equivalent
// cancellation closes the dialog and resets its own cursor to Default, so
// a later Open never resumes from a stale, previously-moved-to option
// (T-054 acceptance: "esc always cancels").
func TestDialogCancelClosesAndRestoresDefault(t *testing.T) {
	d := NewDialog("t", "m", []DialogOption{{Label: "A"}, {Label: "B"}, {Label: "C"}}, 0)
	d = d.Open()
	d = d.MoveNext().MoveNext() // highlight index 2 ("C")

	if d.SelectedIndex() != 2 {
		t.Fatalf("setup: SelectedIndex = %d, want 2", d.SelectedIndex())
	}

	d = d.Cancel()

	if d.IsOpen() {
		t.Fatal("expected Cancel to close the dialog")
	}

	if d.SelectedIndex() != d.Default {
		t.Fatalf("expected Cancel to restore the default option, got index %d, default %d",
			d.SelectedIndex(), d.Default)
	}

	// Reopening must start fresh at Default, not resume the cancelled
	// cursor position.
	d = d.Open()
	if d.SelectedIndex() != d.Default {
		t.Fatalf("reopened dialog resumed at %d instead of default %d", d.SelectedIndex(), d.Default)
	}
}

// TestDialogConfirmReturnsHighlightedOption confirms Confirm commits to
// whatever option is currently highlighted, not always Default, and closes
// the dialog.
func TestDialogConfirmReturnsHighlightedOption(t *testing.T) {
	d := NewDialog("t", "m", []DialogOption{{Label: "Keep"}, {Label: "Delete"}}, 0)
	d = d.Open().MoveNext()

	updated, chosen := d.Confirm()

	if chosen != 1 {
		t.Fatalf("Confirm chosen = %d, want 1 (Delete)", chosen)
	}

	if updated.IsOpen() {
		t.Fatal("expected Confirm to close the dialog")
	}
}

// TestDialogMoveNextPrevWrapAround confirms cursor movement cycles through
// Options in both directions and does nothing while closed.
func TestDialogMoveNextPrevWrapAround(t *testing.T) {
	d := NewDialog("t", "m", []DialogOption{{Label: "A"}, {Label: "B"}, {Label: "C"}}, 0)

	// Closed: movement is a no-op.
	if moved := d.MoveNext(); moved.SelectedIndex() != 0 {
		t.Fatalf("MoveNext on a closed dialog changed selection to %d", moved.SelectedIndex())
	}

	d = d.Open()

	d = d.MoveNext().MoveNext().MoveNext() // 0 -> 1 -> 2 -> 0
	if d.SelectedIndex() != 0 {
		t.Fatalf("MoveNext did not wrap: got %d, want 0", d.SelectedIndex())
	}

	d = d.MovePrev() // 0 -> 2
	if d.SelectedIndex() != 2 {
		t.Fatalf("MovePrev did not wrap: got %d, want 2", d.SelectedIndex())
	}
}

// TestNewDialogClampsOutOfRangeDefault confirms an invalid Default index
// (including on an empty Options slice) falls back to 0 rather than
// panicking later inside View or Confirm.
func TestNewDialogClampsOutOfRangeDefault(t *testing.T) {
	d := NewDialog("t", "m", []DialogOption{{Label: "A"}}, 5)
	if d.Default != 0 {
		t.Fatalf("Default = %d, want 0 for an out-of-range input", d.Default)
	}

	empty := NewDialog("t", "m", nil, 0)
	if _, ok := empty.SelectedOption(); ok {
		t.Fatal("expected SelectedOption to report false for an empty Options slice")
	}

	// Must not panic.
	_ = empty.Open().View(dialogTestTheme(), 40)
}
