package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/engine/fake"
)

// sectionsModel is a Downloads screen at 80x24 over `active` queued torrents
// (a00, a01, ...) and then `done` completed ones (c00, c01, ...).
func sectionsModel(t *testing.T, active, done int) (Model, *fake.Engine) {
	t.Helper()

	eng := newTestEngine(t)
	t.Cleanup(func() { _ = eng.Close() })

	for i := range active {
		addFakeTorrent(t, eng, fmt.Sprintf("magnet:?xt=urn:btih:%040d&dn=a%02d", i, i), nil)
	}

	for i := range done {
		addFakeTorrent(t, eng, fmt.Sprintf("magnet:?xt=urn:btih:%040d&dn=c%02d", 100+i, i), fake.Completed())
	}

	m := New(eng, testTheme())
	m.screen = ScreenDownloads

	m = mustUpdate(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mustUpdate(m, engineUpdateMsg{statuses: eng.List()})

	return m, eng
}

// firstBodyLine is the trimmed first line of the Downloads body.
func firstBodyLine(m Model) string { return strings.TrimSpace(bodyLines(m)[0]) }

// assertBlockAligned fails the test unless the first body line is a section title
// or a torrent name: a block is never cut at the top.
func assertBlockAligned(t *testing.T, m Model, when string) {
	t.Helper()

	first := firstBodyLine(m)
	isTitle := strings.HasPrefix(first, "Active (") || strings.HasPrefix(first, "Completed (")
	isName := len(first) == 3 && (first[0] == 'a' || first[0] == 'c')

	if !isTitle && !isName {
		t.Fatalf("%s: the first body line is %q, want a section title or torrent name\n%s", when, first, m.View())
	}

	if l := m.downloadsLayout(); !slices.Contains(l.starts, m.downloads.scroll) {
		t.Fatalf("%s: scroll = %d is not a block start %v", when, m.downloads.scroll, l.starts)
	}
}

// TestDownloadsReturnAfterTheListChangedIsAligned: the scroll offset is
// maintained only while Downloads is the screen, so a list that shrinks
// while the user is elsewhere is resynced on the way back (T-9117).
func TestDownloadsReturnAfterTheListChangedIsAligned(t *testing.T) {
	m, eng := sectionsModel(t, 12, 0)

	m = pressN(m, "j", 11)
	if m.downloads.scroll == 0 {
		t.Fatal("setup: the overflowing list did not scroll")
	}

	m = mustUpdate(m, tea.KeyMsg{Type: tea.KeyTab})
	if m.screen == ScreenDownloads {
		t.Fatal("setup: tab left the screen on Downloads")
	}

	// Six rows go while away: the old offset is past the end of what is left.
	m = mustUpdate(m, engineUpdateMsg{statuses: eng.List()[6:]})

	for m.screen != ScreenDownloads {
		m = mustUpdate(m, tea.KeyMsg{Type: tea.KeyShiftTab})
	}

	if firstBodyLine(m) == "" {
		t.Fatalf("blank first body line after returning to a shorter list:\n%s", m.View())
	}

	assertBlockAligned(t, m, "after returning")
	selectedLine(t, m) // fails when the selected row is off screen
}

// TestDownloadsCompletedOnlyListScrolls: with no Active section the first
// block is the Completed title, and scrolling down and back up keeps every
// window block-aligned and ends on that title (T-9117).
func TestDownloadsCompletedOnlyListScrolls(t *testing.T) {
	m, _ := sectionsModel(t, 0, 12)

	if got := firstBodyLine(m); got != "Completed (12)" {
		t.Fatalf("first body line = %q, want the Completed title", got)
	}

	for i := range 11 {
		m = pressN(m, "j", 1)
		assertBlockAligned(t, m, fmt.Sprintf("after %d moves down", i+1))
		selectedLine(t, m)
	}

	if m.downloads.scroll == 0 {
		t.Fatal("scroll = 0 at the last row of an overflowing list")
	}

	for i := range 11 {
		m = pressN(m, "k", 1)
		assertBlockAligned(t, m, fmt.Sprintf("after %d moves up", i+1))
		selectedLine(t, m)
	}

	if m.downloads.scroll != 0 || firstBodyLine(m) != "Completed (12)" {
		t.Fatalf("back on the first row: scroll = %d, first line %q; want 0 and the title", m.downloads.scroll, firstBodyLine(m))
	}
}

// TestDownloadsSectionTitleReturnsWithItsFirstRow: moving up onto the first
// row of either section brings that section's title back into the window with
// it, whichever way the window had to move (T-9117).
func TestDownloadsSectionTitleReturnsWithItsFirstRow(t *testing.T) {
	const active, done = 6, 6

	m, _ := sectionsModel(t, active, done)

	m = pressN(m, "j", active+done-1)

	for cursor := active + done - 1; cursor >= 0; cursor-- {
		if m.downloads.cursor != cursor {
			t.Fatalf("setup: cursor = %d, want %d", m.downloads.cursor, cursor)
		}

		selectedLine(t, m)

		body := strings.Join(bodyLines(m), "\n")

		switch cursor {
		case active:
			if !strings.Contains(body, "Completed (6)") {
				t.Fatalf("on the first Completed row the window lacks its title:\n%s", m.View())
			}
		case 0:
			if !strings.Contains(body, "Active (6)") {
				t.Fatalf("on the first Active row the window lacks its title:\n%s", m.View())
			}
		}

		m = pressN(m, "k", 1)
	}
}
