package tui

import (
	"context"
	"errors"
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
// panel already open, wired to pm. Free space is scripted (1 TiB) so the
// download directory's min_free_space check never depends on the host disk.
func newPrefsTestModel(t *testing.T, eng engine.Engine, pm PreferencesManager) Model {
	t.Helper()

	return newPrefsTestModelWithProbe(t, eng, pm, freeSpaceProbe(1<<40))
}

// typeText and sendPrefsKey drive the preferences panel through
// handlePreferencesKey and immediately settle whatever tea.Cmd it returns —
// checkPrefsDownloadDirCmd's validation and savePrefsCmd's save result each
// produce exactly one message, so a single runCmd hop (download_actions_test.go's
// own helper) is enough. The only follow-up Cmd that hop can return is the
// status bar's clear timer, which no assertion here depends on.
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

// TestPreferencesDownloadDirCheckMustLandBeforeSave reproduces QA finding 1a
// on PR #50: pressing ctrl+s before prefsDownloadDirCheckMsg arrives must not
// save a download directory that turns out to be a regular file. Earlier,
// fieldIssue only consulted the async check's result when
// f.checkedPath == f.downloadDir and otherwise treated the field as valid,
// so a save racing ahead of the check went through unchecked.
func TestPreferencesDownloadDirCheckMustLandBeforeSave(t *testing.T) {
	dir := t.TempDir()
	occupied := filepath.Join(dir, "occupied")
	if err := os.WriteFile(occupied, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	pm := &fakePreferencesManager{cfg: newTestConfig(dir)}
	m := newPrefsTestModel(t, fake.New(), pm)

	f := m.settings.prefsForm
	clearLen := len(f.downloadDir) + 4

	for i := 0; i < clearLen; i++ {
		updated, cmd := m.handlePreferencesKey(tea.KeyMsg{Type: tea.KeyBackspace})
		m = updated.(Model)
		_ = cmd // dropped: only the final rune's check matters below
	}

	// Type the occupied path but deliberately never run the resulting
	// tea.Cmd, simulating ctrl+s racing ahead of prefsDownloadDirCheckMsg.
	updated, cmd := m.handlePreferencesKey(keyRune(occupied))
	m = updated.(Model)
	_ = cmd

	if got := m.settings.prefsForm.checkedPath; got == strings.TrimSpace(m.settings.prefsForm.downloadDir) {
		t.Fatalf("test setup invalid: the check must not have landed yet, checkedPath = %q", got)
	}

	// SaveConfig only ever runs inside the tea.Cmd ctrl+s returns
	// (savePrefsCmd), so that Cmd must be run here or this assertion could
	// never fail.
	updated, cmd = m.handlePreferencesKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	m, _ = runCmd(t, updated.(Model), cmd)

	if pm.saveCallCount() != 0 {
		t.Fatal("must not save while the download-dir check is still pending")
	}

	if m.settings.prefsForm == nil {
		t.Fatal("a refused save must leave the panel open")
	}
}

// TestPreferencesDownloadDirCheckAppliesDespiteTrailingWhitespace reproduces
// QA finding 1b on PR #50: checkedPath is stored trimmed, but was compared
// against the untrimmed current value, so a trailing space or tab meant the
// check's result never applied and a file path could be saved and applied
// live.
func TestPreferencesDownloadDirCheckAppliesDespiteTrailingWhitespace(t *testing.T) {
	dir := t.TempDir()
	occupied := filepath.Join(dir, "occupied")
	if err := os.WriteFile(occupied, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	pm := &fakePreferencesManager{cfg: newTestConfig(dir)}
	m := newPrefsTestModel(t, fake.New(), pm)

	f := m.settings.prefsForm
	backspace(t, &m, len(f.downloadDir)+4)
	typeText(t, &m, occupied+"\t")

	issues := m.settings.prefsForm.liveIssues()

	found := false

	for _, issue := range issues {
		if strings.Contains(issue, "a file, not a folder") {
			found = true
		}
	}

	if !found {
		t.Fatalf("issues = %v, want the occupied-by-a-file reason despite trailing whitespace", issues)
	}

	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if pm.saveCallCount() != 0 {
		t.Fatal("must not save a trailing-whitespace path pointed at a file")
	}
}

// --- download dir: not-writable / not-enough-space outcomes -------------

// TestPreferencesDownloadDirNotWritable exercises the "not writable" branch
// of destCheck.blockReason through the full preferences flow, with a fake
// m.destProbe standing in for the filesystem (QA finding 3 on PR #50).
func TestPreferencesDownloadDirNotWritable(t *testing.T) {
	dir := t.TempDir()
	pm := &fakePreferencesManager{cfg: newTestConfig(dir)}

	probe := destProbe{
		freeSpace: func(string) (uint64, error) { return 1 << 40, nil },
		writable:  func(string) error { return errors.New("permission denied") },
	}

	m := newPrefsTestModelWithProbe(t, fake.New(), pm, probe)

	issues := m.settings.prefsForm.liveIssues()

	found := false

	for _, issue := range issues {
		if strings.Contains(issue, "not writable") {
			found = true
		}
	}

	if !found {
		t.Fatalf("issues = %v, want a not-writable reason", issues)
	}

	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if pm.saveCallCount() != 0 {
		t.Fatal("must not save an unwritable download directory")
	}
}

// freeSpaceProbe reports free bytes for every path and a real writability
// probe, so a test pins the free-space outcome without depending on the
// host disk.
func freeSpaceProbe(free uint64) destProbe {
	return destProbe{
		freeSpace: func(string) (uint64, error) { return free, nil },
		writable:  probeWritable,
	}
}

// hasIssue reports whether any of issues starts with prefix.
func hasIssue(issues []string, prefix string) bool {
	for _, issue := range issues {
		if strings.HasPrefix(issue, prefix) {
			return true
		}
	}

	return false
}

// TestPreferencesDownloadDirNotEnoughSpace drives the download directory's
// free-space validation through the panel (QA finding 3 on PR #50, R2 on
// its second review): the default directory has no torrent size to add, so
// the bar is the minimum free space itself — a directory with less free
// than that could never start a download (T-034 holds any download once
// free space falls below remaining + min_free_space).
func TestPreferencesDownloadDirNotEnoughSpace(t *testing.T) {
	dir := t.TempDir()
	cfg := newTestConfig(dir)
	cfg.MinFreeSpace = "2GB"

	pm := &fakePreferencesManager{cfg: cfg}
	m := newPrefsTestModelWithProbe(t, fake.New(), pm, freeSpaceProbe(1<<30)) // 1 GiB free

	issues := m.settings.prefsForm.liveIssues()
	if !hasIssue(issues, "Download directory: not enough space") {
		t.Fatalf("issues = %v, want the download directory refused for having less than min_free_space free", issues)
	}

	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if pm.saveCallCount() != 0 {
		t.Fatal("must not save a download directory with less than min_free_space free")
	}
}

// TestPreferencesMinFreeSpaceEditRevalidatesDownloadDir pins R2 on PR #50's
// second review: editing min_free_space alone must re-run the download
// directory's check with the value being typed, so a stale check made
// against the old margin never stays in force.
func TestPreferencesMinFreeSpaceEditRevalidatesDownloadDir(t *testing.T) {
	dir := t.TempDir()
	cfg := newTestConfig(dir)
	cfg.MinFreeSpace = "512MB"

	pm := &fakePreferencesManager{cfg: cfg}
	m := newPrefsTestModelWithProbe(t, fake.New(), pm, freeSpaceProbe(1<<30)) // 1 GiB free

	if issues := m.settings.prefsForm.liveIssues(); len(issues) != 0 {
		t.Fatalf("test setup invalid: issues = %v, want none with 1 GiB free and a 512MB margin", issues)
	}

	tabTo(t, &m, int(prefFieldMinFreeSpace))
	if prefsFieldKind(m.settings.prefsForm.cursor) != prefFieldMinFreeSpace {
		t.Fatalf("cursor = %d, want the min-free-space row", m.settings.prefsForm.cursor)
	}

	backspace(t, &m, len(cfg.MinFreeSpace))
	typeText(t, &m, "2GB")

	issues := m.settings.prefsForm.liveIssues()
	if !hasIssue(issues, "Download directory: not enough space") {
		t.Fatalf("issues = %v, want the download directory re-checked against the new 2GB margin", issues)
	}

	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if pm.saveCallCount() != 0 {
		t.Fatal("must not save once the raised margin no longer fits the download directory")
	}

	backspace(t, &m, len("2GB"))
	typeText(t, &m, "256MB")

	if issues := m.settings.prefsForm.liveIssues(); len(issues) != 0 {
		t.Fatalf("issues = %v, want none once the margin is lowered back under the free space", issues)
	}

	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyCtrlS})
	if pm.saveCallCount() != 1 {
		t.Fatalf("saveCalls = %d, want 1", pm.saveCallCount())
	}

	if got := pm.lastSaved.MinFreeSpace; got != "256MB" {
		t.Fatalf("saved MinFreeSpace = %q, want \"256MB\"", got)
	}
}

// TestPreferencesMinFreeSpaceCheckMustLandBeforeSave is the margin half of
// TestPreferencesDownloadDirCheckMustLandBeforeSave: a check made against
// the old min_free_space must not satisfy ctrl+s after the margin changed,
// even though the download directory's path did not.
func TestPreferencesMinFreeSpaceCheckMustLandBeforeSave(t *testing.T) {
	dir := t.TempDir()
	cfg := newTestConfig(dir)
	cfg.MinFreeSpace = "512MB"

	pm := &fakePreferencesManager{cfg: cfg}
	m := newPrefsTestModelWithProbe(t, fake.New(), pm, freeSpaceProbe(1<<30)) // 1 GiB free

	tabTo(t, &m, int(prefFieldMinFreeSpace))

	// Edit the margin but deliberately never run the resulting tea.Cmds,
	// simulating ctrl+s racing ahead of the re-check.
	for range len(cfg.MinFreeSpace) {
		updated, cmd := m.handlePreferencesKey(tea.KeyMsg{Type: tea.KeyBackspace})
		m = updated.(Model)
		_ = cmd
	}

	updated, cmd := m.handlePreferencesKey(keyRune("2GB"))
	m = updated.(Model)
	_ = cmd

	updated, cmd = m.handlePreferencesKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	m, _ = runCmd(t, updated.(Model), cmd)

	if pm.saveCallCount() != 0 {
		t.Fatal("must not save while the download-dir check for the new margin is still pending")
	}

	if m.settings.prefsForm == nil {
		t.Fatal("a refused save must leave the panel open")
	}
}

// TestPreferencesOpensFromSettingsThroughUpdate drives the panel through
// Model.Update rather than its handlers: "p" on the settings screen opens
// it, the opening check routes back through Update, and esc closes it.
func TestPreferencesOpensFromSettingsThroughUpdate(t *testing.T) {
	dir := t.TempDir()
	pm := &fakePreferencesManager{cfg: newTestConfig(dir)}

	m := New(fake.New(), testTheme(), WithPreferencesManager(pm))
	m.screen = ScreenSettings
	m.width, m.height = 100, 40
	m.destProbe = freeSpaceProbe(1 << 40)

	updated, cmd := m.Update(keyRune("p"))
	m, _ = runCmd(t, updated.(Model), cmd)

	if m.context() != ContextPreferences {
		t.Fatalf("context = %q, want %q after p on the settings screen", m.context(), ContextPreferences)
	}

	if got := m.settings.prefsForm.checkedPath; got != dir {
		t.Fatalf("checkedPath = %q, want the opening check for %q routed back through Update", got, dir)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if m.settings.prefsForm != nil || m.context() == ContextPreferences {
		t.Fatal("esc on an untouched panel must close it")
	}
}

// newPrefsTestModelWithProbe is newPrefsTestModel with a caller-supplied
// destProbe wired in before the panel opens (and its opening check runs),
// so a test can script a full disk or an unwritable folder without
// touching the real filesystem.
func newPrefsTestModelWithProbe(t *testing.T, eng engine.Engine, pm PreferencesManager, probe destProbe) Model {
	t.Helper()

	m := New(eng, testTheme(), WithPreferencesManager(pm))
	m.screen = ScreenSettings
	m.width, m.height = 100, 40
	m.destProbe = probe

	updated, cmd := m.handlePreferencesOpen()
	m = updated.(Model)
	m, _ = runCmd(t, m, cmd)

	return m
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

	// Confirm the removal and save: the destination leaves the saved list,
	// but the in-use torrent's own path stays a known root (AGENT.md §6.12).
	sendPrefsKey(t, &m, keyRune("k")) // off the default (Cancel) onto Remove
	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyEnter})
	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeyCtrlS})

	if pm.saveCallCount() != 1 {
		t.Fatalf("saveCalls = %d, want 1", pm.saveCallCount())
	}

	for _, d := range m.savedDestinations {
		if d == dest {
			t.Fatalf("savedDestinations = %v, want %q removed", m.savedDestinations, dest)
		}
	}

	inUse := filepath.Join(dest, "b")

	found := false

	for _, r := range m.destinationRoots() {
		if r == inUse {
			found = true
		}
	}

	if !found {
		t.Fatalf("destinationRoots = %v, want the in-use torrent's %q still a known root", m.destinationRoots(), inUse)
	}
}

