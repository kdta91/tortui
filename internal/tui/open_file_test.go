package tui

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/engine/fake"
	"github.com/kdta91/tortui/internal/platform"
)

// filesEngine is a fake engine whose Files reports a scripted list.
type filesEngine struct {
	*fake.Engine

	files    []engine.FileStatus
	filesErr error
}

func (e *filesEngine) Files(string) ([]engine.FileStatus, error) {
	return e.files, e.filesErr
}

// launchCall is one recorded open/reveal request.
type launchCall struct {
	path  string
	roots []string
}

// recordingLauncher returns an openPathFunc that records every call and
// never launches anything.
func recordingLauncher(calls *[]launchCall) openPathFunc {
	return func(path string, roots []string) error {
		*calls = append(*calls, launchCall{path: path, roots: append([]string(nil), roots...)})
		return nil
	}
}

func newOpenFileModel(t *testing.T, eng engine.Engine, statuses []engine.TorrentStatus, opts ...Option) Model {
	t.Helper()

	m := New(eng, testTheme(), opts...)
	m.screen = ScreenDownloads
	m.width, m.height = 100, 40
	m.torrentStatuses = statuses

	return m
}

func statusBarText(m Model) string {
	return m.statusBar.View(300, "downloads", m.theme)
}

func TestOpenFileLaunchesTheLargestFile(t *testing.T) {
	save := filepath.Join(t.TempDir(), "dl")
	eng := &filesEngine{Engine: fake.New(), files: []engine.FileStatus{
		{Path: "set/readme.txt", SizeBytes: 10},
		{Path: "set/disc/big.iso", SizeBytes: 9000},
		{Path: "set/extra.bin", SizeBytes: 500},
	}}

	var opened, revealed []launchCall
	m := newOpenFileModel(t, eng, []engine.TorrentStatus{{
		ID: "t1", Name: "set", State: engine.StateSeeding, Progress: 1, SavePath: save,
	}}, WithOpenFile(recordingLauncher(&opened)), WithRevealFile(recordingLauncher(&revealed)))

	m, cmd := press(t, m, keyRune("o"))
	_, _ = runCmd(t, m, cmd)

	want := filepath.Join(save, "set", "disc", "big.iso")
	if len(opened) != 1 || opened[0].path != want {
		t.Fatalf("open calls = %+v, want one for %q", opened, want)
	}

	if len(revealed) != 0 {
		t.Errorf("o also revealed: %+v", revealed)
	}
}

func TestOpenFolderRevealsTheLargestFile(t *testing.T) {
	save := filepath.Join(t.TempDir(), "dl")
	eng := &filesEngine{Engine: fake.New(), files: []engine.FileStatus{
		{Path: "single.iso", SizeBytes: 42},
	}}

	var opened, revealed []launchCall
	m := newOpenFileModel(t, eng, []engine.TorrentStatus{{
		ID: "t1", Name: "single.iso", State: engine.StateSeeding, Progress: 1, SavePath: save,
	}}, WithOpenFile(recordingLauncher(&opened)), WithRevealFile(recordingLauncher(&revealed)))

	m, cmd := press(t, m, keyRune("f"))
	_, _ = runCmd(t, m, cmd)

	want := filepath.Join(save, "single.iso")
	if len(revealed) != 1 || revealed[0].path != want {
		t.Fatalf("reveal calls = %+v, want one for %q", revealed, want)
	}

	if len(opened) != 0 {
		t.Errorf("f also opened: %+v", opened)
	}
}

// TestOpenPassesEveryKnownRoot: the containment check gets the whole root
// set — default dir, saved destinations, and every torrent's own
// destination — never a single directory (AGENT.md §6.12).
func TestOpenPassesEveryKnownRoot(t *testing.T) {
	base := t.TempDir()
	def := filepath.Join(base, "default")
	saved := filepath.Join(base, "saved")
	dest1 := filepath.Join(base, "one")
	dest2 := filepath.Join(base, "two")

	eng := &filesEngine{Engine: fake.New(), files: []engine.FileStatus{{Path: "a.iso", SizeBytes: 1}}}

	var opened []launchCall
	m := newOpenFileModel(t, eng, []engine.TorrentStatus{
		{ID: "t1", Name: "a.iso", State: engine.StateSeeding, Progress: 1, SavePath: dest1},
		{ID: "t2", Name: "b.iso", State: engine.StatePaused, Progress: 1, SavePath: dest2},
	}, WithOpenFile(recordingLauncher(&opened)), WithDownloadDir(def), WithSavedDestinations([]string{saved}))

	m, cmd := press(t, m, keyRune("o"))
	_, _ = runCmd(t, m, cmd)

	if len(opened) != 1 {
		t.Fatalf("open calls = %+v, want 1", opened)
	}

	if want := []string{def, saved, dest1, dest2}; !reflect.DeepEqual(opened[0].roots, want) {
		t.Errorf("roots = %q, want %q", opened[0].roots, want)
	}
}

func TestOpenOnIncompleteTorrentSaysSoAndDoesNotLaunch(t *testing.T) {
	eng := &filesEngine{Engine: fake.New(), files: []engine.FileStatus{{Path: "a.iso", SizeBytes: 1}}}

	for _, key := range []string{"o", "f"} {
		var calls []launchCall
		m := newOpenFileModel(t, eng, []engine.TorrentStatus{{
			ID: "t1", Name: "a.iso", State: engine.StateDownloading, Progress: 0.62, SavePath: t.TempDir(),
		}}, WithOpenFile(recordingLauncher(&calls)), WithRevealFile(recordingLauncher(&calls)))

		m, _ = press(t, m, keyRune(key)) // cmd is only the notice's timeout tick

		if len(calls) != 0 {
			t.Fatalf("%s on an incomplete torrent launched %+v", key, calls)
		}

		if bar := statusBarText(m); !strings.Contains(bar, "a.iso is still downloading (62%)") {
			t.Errorf("%s: status bar = %q, want the still-downloading message", key, bar)
		}
	}
}

