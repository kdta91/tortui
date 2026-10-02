package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/engine/fake"
)

// scrollModel is a Downloads screen at 80x24 over n queued fake torrents,
// named t00, t01, ... in list order.
func scrollModel(t *testing.T, n int) (Model, *fake.Engine) {
	t.Helper()

	eng := newTestEngine(t)
	t.Cleanup(func() { _ = eng.Close() })

	for i := range n {
		addFakeTorrent(t, eng, fmt.Sprintf("magnet:?xt=urn:btih:%040d&dn=t%02d", i, i), nil)
	}

	m := New(eng, testTheme())
	m.screen = ScreenDownloads

	m = mustUpdate(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mustUpdate(m, engineUpdateMsg{statuses: eng.List()})

	return m, eng
}

func mustUpdate(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)

	return next.(Model)
}

func pressN(m Model, key string, n int) Model {
	for range n {
		m = mustUpdate(m, keyRune(key))
	}

	return m
}

// bodyLines is the View lines under the tab bar and its separator.
func bodyLines(m Model) []string {
	return strings.Split(m.View(), "\n")[2:]
}

// selectedLine is the index in bodyLines of the selected row's name line (the
// colourless test theme draws no marker, so the name is the handle).
func selectedLine(t *testing.T, m Model) int {
	t.Helper()

	name := m.downloadName(m.downloadRows()[m.downloads.cursor])

	for i, l := range bodyLines(m) {
		if strings.TrimSpace(l) == name {
			return i
		}
	}

	t.Fatalf("no selected row in view:\n%s", m.View())

	return -1
}

func TestDownloadsScrollUpFromEndKeepsWindow(t *testing.T) {
	m, _ := scrollModel(t, 12)

	m = pressN(m, "j", 11)
	if m.downloads.scroll == 0 {
		t.Fatalf("setup: expected an overflowing list to scroll, scroll = 0")
	}

	before := bodyLines(m)
	scrollBefore := m.downloads.scroll
	bottom := selectedLine(t, m)

	m = pressN(m, "k", 1)

	if m.downloads.scroll != scrollBefore {
		t.Fatalf("scroll moved on a single step up: %d -> %d", scrollBefore, m.downloads.scroll)
	}

	if got := strings.TrimSpace(bodyLines(m)[0]); got != strings.TrimSpace(before[0]) {
		t.Fatalf("first body line changed on a single step up: %q -> %q", before[0], got)
	}

	if got := selectedLine(t, m); got >= bottom {
		t.Fatalf("selection line = %d after moving up, want above the bottom edge line %d", got, bottom)
	}
}

func TestDownloadsScrollUpPastTopScrollsOneBlock(t *testing.T) {
	m, _ := scrollModel(t, 12)

	m = pressN(m, "j", 11)
	top := m.downloads.scroll

	// Walk up until the selection reaches the top of the window.
	for m.downloads.cursor > 0 && selectedLine(t, m) > 2 {
		m = pressN(m, "k", 1)
	}

	atTop := m.downloads.scroll
	if atTop != top {
		t.Fatalf("scroll changed while the selection was inside the window: %d -> %d", top, atTop)
	}

	l := m.downloadsLayout()
	m = pressN(m, "k", 1)

	if m.downloads.scroll >= atTop {
		t.Fatalf("scroll = %d after moving above the window, want < %d", m.downloads.scroll, atTop)
	}

	if !slices.Contains(l.starts, m.downloads.scroll) {
		t.Fatalf("scroll = %d is not a block start %v", m.downloads.scroll, l.starts)
	}

	// The new row is the first visible block.
	if first := strings.TrimSpace(bodyLines(m)[0]); first != m.downloadName(m.downloadRows()[m.downloads.cursor]) {
		t.Fatalf("first body line = %q, want the newly selected row", first)
	}
}

func TestDownloadsScrollUpToFirstRowBringsSectionTitleBack(t *testing.T) {
	m, _ := scrollModel(t, 12)

	m = pressN(m, "j", 11)
	m = pressN(m, "k", 11)

	if m.downloads.scroll != 0 {
		t.Fatalf("scroll = %d with the first row selected, want 0", m.downloads.scroll)
	}

	if first := strings.TrimSpace(bodyLines(m)[0]); first != "Active (12)" {
		t.Fatalf("first body line = %q, want the section title", first)
	}
}

func TestDownloadsScrollWithinWindowDoesNotScroll(t *testing.T) {
	m, _ := scrollModel(t, 12)

	for range 2 {
		m = pressN(m, "j", 1)
		if m.downloads.scroll != 0 {
			t.Fatalf("scroll = %d moving inside the first window, want 0", m.downloads.scroll)
		}
	}
}

func TestDownloadsScrollShrinkHeightKeepsSelectionVisible(t *testing.T) {
	m, _ := scrollModel(t, 12)

	m = pressN(m, "j", 11)
	m = mustUpdate(m, tea.WindowSizeMsg{Width: 80, Height: 12})

	selectedLine(t, m) // fails the test when the selected row is not on screen

	if got := len(strings.Split(m.View(), "\n")); got > 12 {
		t.Fatalf("view is %d lines at height 12", got)
	}
}

func TestDownloadsScrollRemovalLeavesNoBlankTop(t *testing.T) {
	m, eng := scrollModel(t, 12)

	m = pressN(m, "j", 11)

	// Drop the first six: the offset (42) is now past the end of the shorter
	// list, with the cursor on the last row.
	m = mustUpdate(m, engineUpdateMsg{statuses: eng.List()[6:]})

	body := bodyLines(m)
	if strings.TrimSpace(body[0]) == "" {
		t.Fatalf("blank first body line after the list shrank:\n%s", m.View())
	}

	names := 0

	for _, l := range body {
		if n := strings.TrimSpace(l); len(n) == 3 && n[0] == 't' {
			names++
		}
	}

	// A full window shows several rows, not just the selected last one.
	if names < 3 {
		t.Fatalf("%d torrents visible after the list shrank, want a full window:\n%s", names, m.View())
	}

	selectedLine(t, m)

	// Shorter than one window: back to the top.
	m = mustUpdate(m, engineUpdateMsg{statuses: eng.List()[9:]})
	if m.downloads.scroll != 0 {
		t.Fatalf("scroll = %d for a list that fits, want 0", m.downloads.scroll)
	}
}

// T-9070: after scrolling at 80x24 the first body line is a section title or
// a torrent name, never the middle of a block.
func TestDownloadsScrollSnapsToBlockBoundary(t *testing.T) {
	m, _ := scrollModel(t, 12)

	for i := range 11 {
		m = pressN(m, "j", 1)

		first := strings.TrimSpace(bodyLines(m)[0])
		isTitle := strings.HasPrefix(first, "Active (") || strings.HasPrefix(first, "Completed (")
		isName := len(first) == 3 && first[0] == 't'

		if !isTitle && !isName {
			t.Fatalf("after %d moves the first body line is %q, want a section title or torrent name", i+1, first)
		}
	}
}