// TestPreferencesSavedDestinationsMostRecentFirst pins the panel's order:
// saved destinations the user added into most recently come first, the
// rest follow in their saved order (the destination picker's own order).
func TestPreferencesSavedDestinationsMostRecentFirst(t *testing.T) {
	root := t.TempDir()
	a, b, c := filepath.Join(root, "a"), filepath.Join(root, "b"), filepath.Join(root, "c")

	cfg := newTestConfig(t.TempDir())
	cfg.SavedDestinations = []string{a, b, c}

	pm := &fakePreferencesManager{cfg: cfg}

	m := New(fake.New(), testTheme(), WithPreferencesManager(pm))
	m.screen = ScreenSettings
	m.destProbe = freeSpaceProbe(1 << 40)
	m.usedDestinations = []string{c, a} // c used most recently, then a

	updated, cmd := m.handlePreferencesOpen()
	m, _ = runCmd(t, updated.(Model), cmd)

	got := m.settings.prefsForm.destinations
	want := []string{c, a, b}

	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("destinations = %v, want %v (most recently used first)", got, want)
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

// --- space typed into a path field ---------------------------------------

// TestPreferencesSpaceTypedIntoDownloadDir reproduces QA finding 5 on PR #50:
// Bubble Tea reports the space bar as its own tea.KeySpace, distinct from
// tea.KeyRunes, so a typed field's append switch (which only matched
// tea.KeyRunes and tea.KeyBackspace) silently dropped every space typed into
// the download directory or a saved destination.
func TestPreferencesSpaceTypedIntoDownloadDir(t *testing.T) {
	dir := t.TempDir()
	pm := &fakePreferencesManager{cfg: newTestConfig(dir)}
	m := newPrefsTestModel(t, fake.New(), pm)

	before := m.settings.prefsForm.downloadDir
	sendPrefsKey(t, &m, tea.KeyMsg{Type: tea.KeySpace})

	if want := before + " "; m.settings.prefsForm.downloadDir != want {
		t.Fatalf("downloadDir = %q, want %q (space must be typed into the field)", m.settings.prefsForm.downloadDir, want)
	}
}

// --- applyTo never discards a parse error --------------------------------

// TestPreferencesApplyToReturnsParseErrorRatherThanDiscarding pins AGENT.md
// §6.9: applyTo must return an error instead of silently zeroing a field
// whose value fails to parse (QA finding 4 on PR #50). fieldIssue already
// keeps an invalid value like this from reaching applyTo through the normal
// ctrl+s path, so this test calls applyTo directly.
func TestPreferencesApplyToReturnsParseErrorRatherThanDiscarding(t *testing.T) {
	dir := t.TempDir()
	f := newPrefsForm(newTestConfig(dir), nil)
	f.maxPeers = "not-a-number"

	if _, err := f.applyTo(newTestConfig(dir)); err == nil {
		t.Fatal("applyTo must return an error for an unparseable field rather than discarding it")
	}
}