// TestOpenRefusesTraversalInTheDeclaredPath: a file path from torrent
// metadata that tries to climb out of its destination never reaches the
// launcher at all.
func TestOpenRefusesTraversalInTheDeclaredPath(t *testing.T) {
	for _, declared := range []string{"set/../../../etc/passwd", "../escape.iso", "", "set/C:evil", `set/a\..\..\b`} {
		eng := &filesEngine{Engine: fake.New(), files: []engine.FileStatus{{Path: declared, SizeBytes: 1}}}

		var calls []launchCall
		m := newOpenFileModel(t, eng, []engine.TorrentStatus{{
			ID: "t1", Name: "set", State: engine.StateSeeding, Progress: 1, SavePath: t.TempDir(),
		}}, WithOpenFile(recordingLauncher(&calls)))

		m, cmd := press(t, m, keyRune("o"))
		m, _ = runCmd(t, m, cmd)

		if len(calls) != 0 {
			t.Fatalf("declared path %q reached the launcher: %+v", declared, calls)
		}

		if bar := statusBarText(m); !strings.Contains(bar, "couldn't open set") {
			t.Errorf("declared path %q: status bar = %q, want a refusal", declared, bar)
		}
	}
}

func TestOpenReportsLauncherAndEngineFailures(t *testing.T) {
	cases := map[string]struct {
		eng    *filesEngine
		launch openPathFunc
		want   string
	}{
		"launcher refuses": {
			eng:    &filesEngine{Engine: fake.New(), files: []engine.FileStatus{{Path: "a.iso", SizeBytes: 1}}},
			launch: func(string, []string) error { return platform.ErrOutsideRoots },
			want:   "couldn't open a.iso: " + platform.ErrOutsideRoots.Error(),
		},
		"files error": {
			eng:    &filesEngine{Engine: fake.New(), filesErr: errors.New("gone")},
			launch: func(string, []string) error { t.Fatal("launcher called"); return nil },
			want:   "couldn't open a.iso: gone",
		},
		"no files": {
			eng:    &filesEngine{Engine: fake.New()},
			launch: func(string, []string) error { t.Fatal("launcher called"); return nil },
			want:   "couldn't open a.iso: no files reported yet",
		},
	}

	for name, tc := range cases {
		m := newOpenFileModel(t, tc.eng, []engine.TorrentStatus{{
			ID: "t1", Name: "a.iso", State: engine.StateSeeding, Progress: 1, SavePath: t.TempDir(),
		}}, WithOpenFile(tc.launch))

		m, cmd := press(t, m, keyRune("o"))
		m, _ = runCmd(t, m, cmd)

		if bar := statusBarText(m); !strings.Contains(bar, tc.want) {
			t.Errorf("%s: status bar = %q, want %q", name, bar, tc.want)
		}
	}
}

// TestOpenFallsBackToDownloadDirForAnEmptySavePath: an empty SavePath means
// the engine's configured default (engine.AddSource), which is downloadDir.
func TestOpenFallsBackToDownloadDirForAnEmptySavePath(t *testing.T) {
	def := t.TempDir()
	eng := &filesEngine{Engine: fake.New(), files: []engine.FileStatus{{Path: "a.iso", SizeBytes: 1}}}

	var calls []launchCall
	m := newOpenFileModel(t, eng, []engine.TorrentStatus{{
		ID: "t1", Name: "a.iso", State: engine.StateSeeding, Progress: 1,
	}}, WithOpenFile(recordingLauncher(&calls)), WithDownloadDir(def))

	m, cmd := press(t, m, keyRune("o"))
	_, _ = runCmd(t, m, cmd)

	if len(calls) != 1 || calls[0].path != filepath.Join(def, "a.iso") {
		t.Fatalf("open calls = %+v, want %q", calls, filepath.Join(def, "a.iso"))
	}

	// With neither, there is nowhere to look.
	calls = nil
	m = newOpenFileModel(t, eng, []engine.TorrentStatus{{
		ID: "t1", Name: "a.iso", State: engine.StateSeeding, Progress: 1,
	}}, WithOpenFile(recordingLauncher(&calls)))

	m, _ = press(t, m, keyRune("o"))

	if len(calls) != 0 || !strings.Contains(statusBarText(m), "no known location for a.iso") {
		t.Errorf("calls = %+v, status = %q", calls, statusBarText(m))
	}
}

func TestOpenOnEmptyDownloadsScreenIsANoOp(t *testing.T) {
	var calls []launchCall
	m := newOpenFileModel(t, fake.New(), nil, WithOpenFile(recordingLauncher(&calls)), WithRevealFile(recordingLauncher(&calls)))

	for _, key := range []string{"o", "f"} {
		var cmd tea.Cmd
		m, cmd = press(t, m, keyRune(key))
		if cmd != nil {
			t.Errorf("%s on an empty screen returned a command", key)
		}
	}

	if len(calls) != 0 {
		t.Errorf("calls = %+v, want none", calls)
	}
}
