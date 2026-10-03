package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/lifecycle"
	"github.com/kdta91/tortui/internal/tui/components"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// TestDroppedRecordNoticeFitsAnEightyColumnStatusBar is T-9127's review fix:
// the notice for a dropped duplicate record whose data sat elsewhere leads
// with where that data is left unmanaged, so an 80-column status bar shows
// it.
func TestDroppedRecordNoticeFitsAnEightyColumnStatusBar(t *testing.T) {
	old := filepath.Join(string(filepath.Separator)+"home", "u", "Downloads", "tortui", "old")
	report := lifecycle.ResumeReport{Dropped: []lifecycle.DroppedRecord{{
		ID: "an-2", Name: "ubuntu-24.04-desktop-amd64.iso", SavePath: old, KeptAs: "an-1", Elsewhere: true,
	}}}

	notices := startupNotices(config.LoadResult{}, "", report)
	if len(notices) != 1 {
		t.Fatalf("notices = %q, want one", notices)
	}

	bar, _ := components.New().Push(notices[0])
	th := theme.New(theme.DefaultThemeName, theme.Capability{Color: theme.ColorNone, Unicode: true, Interactive: true})
	view := bar.View(80, "search", th)

	if w := theme.Width(view); w > 80 {
		t.Errorf("status bar is %d columns wide, want at most 80: %q", w, view)
	}

	for _, want := range []string{"unmanaged data in " + old, "dropped duplicate record"} {
		if !strings.Contains(view, want) {
			t.Errorf("status bar %q does not show %q", view, want)
		}
	}
}
