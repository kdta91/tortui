package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/engine/fake"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// TestBannerRendersAboveEveryScreen is T-056's direct proof that setting
// Model.Banner makes it unmistakable --demo data is on screen: the banner
// text shows up on every screen, not just the default one, and survives a
// screen switch.
func TestBannerRendersAboveEveryScreen(t *testing.T) {
	m := New(fake.New(), theme.New("", theme.Capability{}))
	m.Banner = "DEMO MODE — synthetic data, no network"

	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model := mm.(Model)

	for _, screen := range screenOrder {
		model.screen = screen

		if !strings.Contains(model.View(), m.Banner) {
			t.Errorf("View() on screen %v does not contain the banner", screen)
		}
	}
}

// TestNoBannerByDefault confirms production wiring (no Banner set) never
// shows one, so New's zero value stays silent.
func TestNoBannerByDefault(t *testing.T) {
	m := New(fake.New(), theme.New("", theme.Capability{}))

	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model := mm.(Model)

	if strings.Contains(model.View(), "DEMO") {
		t.Error("View() mentions DEMO with no Banner set")
	}
}

// TestBannerRendersOverModals confirms the banner still shows while a modal
// (the help overlay) is open, not just on the plain screen render path.
func TestBannerRendersOverModals(t *testing.T) {
	m := New(fake.New(), theme.New("", theme.Capability{}))
	m.Banner = "DEMO MODE"

	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model := mm.(Model)
	model.showHelp = true

	if !strings.Contains(model.View(), m.Banner) {
		t.Error("View() with the help overlay open does not contain the banner")
	}
}
