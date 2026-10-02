package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
)

// newBuiltinManager builds a settingsManager over a temp config file and a
// registry fed by rt, so nothing reaches the network.
func newBuiltinManager(t *testing.T, cfg config.Config, rt http.RoundTripper) (*settingsManager, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	defs := t.TempDir()
	live := buildSources(cfg, defs, discardLogger(), rt)

	return newSettingsManager(path, cfg, live, engine.RootAdder(nil), discardLogger()), path
}

func registeredIDs(m *settingsManager) []string {
	var ids []string
	for _, ix := range m.live.reg.Enabled() {
		ids = append(ids, ix.ID())
	}

	return ids
}

// TestDisabledBuiltinIsNotRegistered: the config list switches a bundled
// source off; a fresh config leaves it on (standalone contract).
func TestDisabledBuiltinIsNotRegistered(t *testing.T) {
	id := bundledID(t)

	on := config.Default(t.TempDir())
	if !registryIDs(t, on, t.TempDir())[id] {
		t.Fatal("bundled source off on a fresh config")
	}

	off := config.Default(t.TempDir())
	off.DisabledBuiltins = []string{id}

	if registryIDs(t, off, t.TempDir())[id] {
		t.Fatal("bundled source still registered though disabled_builtin lists it")
	}
}

func TestBuiltinSourcesListsBundledWithState(t *testing.T) {
	id := bundledID(t)
	m, _ := newBuiltinManager(t, config.Default(t.TempDir()), &recordingTransport{})

	rows, _ := m.BuiltinSources()
	if len(rows) == 0 || rows[0].ID != id || !rows[0].Enabled || rows[0].Name == "" {
		t.Fatalf("rows = %+v, want the bundled source, enabled, named", rows)
	}

	if err := m.SetBuiltinEnabled(id, false); err != nil {
		t.Fatal(err)
	}

	if after, _ := m.BuiltinSources(); after[0].Enabled {
		t.Fatal("row still enabled after SetBuiltinEnabled(false)")
	}
}

// TestBuiltinRowHiddenWhenAnEntryReplacesIt: the [[indexer]] entry is the
// row the user sees; a second built-in row would be a duplicate.
func TestBuiltinRowHiddenWhenAnEntryReplacesIt(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Indexers = []config.Indexer{{ID: bundledID(t), Name: "mine", Type: "scraper", Definition: "x.yml", Enabled: false}}

	m, _ := newBuiltinManager(t, cfg, &recordingTransport{})

	if rows, _ := m.BuiltinSources(); len(rows) != 0 {
		t.Fatalf("rows = %+v, want none", rows)
	}
}

