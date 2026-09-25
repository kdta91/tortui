package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/engine/fake"
)

// fakePreferencesManager is an in-memory PreferencesManager double, the
// same "never touches disk or network, every method scriptable" shape
// fakeSourceManager already establishes for SourceManager (AGENT.md §6.7).
type fakePreferencesManager struct {
	mu  sync.Mutex
	cfg config.Config

	saveErr   error
	saveCalls int
	lastSaved config.Config
}

func (f *fakePreferencesManager) Config() config.Config {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.cfg
}

func (f *fakePreferencesManager) SaveConfig(cfg config.Config) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.saveCalls++
	if f.saveErr != nil {
		return f.saveErr
	}

	f.cfg = cfg
	f.lastSaved = cfg

	return nil
}

func (f *fakePreferencesManager) saveCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.saveCalls
}

// newPrefsTestModel builds a Model on ScreenSettings with the preferences
// panel already open, wired to pm.
func newPrefsTestModel(t *testing.T, eng engine.Engine, pm PreferencesManager) Model {
	t.Helper()

	m := New(eng, testTheme(), WithPreferencesManager(pm))
	m.screen = ScreenSettings
	m.width, m.height = 100, 40

	updated, cmd := m.handlePreferencesOpen()
	m = updated.(Model)
	m, _ = runCmd(t, m, cmd)

	return m
}

// typeText and sendPrefsKey drive the preferences panel through
// handlePreferencesKey and immediately settle whatever tea.Cmd it returns —
// checkPrefsDownloadDirCmd's validation and savePrefsCmd's save result each
// produce exactly one message and never a further command, so a single
// runCmd hop (download_actions_test.go's own helper) is always enough here.
func typeText(t *testing.T, m *Model, s string) {
	t.Helper()

	updated, cmd := m.handlePreferencesKey(keyRune(s))
	next, _ := runCmd(t, updated.(Model), cmd)
	*m = next
}

func sendPrefsKey(t *testing.T, m *Model, msg tea.KeyMsg) {
	t.Helper()

	updated, cmd := m.handlePreferencesKey(msg)
	next, _ := runCmd(t, updated.(Model), cmd)
	*m = next
}

func backspace(t *testing.T, m *Model, n int) {
	t.Helper()

	for i := 0; i < n; i++ {
		sendPrefsKey(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	}
}

func tabTo(t *testing.T, m *Model, n int) {
	t.Helper()

	for i := 0; i < n; i++ {
		sendPrefsKey(t, m, tea.KeyMsg{Type: tea.KeyTab})
	}
}

func newTestConfig(downloadDir string) config.Config {
	return config.Default(downloadDir)
}

// --- opening ----------------------------------------------------------

func TestPreferencesOpenRefusesWithoutManager(t *testing.T) {
	m := New(fake.New(), testTheme())
	m.screen = ScreenSettings

	updated, _ := m.handlePreferencesOpen()
	m = updated.(Model)

	if m.settings.prefsForm != nil {
		t.Fatal("prefsForm must stay nil with no PreferencesManager wired")
	}

	if got := m.statusBar.Message(); got == "" {
		t.Fatal("expected a status message explaining no manager is configured")
	}
}

func TestPreferencesOpenPrefillsFromConfig(t *testing.T) {
	dir := t.TempDir()
	pm := &fakePreferencesManager{cfg: newTestConfig(dir)}

	m := newPrefsTestModel(t, fake.New(), pm)

	f := m.settings.prefsForm
	if f == nil {
		t.Fatal("expected the preferences panel to open")
	}

	if f.downloadDir != dir {
		t.Fatalf("downloadDir = %q, want %q", f.downloadDir, dir)
	}
	if f.maxPeers != "50" {
		t.Fatalf("maxPeers = %q, want \"50\"", f.maxPeers)
	}
	if f.theme != "default" {
		t.Fatalf("theme = %q, want \"default\"", f.theme)
	}
}

// --- inline validation: never silently clamped -------------------------

func TestPreferencesInvalidMaxPeersRejectedInline(t *testing.T) {
	dir := t.TempDir()
	pm := &fakePreferencesManager{cfg: newTestConfig(dir)}
	m := newPrefsTestModel(t, fake.New(), pm)

	tabTo(t, &m, 4) // download dir -> max download rate -> max upload rate -> max active -> max peers
	if prefsFieldKind(m.settings.prefsForm.cursor) != prefFieldMaxPeers {
		t.Fatalf("cursor = %d, want the max-peers row", m.settings.prefsForm.cursor)
	}

	backspace(t, &m, 4) // clear "50"
	typeText(t, &m, "0")

	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyCtrlS})

	if pm.saveCallCount() != 0 {
		t.Fatal("an invalid value must never reach SaveConfig")
	}

	issues := m.settings.prefsForm.liveIssues()
	if len(issues) == 0 {
		t.Fatal("expected an inline issue for max_peers = 0")
	}

	found := false
	for _, issue := range issues {
		if issue == "Max peers: must be at least 1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("issues = %v, want one naming max peers", issues)
	}
}

