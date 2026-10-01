package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
)

// Layout tests for the 80x24 floor (AGENT.md §7): every screen and overlay
// is rendered the way --demo shows it, at the sizes the README promises, and
// checked for the three things that broke — height, the tab bar and status
// bar staying on screen, and no key hint lost to truncation.

type termSize struct{ w, h int }

var layoutSizes = []termSize{{80, 24}, {100, 30}, {120, 40}}

// demoLikeStatuses mirrors --demo's five torrents: a normal download, a
// stalled one, a queued one, an errored one and a finished one.
func demoLikeStatuses() []engine.TorrentStatus {
	return []engine.TorrentStatus{
		{ID: "t1", Name: "alpha-archive-1.iso", State: engine.StateDownloading, Progress: 0.62, DownloadedBytes: 3 << 30, TotalBytes: 5 << 30, DownRate: 12 << 20, UpRate: 880 << 10, Peers: 18, ETA: 2 * time.Minute, SavePath: "/dl/alpha"},
		{ID: "t2", Name: "bravo-dataset.tar", State: engine.StateDownloading, Progress: 0.1, TotalBytes: 2 << 30, Peers: 0, ETA: -1, SavePath: "/dl/bravo"},
		{ID: "t3", Name: "charlie-docs.zip", State: engine.StateQueued, TotalBytes: 1 << 30, ETA: -1, SavePath: "/dl/charlie"},
		{ID: "t4", Name: "delta-broken.iso", State: engine.StateErrored, Err: fmt.Errorf("metadata timeout: no peers responded within 60s"), ETA: -1, SavePath: "/dl/delta"},
		{ID: "t5", Name: "echo-done.iso", State: engine.StateSeeding, Progress: 1, DownloadedBytes: 1 << 30, TotalBytes: 1 << 30, UpRate: 100 << 10, ETA: 0, SavePath: "/dl/echo"},
	}
}

func layoutModel(t *testing.T, sz termSize, banner bool, opts ...Option) Model {
	t.Helper()

	m := New(newTestEngine(t), testTheme(), opts...)
	if banner {
		m.Banner = "DEMO MODE — synthetic data, zero network"
	}

	next, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})

	return next.(Model)
}

func viewLines(m Model) []string { return strings.Split(m.View(), "\n") }

func layoutPress(t *testing.T, m Model, keys ...tea.KeyMsg) Model {
	t.Helper()

	for _, k := range keys {
		next, _ := m.Update(k)
		m = next.(Model)
	}

	return m
}

var keySpace = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}

// assertChrome checks the View fits the terminal, keeps the tab bar first
// (after the demo banner, when set) and the status bar last.
func assertChrome(t *testing.T, m Model, sz termSize, banner bool) []string {
	t.Helper()

	lines := viewLines(m)
	if len(lines) > sz.h {
		t.Fatalf("%dx%d banner=%v: %d lines, want ≤ %d:\n%s", sz.w, sz.h, banner, len(lines), sz.h, m.View())
	}

	tabAt := 0
	if banner {
		tabAt = 2
	}

	if !strings.Contains(lines[tabAt], "Search [1]") || !strings.Contains(lines[tabAt], "Downloads [4]") {
		t.Fatalf("%dx%d banner=%v: line %d is not the tab bar:\n%s", sz.w, sz.h, banner, tabAt, m.View())
	}

	if last := lines[len(lines)-1]; !strings.Contains(last, "active") {
		t.Fatalf("%dx%d banner=%v: last line is not the status bar: %q", sz.w, sz.h, banner, last)
	}

	return lines
}

func TestDownloadsKeepChromeAndFitHeight(t *testing.T) {
	t.Parallel()

	for _, sz := range layoutSizes {
		for _, banner := range []bool{false, true} {
			m := layoutModel(t, sz, banner)
			m.screen = ScreenDownloads
			next, _ := m.Update(engineUpdateMsg{statuses: demoLikeStatuses()})
			m = next.(Model)

			lines := assertChrome(t, m, sz, banner)

			if !strings.Contains(strings.Join(lines, "\n"), "Active (4)") {
				t.Errorf("%dx%d banner=%v: Active header missing:\n%s", sz.w, sz.h, banner, m.View())
			}
		}
	}
}

