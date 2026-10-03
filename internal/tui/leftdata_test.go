package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// TestLeftDataRefusalFitsAnEightyColumnStatusBar is T-9127's review fix: an
// add refused because an earlier, failed add left data elsewhere shows the
// remedy and the path on an 80-column status bar, without the usual
// add-failure prefix pushing them off the edge.
func TestLeftDataRefusalFitsAnEightyColumnStatusBar(t *testing.T) {
	left := filepath.Join(string(filepath.Separator)+"data", "movies", "left-behind")
	err := fmt.Errorf("add: %w", &engine.LeftDataError{Path: left})

	m := New(newTestEngine(t), testTheme())
	m.width = 80

	updated, _ := m.handleAddResult(addResultMsg{name: "left-behind", err: err})

	bar := updated.(Model).renderStatusBar()

	if w := theme.Width(bar); w > 80 {
		t.Errorf("status bar is %d columns wide, want at most 80: %q", w, bar)
	}

	for _, want := range []string{"remove its errored row (x) first", "left data in " + left} {
		if !strings.Contains(bar, want) {
			t.Errorf("status bar %q does not show %q", bar, want)
		}
	}
}
