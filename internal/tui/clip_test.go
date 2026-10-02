package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/tui/theme"
)

func colourTheme() theme.Theme {
	return theme.New(theme.DefaultThemeName, theme.Capability{Color: theme.ColorTrue, Unicode: true, Interactive: true})
}

// TestTruncateLinesCountsVisibleColumns pins T-9058: a styled line is judged
// by what shows, not by its escape bytes.
func TestTruncateLinesCountsVisibleColumns(t *testing.T) {
	t.Parallel()

	th := colourTheme()
	fits := th.Accent.Render("0123456789")

	if !strings.ContainsRune(fits, '\x1b') {
		t.Fatalf("colour forced on but %q has no escape code", fits)
	}

	if got := truncateLines(fits, 10); got != fits {
		t.Errorf("a styled line that fits was cut: %q", got)
	}

	long := th.Accent.Render("0123456789abcdefghij")
	got := truncateLines(long+"\n"+fits, 12)
	lines := strings.Split(got, "\n")

	if w := ansi.StringWidth(lines[0]); w != 12 {
		t.Errorf("cut line is %d columns, want exactly 12: %q", w, lines[0])
	}

	if want := "012345678..."; ansi.Strip(lines[0]) != want {
		t.Errorf("cut line text = %q, want %q", ansi.Strip(lines[0]), want)
	}

	if lines[1] != fits {
		t.Errorf("second line changed: %q", lines[1])
	}
}

// TestEveryScreenFitsNarrowWidths pins T-9068: no rendered line is wider than
// the terminal, on any screen, down to 40 columns.
func TestEveryScreenFitsNarrowWidths(t *testing.T) {
	t.Parallel()

	for _, w := range []int{40, 60, 68, 80} {
		for _, sc := range screenOrder {
			m := settingsLayoutModel(t, termSize{w, 20})
			m.screen = sc

			for _, line := range strings.Split(m.View(), "\n") {
				if got := ansi.StringWidth(line); got > w {
					t.Errorf("%s at width %d: line is %d columns: %q", sc, w, got, line)
				}
			}
		}
	}
}

func TestTabBarKeepsEveryScreenReachableWhenClipped(t *testing.T) {
	t.Parallel()

	m := settingsLayoutModel(t, termSize{60, 20})
	bar := ansi.Strip(m.renderTabs())

	for _, name := range []string{"Search", "Results", "Details", "Downloads", "Settings"} {
		if !strings.Contains(bar, name) {
			t.Errorf("tab bar at 60 columns lost %q: %q", name, bar)
		}
	}
}

func manySourcesModel(t *testing.T, n int, sz termSize) Model {
	t.Helper()

	rows := make([]config.Indexer, 0, n)
	for i := range n {
		rows = append(rows, config.Indexer{ID: fmt.Sprintf("s%02d", i), Name: fmt.Sprintf("Source %02d", i), Type: "torznab", Enabled: true, URL: "https://feed.example.org/api"})
	}

	sm := &fakeSourceManager{sources: rows}
	m := layoutModel(t, sz, false, WithSourceManager(sm))
	m.screen = ScreenSettings

	return m
}

func TestSettingsListScrollsWithManySources(t *testing.T) {
	t.Parallel()

	sz := termSize{80, 24}
	m := manySourcesModel(t, 30, sz)

	for step := 0; step < 30; step++ {
		out := m.View()
		lines := strings.Split(out, "\n")

		if len(lines) > sz.h {
			t.Fatalf("step %d: %d lines, want ≤ %d:\n%s", step, len(lines), sz.h, out)
		}

		flat := unwrapped(out)
		for _, hint := range strings.Split(settingsScreenLegend, " · ") {
			if !strings.Contains(flat, hint) {
				t.Fatalf("step %d: legend lost %q:\n%s", step, hint, out)
			}
		}

		if !strings.Contains(out, fmt.Sprintf("> Source %02d", step)) {
			t.Fatalf("step %d: selected row not visible:\n%s", step, out)
		}

		if !strings.Contains(lines[len(lines)-1], "Settings") {
			t.Fatalf("step %d: status bar not last:\n%s", step, out)
		}

		m = layoutPress(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
}