func TestDownloadsSelectedRowStaysVisibleBelowTheFold(t *testing.T) {
	t.Parallel()

	names := []string{"alpha-archive-1.iso", "bravo-dataset.tar", "charlie-docs.zip", "delta-broken.iso", "echo-done.iso"}

	for _, sz := range layoutSizes {
		for _, banner := range []bool{false, true} {
			m := layoutModel(t, sz, banner)
			m.screen = ScreenDownloads
			next, _ := m.Update(engineUpdateMsg{statuses: demoLikeStatuses()})
			m = next.(Model)

			for i, name := range names {
				if i > 0 {
					m = layoutPress(t, m, tea.KeyMsg{Type: tea.KeyDown})
				}

				lines := assertChrome(t, m, sz, banner)

				if !strings.Contains(strings.Join(lines, "\n"), name) {
					t.Errorf("%dx%d banner=%v: selected row %d (%s) not visible:\n%s", sz.w, sz.h, banner, i, name, m.View())
				}
			}

			// And back up: the first row and its section header return.
			for range names[1:] {
				m = layoutPress(t, m, tea.KeyMsg{Type: tea.KeyUp})
			}

			lines := assertChrome(t, m, sz, banner)
			if !strings.Contains(strings.Join(lines, "\n"), "Active (4)") || !strings.Contains(strings.Join(lines, "\n"), names[0]) {
				t.Errorf("%dx%d banner=%v: scrolling back up lost the first row:\n%s", sz.w, sz.h, banner, m.View())
			}
		}
	}
}

func TestHelpOverlayFitsAndKeepsHeading(t *testing.T) {
	t.Parallel()

	for _, sz := range layoutSizes {
		for _, banner := range []bool{false, true} {
			for _, sc := range screenOrder {
				m := layoutModel(t, sz, banner)
				m.screen = sc
				m = layoutPress(t, m, keyRune("?"))

				lines := viewLines(m)
				if len(lines) > sz.h {
					t.Errorf("%s %dx%d banner=%v: help is %d lines, want ≤ %d:\n%s", sc, sz.w, sz.h, banner, len(lines), sz.h, m.View())
				}

				keysAt := 0
				if banner {
					keysAt = 2
				}

				if lines[keysAt] != "Keys" {
					t.Errorf("%s %dx%d banner=%v: line %d = %q, want the Keys heading:\n%s", sc, sz.w, sz.h, banner, keysAt, lines[keysAt], m.View())
				}

				// Compact or tabular, every binding survives.
				flat := strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
				for _, want := range []string{"jump to settings", "toggle this help overlay", "quit (prompts if downloads active)", "close help"} {
					if !strings.Contains(flat, want) {
						t.Errorf("%s %dx%d banner=%v: help lost %q:\n%s", sc, sz.w, sz.h, banner, want, m.View())
					}
				}
			}
		}
	}
}

func TestHelpHasNoInternalIDs(t *testing.T) {
	t.Parallel()

	for _, sc := range screenOrder {
		m := layoutModel(t, termSize{120, 40}, false)
		m.screen = sc
		m = layoutPress(t, m, keyRune("?"))

		out := m.View()
		for _, bad := range []string{"T-0", "T-9", "DEC-"} {
			if strings.Contains(out, bad) {
				t.Errorf("%s help shows an internal id %q:\n%s", sc, bad, out)
			}
		}
	}
}

func settingsLayoutModel(t *testing.T, sz termSize, extra ...Option) Model {
	t.Helper()

	sm := &fakeSourceManager{sources: []config.Indexer{{ID: "s1", Name: "Sample", Type: "torznab", Enabled: true, URL: "https://feed.example.org/api"}}}
	m := layoutModel(t, sz, false, append([]Option{WithSourceManager(sm)}, extra...)...)
	m.screen = ScreenSettings

	return m
}

// unwrapped joins the screen's lines so a hint that wrapped still matches.
func unwrapped(s string) string { return strings.Join(strings.Fields(s), " ") }

func TestSettingsLegendKeepsEveryHint(t *testing.T) {
	t.Parallel()

	for _, sz := range []termSize{{80, 24}, {100, 30}} {
		m := settingsLayoutModel(t, sz)
		out := m.View()

		for _, line := range strings.Split(out, "\n") {
			if w := len([]rune(line)); w > sz.w {
				t.Errorf("%dx%d: line wider than the terminal (%d): %q", sz.w, sz.h, w, line)
			}
		}

		flat := unwrapped(out)
		for _, hint := range strings.Split(settingsScreenLegend, " · ") {
			if !strings.Contains(flat, hint) {
				t.Errorf("%dx%d: settings legend lost %q:\n%s", sz.w, sz.h, hint, out)
			}
		}

		if !strings.Contains(flat, "p preferences") || !strings.Contains(flat, "r reload definitions") {
			t.Errorf("%dx%d: p / r hints missing:\n%s", sz.w, sz.h, out)
		}

		if len(viewLines(m)) > sz.h {
			t.Errorf("%dx%d: settings overflows:\n%s", sz.w, sz.h, out)
		}
	}
}

