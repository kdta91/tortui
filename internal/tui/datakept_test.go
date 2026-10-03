package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// TestDataKeptRemoveSaysSoWithinEightyColumns is PR #100 review note b
// (T-9133): a remove with data that kept the data, because another download
// uses it, says so and where on an 80-column status bar, never that the data
// was deleted. The torrent was removed all the same, so the session is saved.
func TestDataKeptRemoveSaysSoWithinEightyColumns(t *testing.T) {
	kept := filepath.Join(string(filepath.Separator)+"data", "movies", "shared-name")
	err := fmt.Errorf("remove: %w", &engine.DataKeptError{Path: kept})

	msg := removeResultMsg{id: "t1", name: "shared-name", deleteData: true, err: err}

	m := New(newTestEngine(t), testTheme())
	m.width = 80

	updated, _ := m.handleRemoveResult(msg)

	bar := updated.(Model).renderStatusBar()

	if w := theme.Width(bar); w > 80 {
		t.Errorf("status bar is %d columns wide, want at most 80: %q", w, bar)
	}

	if want := "data kept: another download uses " + kept; !strings.Contains(bar, want) {
		t.Errorf("status bar %q does not show %q", bar, want)
	}

	for _, wrong := range []string{"deleted its data", "couldn't remove"} {
		if strings.Contains(bar, wrong) {
			t.Errorf("status bar %q says %q", bar, wrong)
		}
	}

	saver := &countingSaver{}

	_, cmd := busyStatusBar(New(newTestEngine(t), testTheme(), WithSessionSaver(saver))).handleRemoveResult(msg)
	if cmd == nil {
		t.Fatal("handleRemoveResult returned no command; want the session save")
	}

	_ = cmd()

	if saver.count() != 1 {
		t.Fatalf("Save calls = %d, want 1: the torrent was removed", saver.count())
	}
}

// TestSessionSavedAfterPauseOrResume is T-952: a pause or resume that took
// is saved, so a restart after a crash brings the torrent back as the user
// left it. One that failed saves nothing.
func TestSessionSavedAfterPauseOrResume(t *testing.T) {
	for _, resume := range []bool{false, true} {
		saver := &countingSaver{}
		m := busyStatusBar(New(newTestEngine(t), testTheme(), WithSessionSaver(saver)))

		_, cmd := m.handlePauseResumeResult(pauseResumeResultMsg{id: "t1", name: "t1.iso", resume: resume})
		if cmd == nil {
			t.Fatalf("resume %v: no command; want the session save", resume)
		}

		_ = cmd()

		if saver.count() != 1 {
			t.Fatalf("resume %v: Save calls = %d, want 1", resume, saver.count())
		}

		failed := &countingSaver{}
		m = busyStatusBar(New(newTestEngine(t), testTheme(), WithSessionSaver(failed)))

		if _, cmd := m.handlePauseResumeResult(pauseResumeResultMsg{
			id: "t1", name: "t1.iso", resume: resume, err: errors.New("refused"),
		}); cmd != nil {
			_ = cmd()
		}

		if failed.count() != 0 {
			t.Fatalf("resume %v: Save calls = %d after a failed call, want 0", resume, failed.count())
		}
	}
}

// TestMaybeDataKeptRemoveSaysMayUse is T-9135: when no download is known to
// use the kept data but one may, the status bar says "may use", within 80
// columns, and never that the data was deleted.
func TestMaybeDataKeptRemoveSaysMayUse(t *testing.T) {
	kept := filepath.Join(string(filepath.Separator)+"data", "movies", "shared-name")
	err := fmt.Errorf("remove: %w", &engine.DataKeptError{Path: kept, Maybe: true})

	m := New(newTestEngine(t), testTheme())
	m.width = 80

	updated, _ := m.handleRemoveResult(removeResultMsg{id: "t1", name: "shared-name", deleteData: true, err: err})

	bar := updated.(Model).renderStatusBar()

	if w := theme.Width(bar); w > 80 {
		t.Errorf("status bar is %d columns wide, want at most 80: %q", w, bar)
	}

	if want := "data kept: another download may use " + kept; !strings.Contains(bar, want) {
		t.Errorf("status bar %q does not show %q", bar, want)
	}

	if strings.Contains(bar, "deleted its data") {
		t.Errorf("status bar %q says the data was deleted", bar)
	}
}