// TestSetBuiltinEnabledPersistsAndSurvivesRestart: the off switch lands in
// config.toml, the registry follows at once, and a restart (Load + build)
// reads it back; switching on removes it again.
func TestSetBuiltinEnabledPersistsAndSurvivesRestart(t *testing.T) {
	id := bundledID(t)
	m, path := newBuiltinManager(t, config.Default(t.TempDir()), &recordingTransport{})

	if !slices.Contains(registeredIDs(m), id) {
		t.Fatal("bundled source not registered at start")
	}

	if err := m.SetBuiltinEnabled(id, false); err != nil {
		t.Fatal(err)
	}

	if slices.Contains(registeredIDs(m), id) {
		t.Fatal("registry still holds the source after disabling")
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(loaded.Config.DisabledBuiltins, id) {
		t.Fatalf("disabled_builtin = %v after the save, want %q", loaded.Config.DisabledBuiltins, id)
	}

	if registryIDs(t, loaded.Config, t.TempDir())[id] {
		t.Fatal("restart re-enabled a disabled built-in")
	}

	if err := m.SetBuiltinEnabled(id, true); err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(registeredIDs(m), id) {
		t.Fatal("registry lacks the source after re-enabling")
	}

	loaded, err = config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if slices.Contains(loaded.Config.DisabledBuiltins, id) {
		t.Fatalf("disabled_builtin = %v after re-enabling", loaded.Config.DisabledBuiltins)
	}
}

func TestSetBuiltinEnabledRefusesAnUnknownID(t *testing.T) {
	m, path := newBuiltinManager(t, config.Default(t.TempDir()), &recordingTransport{})

	if err := m.SetBuiltinEnabled("nope", false); err == nil {
		t.Fatal("an id that is no bundled source was accepted")
	}

	if _, err := config.Load(path); err != nil {
		t.Fatal(err)
	}

	if len(m.Config().DisabledBuiltins) != 0 {
		t.Fatalf("disabled list = %v after a refused id", m.Config().DisabledBuiltins)
	}
}

// TestOtherSavesKeepTheDisabledList: saving sources or preferences rewrites
// config.toml and must not forget which built-ins are off.
func TestOtherSavesKeepTheDisabledList(t *testing.T) {
	id := bundledID(t)
	cfg := config.Default(t.TempDir())
	m, path := newBuiltinManager(t, cfg, &recordingTransport{})

	if err := m.SetBuiltinEnabled(id, false); err != nil {
		t.Fatal(err)
	}

	if err := m.SaveSources([]config.Indexer{{ID: "x", Name: "X", Type: "torznab", URL: "https://example.org/a", Enabled: true}}); err != nil {
		t.Fatal(err)
	}

	prefs := m.Config()
	prefs.DisabledBuiltins = nil // a stale snapshot

	if err := m.SaveConfig(prefs); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(loaded.Config.DisabledBuiltins, id) {
		t.Fatalf("disabled_builtin = %v after other saves, want %q kept", loaded.Config.DisabledBuiltins, id)
	}
}

// TestTestBuiltinProbesThroughTheTransport: the probe runs one search
// through the injected transport (a 404 here) and returns its error, with
// no real network.
func TestTestBuiltinProbesThroughTheTransport(t *testing.T) {
	rt := &recordingTransport{}
	m, _ := newBuiltinManager(t, config.Default(t.TempDir()), rt)

	if err := m.TestBuiltin(context.Background(), bundledID(t)); err == nil {
		t.Fatal("a 404 probe reported success")
	}

	if rt.count() == 0 {
		t.Fatal("the probe made no request through the transport")
	}

	if err := m.TestBuiltin(context.Background(), "nope"); err == nil {
		t.Fatal("an unknown id was probed")
	}
}

func TestDoctorBuiltinsNamesTheBundledSources(t *testing.T) {
	got := DoctorBuiltins(t.TempDir())
	if len(got) == 0 || got[0].ID != bundledID(t) || got[0].URL == "" || got[0].Name == "" {
		t.Fatalf("DoctorBuiltins = %+v", got)
	}
}

// overrideDefinition is a valid definition for the bundled id whose address
// is an invented host, so it is easy to tell from the embedded one.
func overrideDefinition(id string) string {
	return strings.NewReplacer("example-user", id, "https://example.org", "https://override.example.org").Replace(userDefinition)
}

// TestDoctorAndSettingsTestResolveTheSameDefinition (T-9075): with a
// definition of a bundled id in the definitions directory, doctor lists its
// address and Settings `t` sends its request there.
func TestDoctorAndSettingsTestResolveTheSameDefinition(t *testing.T) {
	id := bundledID(t)
	defs := t.TempDir()

	if err := os.WriteFile(filepath.Join(defs, id+".yml"), []byte(overrideDefinition(id)), 0o600); err != nil {
		t.Fatal(err)
	}

	got := DoctorBuiltins(defs)
	if len(got) == 0 || got[0].ID != id || got[0].URL != "https://override.example.org" {
		t.Fatalf("doctor lists %+v, want the override's address", got)
	}

	rt := &recordingTransport{}
	cfg := config.Default(t.TempDir())
	live := buildSources(cfg, defs, discardLogger(), rt)
	m := newSettingsManager(filepath.Join(t.TempDir(), "config.toml"), cfg, live, engine.RootAdder(nil), discardLogger())

	_ = m.TestBuiltin(context.Background(), id) // the 404 is the transport's

	rt.mu.Lock()
	defer rt.mu.Unlock()

	if len(rt.urls) == 0 || rt.urls[0] != "override.example.org" {
		t.Fatalf("Settings test requested %v, want override.example.org", rt.urls)
	}
}

// TestBuiltinVersionRisesOnlyOnASuccessfulSave (T-9111): the TUI orders two
// reads by this number, so a save must raise it and a refused one must not.
func TestBuiltinVersionRisesOnlyOnASuccessfulSave(t *testing.T) {
	id := bundledID(t)
	m, _ := newBuiltinManager(t, config.Default(t.TempDir()), &recordingTransport{})

	_, v0 := m.BuiltinSources()

	if err := m.SetBuiltinEnabled("not-a-bundled-id", false); err == nil {
		t.Fatal("unknown id accepted")
	}

	if _, v := m.BuiltinSources(); v != v0 {
		t.Fatalf("version %d after a refused toggle, want %d", v, v0)
	}

	if err := m.SetBuiltinEnabled(id, false); err != nil {
		t.Fatal(err)
	}

	_, v1 := m.BuiltinSources()
	if v1 <= v0 {
		t.Fatalf("version %d after a toggle, want above %d", v1, v0)
	}

	if err := m.SaveSources(nil); err != nil {
		t.Fatal(err)
	}

	if _, v2 := m.BuiltinSources(); v2 <= v1 {
		t.Fatalf("version %d after SaveSources, want above %d", v2, v1)
	}
}
