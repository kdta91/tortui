package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/engine/fake"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// TestNoFirstRunByDefault confirms production wiring — no WithFirstRun
// option — starts straight on ScreenSearch, exactly as every pre-T-090
// caller expects.
func TestNoFirstRunByDefault(t *testing.T) {
	m := New(fake.New(), theme.New("", theme.Capability{}))

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
	m := New(fake.New(), theme.New("", theme.Capability{}), WithFirstRun(true))

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
	m := New(fake.New(), theme.New("", theme.Capability{}), WithFirstRun(true))

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

// TestFirstRunTakesPriorityOverOtherModals confirms the overlay is what
// renders even if some other modal flag happens to be set, since it can
// only ever be true on the very first frame before anything else has had a
// chance to open.
func TestFirstRunTakesPriorityOverOtherModals(t *testing.T) {
	m := New(fake.New(), theme.New("", theme.Capability{}), WithFirstRun(true))

	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model := mm.(Model)
	model.showHelp = true

	if model.context() != ContextFirstRun {
		t.Fatalf("context() = %v, want ContextFirstRun even with showHelp set", model.context())
	}
}
