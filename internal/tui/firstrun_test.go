package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/tui/theme"
)

// TestNoFirstRunByDefault confirms production wiring — no WithFirstRun
// option — starts straight on ScreenSearch, exactly as every pre-T-090
// caller expects.
func TestNoFirstRunByDefault(t *testing.T) {
	m := New(newTestEngine(t), theme.New("", theme.Capability{}))

	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model := mm.(Model)

	if model.firstRun {
		t.Fatal("firstRun is true with no WithFirstRun option")
	}

	if model.context() == ContextFirstRun {
		t.Fatal("context() reports ContextFirstRun with no WithFirstRun option")
	}
}

// TestFirstRunShowsLegalNoticeAndSourcesNotice is T-090's direct proof: the
// overlay carries the same legal-notice text README.md quotes (LegalNotice)
// and explains that only a small bundled set of lawful sources ships,
// pointing the user at the docs for adding their own.
func TestFirstRunShowsLegalNoticeAndSourcesNotice(t *testing.T) {
	m := New(newTestEngine(t), theme.New("", theme.Capability{}), WithFirstRun(true))

	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model := mm.(Model)

	if !model.firstRun {
		t.Fatal("firstRun is false with WithFirstRun(true)")
	}

	if model.context() != ContextFirstRun {
		t.Fatalf("context() = %v, want ContextFirstRun", model.context())
	}

	view := model.View()

	// theme.Wrap breaks the notice text across lines, so check word
	// fragments that survive wrapping rather than the whole sentence.
	for _, want := range []string{"responsible for what you search for and", "README.md", "Settings"} {
		if !strings.Contains(view, want) {
			t.Errorf("View() does not contain %q:\n%s", want, view)
		}
	}
}

// TestFirstRunDismissedByAnyKey confirms the overlay closes on any key
// press — not a specific binding — and control returns to the Model's
// normal starting screen.
func TestFirstRunDismissedByAnyKey(t *testing.T) {
	m := New(newTestEngine(t), theme.New("", theme.Capability{}), WithFirstRun(true))

	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model := mm.(Model)

	mm, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	model = mm.(Model)

	if model.firstRun {
		t.Fatal("firstRun still true after a key press")
	}

	if model.context() == ContextFirstRun {
		t.Fatal("context() still reports ContextFirstRun after a key press")
	}

	if model.screen != ScreenSearch {
		t.Fatalf("screen = %v, want ScreenSearch after dismissing first-run", model.screen)
	}
}

// TestFirstRunCtrlCOnlyDismissesOverlay confirms ctrl+c — normally the
// global quit shortcut (AGENT.md §7: "q / ctrl+c | Quit") — only dismisses
// the first-run overlay rather than quitting the program outright: it never
// returns a tea.Quit command, and a second ctrl+c after dismissal quits
// normally (found in review of PR #52).
func TestFirstRunCtrlCOnlyDismissesOverlay(t *testing.T) {
	m := New(newTestEngine(t), theme.New("", theme.Capability{}), WithFirstRun(true))

	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model := mm.(Model)

	mm, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = mm.(Model)

	if model.firstRun {
		t.Fatal("firstRun still true after ctrl+c")
	}

	if cmd != nil && isQuitCmd(cmd) {
		t.Fatal("ctrl+c on the first-run overlay returned a quit command; it should only dismiss")
	}

	// A second ctrl+c, now that the overlay is gone, quits normally (no
	// active downloads on a fresh fake.Engine, so no confirm prompt).
	_, cmd = model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !isQuitCmd(cmd) {
		t.Fatal("a second ctrl+c after dismissal did not quit")
	}
}

// isQuitCmd reports whether cmd, once run, produces bubbletea's own
// QuitMsg — the only reliable way to recognise tea.Quit's result, since
// tea.Cmd is just a func.
func isQuitCmd(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}

	_, ok := cmd().(tea.QuitMsg)

	return ok
}

// TestFirstRunTakesPriorityOverOtherModals confirms the overlay is what
// renders even if some other modal flag happens to be set, since it can
// only ever be true on the very first frame before anything else has had a
// chance to open.
func TestFirstRunTakesPriorityOverOtherModals(t *testing.T) {
	m := New(newTestEngine(t), theme.New("", theme.Capability{}), WithFirstRun(true))

	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model := mm.(Model)
	model.showHelp = true

	if model.context() != ContextFirstRun {
		t.Fatalf("context() = %v, want ContextFirstRun even with showHelp set", model.context())
	}
}