func TestFormAndPreferencesFootersKeepEveryHint(t *testing.T) {
	t.Parallel()

	for _, sz := range []termSize{{80, 24}, {100, 30}} {
		m := settingsLayoutModel(t, sz)
		m = layoutPress(t, m, keyRune("a"))
		out := m.View()
		flat := unwrapped(out)

		for _, want := range []string{"ctrl+t test", "ctrl+r reveal", "esc cancel", "ctrl+s/enter save"} {
			if !strings.Contains(flat, want) {
				t.Errorf("%dx%d: add form footer lost %q:\n%s", sz.w, sz.h, want, out)
			}
		}

		if n := len(viewLines(m)); n > sz.h {
			t.Errorf("%dx%d: add form is %d lines, want ≤ %d:\n%s", sz.w, sz.h, n, sz.h, out)
		}

		m = settingsLayoutModel(t, sz, WithPreferencesManager(&fakePreferencesManager{cfg: newTestConfig(t.TempDir())}))
		m = layoutPress(t, m, keyRune("p"))
		out = m.View()
		flat = unwrapped(out)

		for _, want := range []string{"ctrl+s save", "esc back", "ctrl+x remove destination"} {
			if !strings.Contains(flat, want) {
				t.Errorf("%dx%d: preferences footer lost %q:\n%s", sz.w, sz.h, want, out)
			}
		}

		if n := len(viewLines(m)); n > sz.h {
			t.Errorf("%dx%d: preferences is %d lines, want ≤ %d:\n%s", sz.w, sz.h, n, sz.h, out)
		}
	}
}

// --- a typed space is kept in every text input ----------------------------

func layoutType(t *testing.T, m Model, s string) Model {
	t.Helper()

	for _, r := range s {
		if r == ' ' {
			m = layoutPress(t, m, keySpace)
		} else {
			m = layoutPress(t, m, keyRune(string(r)))
		}
	}

	return m
}

func TestSourceFormKeepsTypedSpaces(t *testing.T) {
	t.Parallel()

	m := settingsLayoutModel(t, termSize{80, 24})
	m = layoutPress(t, m, keyRune("a"))
	m = layoutType(t, m, "Example Torznab")

	if got := m.settings.form.name; got != "Example Torznab" {
		t.Fatalf("name = %q, want %q", got, "Example Torznab")
	}

	// The import field (scraper definitions) takes a path with a space too.
	m.settings.form.typ = "scraper"
	for m.settings.form.current() != fieldImport {
		m = layoutPress(t, m, tea.KeyMsg{Type: tea.KeyTab})
	}

	m = layoutType(t, m, "my defs/one.yaml")
	if got := m.settings.form.importText; got != "my defs/one.yaml" {
		t.Fatalf("importText = %q, want a kept space", got)
	}
}

func TestAggregatorWizardKeepsTypedSpaces(t *testing.T) {
	t.Parallel()

	m := settingsLayoutModel(t, termSize{80, 24})
	ag := newAggregatorForm()
	m.settings.aggImport = &ag

	m = layoutType(t, m, "a b")
	if got := m.settings.aggImport.baseURL; got != "a b" {
		t.Fatalf("baseURL = %q, want %q", got, "a b")
	}

	m = layoutPress(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = layoutType(t, m, "k e y")

	if got := m.settings.aggImport.apiKey; got != "k e y" {
		t.Fatalf("apiKey = %q, want %q", got, "k e y")
	}
}

func TestSearchQueryKeepsTypedSpaces(t *testing.T) {
	t.Parallel()

	m := layoutModel(t, termSize{80, 24}, false)
	m = layoutPress(t, m, keyRune("/"))
	m = layoutType(t, m, "two words")

	if got := m.search.query; got != "two words" {
		t.Fatalf("query = %q, want %q", got, "two words")
	}
}

func TestNoBindingHelpNamesATrackerID(t *testing.T) {
	t.Parallel()

	for _, b := range NewKeyMap().bindings {
		for _, bad := range []string{"T-0", "T-9", "DEC-"} {
			if strings.Contains(b.Help, bad) {
				t.Errorf("binding %v Help %q names an internal id", b.Keys, b.Help)
			}
		}
	}
}

// A row taller than the room left keeps its top (the name) on screen.
func TestDownloadsRowTallerThanBodyKeepsItsName(t *testing.T) {
	t.Parallel()

	m := layoutModel(t, termSize{80, 8}, false)
	m.screen = ScreenDownloads
	next, _ := m.Update(engineUpdateMsg{statuses: demoLikeStatuses()})
	m = next.(Model)

	// delta-broken.iso is the fourth active row and has the longest block.
	m = layoutPress(t, m, tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyDown})

	if out := m.View(); !strings.Contains(out, "delta-broken.iso") {
		t.Fatalf("tall selected row lost its name:\n%s", out)
	}
}