func TestPreferencesInvalidSeedDurationRejectedInline(t *testing.T) {
	dir := t.TempDir()
	pm := &fakePreferencesManager{cfg: newTestConfig(dir)}
	m := newPrefsTestModel(t, fake.New(), pm)

	tabTo(t, &m, 8) // -> seed duration
	if prefsFieldKind(m.settings.prefsForm.cursor) != prefFieldSeedDuration {
		t.Fatalf("cursor = %d, want the seed-duration row", m.settings.prefsForm.cursor)
	}

	backspace(t, &m, 8) // clear "24h"
	typeText(t, &m, "not-a-duration")

	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyCtrlS})

	if pm.saveCallCount() != 0 {
		t.Fatal("an unparseable duration must never reach SaveConfig")
	}
	if len(m.settings.prefsForm.liveIssues()) == 0 {
		t.Fatal("expected an inline issue for an invalid seed_duration")
	}
}

// --- download dir: existence/writability validation ---------------------

func TestPreferencesDownloadDirRejectsFileInThePlaceOfAFolder(t *testing.T) {
	dir := t.TempDir()
	occupied := filepath.Join(dir, "occupied")
	if err := os.WriteFile(occupied, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	pm := &fakePreferencesManager{cfg: newTestConfig(dir)}
	m := newPrefsTestModel(t, fake.New(), pm)

	f := m.settings.prefsForm
	backspace(t, &m, len(f.downloadDir)+4)
	typeText(t, &m, occupied)

	// checkPrefsDownloadDirCmd runs off a tea.Cmd (AGENT.md §6.1); runCmd
	// above already drove it to completion synchronously via typeText, so
	// the check is in by the time liveIssues reads it.
	issues := m.settings.prefsForm.liveIssues()
	if len(issues) == 0 {
		t.Fatal("expected the download-dir row to report a problem")
	}

	found := false
	for _, issue := range issues {
		if issue == "Download directory: a file, not a folder, is already at this path" {
			found = true
		}
	}
	if !found {
		t.Fatalf("issues = %v, want the occupied-by-a-file reason", issues)
	}

	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if pm.saveCallCount() != 0 {
		t.Fatal("must not save with the download dir pointed at a file")
	}
}

// --- saved destinations: add / rename / remove --------------------------

func TestPreferencesSavedDestinationsAddRenameRemove(t *testing.T) {
	dir := t.TempDir()
	newDest := filepath.Join(t.TempDir(), "movies")

	pm := &fakePreferencesManager{cfg: newTestConfig(dir)}
	m := newPrefsTestModel(t, fake.New(), pm)

	f := m.settings.prefsForm
	if f.rowCount() != int(numFixedPrefFields)+1 {
		t.Fatalf("rowCount = %d, want fixed fields plus one add row (no destinations yet)", f.rowCount())
	}

	// Cursor to the add row and press enter: appends a blank destination.
	m.settings.prefsForm.cursor = int(numFixedPrefFields)
	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyEnter})

	f = m.settings.prefsForm
	if len(f.destinations) != 1 {
		t.Fatalf("destinations = %v, want one blank entry after enter on the add row", f.destinations)
	}

	typeText(t, &m, newDest)
	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyCtrlS})

	if pm.saveCallCount() != 1 {
		t.Fatalf("saveCalls = %d, want 1", pm.saveCallCount())
	}
	if got := pm.lastSaved.SavedDestinations; len(got) != 1 || got[0] != newDest {
		t.Fatalf("SavedDestinations = %v, want [%q]", got, newDest)
	}

	// Reopen: the panel should reflect the just-saved destination (T-082:
	// applies live — configSnapshot updated, no restart needed for it).
	updated, cmd := m.handlePreferencesOpen()
	m = updated.(Model)
	m, _ = runCmd(t, m, cmd)

	f = m.settings.prefsForm
	if len(f.destinations) != 1 || f.destinations[0] != newDest {
		t.Fatalf("reopened destinations = %v, want [%q]", f.destinations, newDest)
	}

	// Rename: clear and retype.
	renamed := filepath.Join(t.TempDir(), "renamed")
	m.settings.prefsForm.cursor = int(numFixedPrefFields)
	backspace(t, &m, len(newDest)+4)
	typeText(t, &m, renamed)
	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyCtrlS})

	if got := pm.lastSaved.SavedDestinations; len(got) != 1 || got[0] != renamed {
		t.Fatalf("after rename, SavedDestinations = %v, want [%q]", got, renamed)
	}

	// Remove: reopen, select the destination row, ctrl+x, confirm.
	updated, cmd = m.handlePreferencesOpen()
	m = updated.(Model)
	m, _ = runCmd(t, m, cmd)

	m.settings.prefsForm.cursor = int(numFixedPrefFields)
	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyCtrlX})

	if !m.settings.prefsForm.removeConfirm.IsOpen() {
		t.Fatal("ctrl+x on a destination row must open the remove confirmation")
	}

	sendPrefsKey(t, &m, keyRune("k")) // move off the safer default (Cancel) onto Remove
	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyEnter})
	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyCtrlS})

	if got := pm.lastSaved.SavedDestinations; len(got) != 0 {
		t.Fatalf("after remove, SavedDestinations = %v, want empty", got)
	}
}

