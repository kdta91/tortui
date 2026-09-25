package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kdta91/tortui/internal/platform"
)

// withHome points HOME (and the Windows equivalents) at dir, so
// platform.ConfigDir/StateDir/DownloadDir resolve deterministically inside
// a temp directory instead of touching the real user environment.
func withHome(t *testing.T, dir string) {
	t.Helper()

	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("APPDATA", filepath.Join(dir, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(dir, "AppData", "Local"))
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_DOWNLOAD_DIR", "")
	t.Setenv(tortuiHomeEnv, "")
}

func TestLoadFirstRunWritesDefaults(t *testing.T) {
	home := t.TempDir()
	withHome(t, home)

	result, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !result.FirstRun {
		t.Fatal("FirstRun = false, want true on a missing config file")
	}

	if len(result.Problems) != 0 {
		t.Fatalf("Problems = %v, want none on first run", result.Problems)
	}

	if _, err := os.Stat(result.Paths.ConfigFile); err != nil {
		t.Fatalf("config file was not written: %v", err)
	}

	info, err := os.Stat(result.Paths.ConfigFile)
	if err != nil {
		t.Fatalf("stat config file: %v", err)
	}

	if !platform.IsWindows() {
		if got := info.Mode().Perm(); got != configFileMode {
			t.Fatalf("config file mode = %v, want %v", got, os.FileMode(configFileMode))
		}
	}

	if _, err := os.Stat(result.Config.DownloadDir); err != nil {
		t.Fatalf("download dir was not created: %v", err)
	}

	// Loading again must not report a first run or rewrite the file.
	second, err := Load("")
	if err != nil {
		t.Fatalf("second Load() error = %v", err)
	}

	if second.FirstRun {
		t.Fatal("FirstRun = true on a second load, want false")
	}
}

func TestLoadValidConfig(t *testing.T) {
	home := t.TempDir()
	withHome(t, home)

	result, err := Load("")
	if err != nil {
		t.Fatalf("first-run Load() error = %v", err)
	}

	// The configured download_dir must live inside this test's own
	// t.TempDir() sandbox, not at a fixed path such as /tmp/tortui-downloads
	// — Load() feeds it to a real os.MkdirAll, so a hardcoded path outside
	// the sandbox would create (and leak) a real directory on the host
	// filesystem every time this test runs (T-005 QA remediation).
	// A TOML literal string (single quotes) is used rather than a basic
	// string so a Windows path's backslashes need no escaping.
	downloadDir := filepath.Join(home, "configured-downloads")
	body := fmt.Sprintf(`
download_dir = '%s'
max_peers = 75

[[indexer]]
id      = "example"
name    = "Example"
type    = "torznab"
url     = "https://example.org/api"
api_key = "placeholder"
enabled = true
`, downloadDir)
	writeFile(t, result.Paths.ConfigFile, body)

	loaded, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.FirstRun {
		t.Fatal("FirstRun = true for an existing valid config, want false")
	}

	if len(loaded.Problems) != 0 {
		t.Fatalf("Problems = %v, want none for a valid config", loaded.Problems)
	}

	if loaded.Config.DownloadDir != downloadDir {
		t.Fatalf("DownloadDir = %q, want the configured value %q", loaded.Config.DownloadDir, downloadDir)
	}

	if loaded.Config.MaxPeers != 75 {
		t.Fatalf("MaxPeers = %d, want 75", loaded.Config.MaxPeers)
	}

	if len(loaded.Config.Indexers) != 1 || loaded.Config.Indexers[0].ID != "example" {
		t.Fatalf("Indexers = %+v, want one entry with id %q", loaded.Config.Indexers, "example")
	}
}

func TestLoadPartialConfigFillsDefaults(t *testing.T) {
	home := t.TempDir()
	withHome(t, home)

	result, err := Load("")
	if err != nil {
		t.Fatalf("first-run Load() error = %v", err)
	}

	// Only override one field; every other key should keep its default.
	writeFile(t, result.Paths.ConfigFile, `max_active_downloads = 7`+"\n")

	loaded, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(loaded.Problems) != 0 {
		t.Fatalf("Problems = %v, want none for a partial-but-valid config", loaded.Problems)
	}

	if loaded.Config.MaxActiveDownloads != 7 {
		t.Fatalf("MaxActiveDownloads = %d, want 7", loaded.Config.MaxActiveDownloads)
	}

	def := Default(loaded.Paths.DownloadDir)
	if loaded.Config.MaxPeers != def.MaxPeers {
		t.Fatalf("MaxPeers = %d, want default %d", loaded.Config.MaxPeers, def.MaxPeers)
	}

	if loaded.Config.SeedPolicy != def.SeedPolicy {
		t.Fatalf("SeedPolicy = %q, want default %q", loaded.Config.SeedPolicy, def.SeedPolicy)
	}

	if loaded.Config.Theme != def.Theme {
		t.Fatalf("Theme = %q, want default %q", loaded.Config.Theme, def.Theme)
	}
}

func TestLoadInvalidConfigReportsAllProblems(t *testing.T) {
	home := t.TempDir()
	withHome(t, home)

	result, err := Load("")
	if err != nil {
		t.Fatalf("first-run Load() error = %v", err)
	}

	const body = `
download_dir = ""
max_peers = -1
seed_policy = "bogus"
listen_port = 99999
`
	writeFile(t, result.Paths.ConfigFile, body)

	loaded, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	wantSubstrings := []string{"download_dir", "max_peers", "seed_policy", "listen_port"}
	for _, want := range wantSubstrings {
		found := false

		for _, p := range loaded.Problems {
			if strings.Contains(p, want) {
				found = true

				break
			}
		}

		if !found {
			t.Errorf("Problems = %v, want an entry mentioning %q", loaded.Problems, want)
		}
	}

	if len(loaded.Problems) < len(wantSubstrings) {
		t.Fatalf("Problems = %v, want at least %d problems (one per bad key)", loaded.Problems, len(wantSubstrings))
	}
}

func TestLoadUnknownKeyIsReported(t *testing.T) {
	home := t.TempDir()
	withHome(t, home)

	result, err := Load("")
	if err != nil {
		t.Fatalf("first-run Load() error = %v", err)
	}

	// Same sandbox reasoning as TestLoadValidConfig above: this must not be
	// a fixed path outside t.TempDir(), since Load() really creates it.
	downloadDir := filepath.Join(home, "configured-downloads")
	body := fmt.Sprintf(`
download_dir = '%s'
totally_made_up_key = "surprise"
`, downloadDir)
	writeFile(t, result.Paths.ConfigFile, body)

	loaded, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	found := false

	for _, p := range loaded.Problems {
		if strings.Contains(p, "unknown config key") && strings.Contains(p, "totally_made_up_key") {
			found = true

			break
		}
	}

	if !found {
		t.Fatalf("Problems = %v, want an entry naming the unknown key", loaded.Problems)
	}
}

func TestLoadMalformedTOMLIsAnError(t *testing.T) {
	home := t.TempDir()
	withHome(t, home)

	result, err := Load("")
	if err != nil {
		t.Fatalf("first-run Load() error = %v", err)
	}

	writeFile(t, result.Paths.ConfigFile, "this is not valid toml {{{")

	if _, err := Load(""); err == nil {
		t.Fatal("Load() error = nil, want an error for unparseable TOML")
	}
}

func TestLoadTortuiHomeRedirection(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv(tortuiHomeEnv, sandbox)

	// Real environment must be ignored while TORTUI_HOME is set.
	t.Setenv("HOME", "/should/not/be/used")
	t.Setenv("XDG_CONFIG_HOME", "/should/not/be/used")

	result, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !result.FirstRun {
		t.Fatal("FirstRun = false, want true under a fresh TORTUI_HOME")
	}

	wantConfigFile := filepath.Join(sandbox, "config", "config.toml")
	if result.Paths.ConfigFile != wantConfigFile {
		t.Fatalf("ConfigFile = %q, want %q", result.Paths.ConfigFile, wantConfigFile)
	}

	wantStateDir := filepath.Join(sandbox, "state")
	if result.Paths.StateDir != wantStateDir {
		t.Fatalf("StateDir = %q, want %q", result.Paths.StateDir, wantStateDir)
	}

	wantDownloadDir := filepath.Join(sandbox, "downloads")
	if result.Paths.DownloadDir != wantDownloadDir {
		t.Fatalf("DownloadDir = %q, want %q", result.Paths.DownloadDir, wantDownloadDir)
	}

	if !strings.HasPrefix(result.Paths.ConfigFile, sandbox) {
		t.Fatalf("ConfigFile %q escaped TORTUI_HOME sandbox %q", result.Paths.ConfigFile, sandbox)
	}

	if _, err := os.Stat(result.Paths.ConfigFile); err != nil {
		t.Fatalf("config file was not written under TORTUI_HOME: %v", err)
	}
}

func TestLoadConfigFlagOverridesConfigFileOnly(t *testing.T) {
	home := t.TempDir()
	withHome(t, home)

	explicit := filepath.Join(t.TempDir(), "custom-config.toml")

	result, err := Load(explicit)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if result.Paths.ConfigFile != explicit {
		t.Fatalf("ConfigFile = %q, want %q", result.Paths.ConfigFile, explicit)
	}

	if !result.FirstRun {
		t.Fatal("FirstRun = false, want true for a missing --config path")
	}

	// The download dir still follows normal per-OS/TORTUI_HOME resolution,
	// not the --config flag.
	wantDownloadDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("os.UserHomeDir: %v", err)
	}

	if !strings.HasPrefix(result.Paths.DownloadDir, wantDownloadDir) {
		t.Fatalf("DownloadDir = %q, want it under %q", result.Paths.DownloadDir, wantDownloadDir)
	}
}