// --- removing a destination in use warns --------------------------------

func TestPreferencesRemoveDestinationWarnsWhenActiveTorrentIsThere(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(t.TempDir(), "in-use")

	cfg := newTestConfig(dir)
	cfg.SavedDestinations = []string{dest}

	pm := &fakePreferencesManager{cfg: cfg}
	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	addFakeTorrent(t, eng, "magnet:?xt=urn:btih:prefs1&dn=a.iso", fake.Downloading(30*time.Second))
	// Overwrite SavePath on the tracked torrent by adding a second one with
	// SavePath set — addFakeTorrent doesn't take a SavePath, so add via the
	// engine directly here instead.
	_, err := eng.Add(context.Background(), engine.AddSource{
		Magnet:   "magnet:?xt=urn:btih:prefs2&dn=b.iso",
		SavePath: filepath.Join(dest, "b"),
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	m := newPrefsTestModel(t, eng, pm)
	updated, _ := m.Update(engineUpdateMsg{statuses: eng.List()})
	m = updated.(Model)

	if !m.destinationInUse(dest) {
		t.Fatal("expected destinationInUse to report the active torrent under dest")
	}

	m.settings.prefsForm.cursor = int(numFixedPrefFields)
	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyCtrlX})

	rendered := m.renderPreferencesScreen()
	if !strings.Contains(rendered, "active download is using this destination") {
		t.Fatalf("rendered remove confirmation = %q, want the in-use warning", rendered)
	}
}