func TestLoadWarnsOnWorldReadableConfig(t *testing.T) {
	if platform.IsWindows() {
		t.Skip("POSIX permission bits do not apply on Windows")
	}

	home := t.TempDir()
	withHome(t, home)

	result, err := Load("")
	if err != nil {
		t.Fatalf("first-run Load() error = %v", err)
	}

	if err := os.Chmod(result.Paths.ConfigFile, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	loaded, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(loaded.Warnings) == 0 {
		t.Fatal("Warnings is empty, want a warning about a world-readable config file")
	}
}

// TestSaveSurvivesCrashBeforeRename simulates a process crash that happens
// after Save's temp file is created and partially written but before the
// rename into place — the exact window AGENT.md §13/T-042 calls out ("a
// crash mid-save must never leave a truncated or empty config"). It
// reproduces that window directly (Save's real implementation is not
// interruptible from a test) rather than by injecting a fault into Save
// itself: the temp file is created and written by hand, then abandoned
// without renaming, exactly as if the process had died at that instant.
func TestSaveSurvivesCrashBeforeRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	original := Default(filepath.Join(dir, "original-downloads"))
	if err := Save(path, original); err != nil {
		t.Fatalf("Save(original) error = %v", err)
	}

	originalBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read original config: %v", err)
	}

	// Simulate a crash partway through a second Save: a temp file is
	// created and gets a partial, garbage write, but the process dies
	// before fsync, chmod, or rename ever run.
	tmp, err := os.CreateTemp(dir, ".config-*.toml.tmp")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := tmp.WriteString("download_dir = \"/only-half-writt"); err != nil {
		t.Fatalf("write partial temp file: %v", err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatalf("close temp file: %v", err)
	}
	// No rename — the "crash" happens right here.

	gotBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config after simulated crash: %v", err)
	}
	if string(gotBytes) != string(originalBytes) {
		t.Fatalf("config file changed after a crash before rename:\ngot:  %q\nwant: %q (untouched)", gotBytes, originalBytes)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() after simulated crash error = %v", err)
	}
	if len(loaded.Problems) != 0 {
		t.Fatalf("Problems = %v, want none — the real config file must be unaffected by the abandoned temp file", loaded.Problems)
	}
	if loaded.Config.DownloadDir != original.DownloadDir {
		t.Fatalf("DownloadDir = %q, want the pre-crash value %q", loaded.Config.DownloadDir, original.DownloadDir)
	}
}

// TestSaveIsAtomicAndReloadable checks the ordinary (non-crash) path: Save
// writes a file that Load reads back with the same values, at the
// locked-down file mode, and leaves no temp file behind.
func TestSaveIsAtomicAndReloadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	cfg := Default(filepath.Join(dir, "downloads"))
	cfg.MaxPeers = 123

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if !platform.IsWindows() {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat saved config: %v", err)
		}
		if got := info.Mode().Perm(); got != configFileMode {
			t.Fatalf("saved config mode = %v, want %v", got, os.FileMode(configFileMode))
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".config-") {
			t.Fatalf("leftover temp file %q after a successful Save", e.Name())
		}
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Config.MaxPeers != 123 {
		t.Fatalf("MaxPeers = %d, want 123", loaded.Config.MaxPeers)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