// --- save applies live fields and reports restart-required ones --------

func TestPreferencesSaveAppliesLiveFieldsAndReportsRestartRequired(t *testing.T) {
	oldDir := t.TempDir()
	newDir := t.TempDir()

	pm := &fakePreferencesManager{cfg: newTestConfig(oldDir)}
	m := newPrefsTestModel(t, fake.New(), pm)

	f := m.settings.prefsForm
	backspace(t, &m, len(f.downloadDir)+4)
	typeText(t, &m, newDir)

	tabTo(t, &m, 3) // -> max active downloads
	backspace(t, &m, 4)
	typeText(t, &m, "5")

	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyCtrlS})

	if pm.saveCallCount() != 1 {
		t.Fatalf("saveCalls = %d, want 1", pm.saveCallCount())
	}

	if m.downloadDir != newDir {
		t.Fatalf("m.downloadDir = %q, want %q applied live", m.downloadDir, newDir)
	}

	if m.settings.prefsForm != nil {
		t.Fatal("a successful save must close the panel")
	}

	msg := m.statusBar.Message()
	if !strings.Contains(msg, "restart to apply") || !strings.Contains(msg, "max_active_downloads") {
		t.Fatalf("status message = %q, want it to name max_active_downloads as needing a restart", msg)
	}
}

func TestPreferencesEscWithoutChangesClosesImmediately(t *testing.T) {
	dir := t.TempDir()
	pm := &fakePreferencesManager{cfg: newTestConfig(dir)}
	m := newPrefsTestModel(t, fake.New(), pm)

	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.settings.prefsForm != nil {
		t.Fatal("esc with no edits must close the panel immediately")
	}
	if m.settings.view != settingsViewSources {
		t.Fatal("esc must return to the source list view")
	}
}

func TestPreferencesEscWithChangesAsksToDiscard(t *testing.T) {
	dir := t.TempDir()
	pm := &fakePreferencesManager{cfg: newTestConfig(dir)}
	m := newPrefsTestModel(t, fake.New(), pm)

	typeText(t, &m, "x") // dirties the download-dir field

	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.settings.prefsForm == nil || !m.settings.prefsForm.confirmDiscard {
		t.Fatal("esc on a dirty form must ask to confirm discarding")
	}

	sendPrefsKey(t, &m, keyRune("n"))
	if m.settings.prefsForm == nil {
		t.Fatal("n must cancel the discard and keep the form open")
	}

	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyEsc})
	sendPrefsKey(t, &m, keyRune("y"))
	if m.settings.prefsForm != nil {
		t.Fatal("y must confirm the discard and close the form")
	}
	if pm.saveCallCount() != 0 {
		t.Fatal("a discarded form must never save")
	}
}

// --- theme/ascii/seed-policy cycling -------------------------------------

func TestPreferencesThemeAndSeedPolicyCycleWithLeftRight(t *testing.T) {
	dir := t.TempDir()
	pm := &fakePreferencesManager{cfg: newTestConfig(dir)}
	m := newPrefsTestModel(t, fake.New(), pm)

	tabTo(t, &m, 6) // -> seed policy
	before := m.settings.prefsForm.seedPolicy
	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyRight})
	if m.settings.prefsForm.seedPolicy == before {
		t.Fatal("right on seed_policy must cycle it")
	}

	tabTo(t, &m, 5) // -> theme
	beforeTheme := m.settings.prefsForm.theme
	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyRight})
	if m.settings.prefsForm.theme == beforeTheme {
		t.Fatal("right on theme must cycle it")
	}

	tabTo(t, &m, 1) // -> ascii
	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeySpace})
	if !m.settings.prefsForm.ascii {
		t.Fatal("space on ascii must toggle it on")
	}
}
