package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/goleak"
)

// homeEnvVars are every variable internal/platform or os.UserHomeDir reads
// to resolve the real per-user config, state, and download roots on
// darwin, linux, and windows.
var homeEnvVars = []string{
	"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA",
	"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_DATA_HOME", "XDG_DOWNLOAD_DIR",
}

// TestMain points every real-home variable at an empty sentinel directory
// and clears TORTUI_HOME before any test runs, then fails the package if
// anything was written there. A test that reaches config.Load without
// sandboxHome(t) would otherwise create config.toml and a downloads folder
// in the developer's real home (PR #54 review finding 7); here it fails the
// run instead, naming what it wrote. It also fails the run if any goroutine
// outlives the tests (T-093).
func TestMain(m *testing.M) {
	os.Exit(runGuarded(m))
}

func runGuarded(m *testing.M) int {
	sentinel, err := os.MkdirTemp("", "tortui-home-guard-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "home guard: create sentinel: %v\n", err)
		return 1
	}

	defer func() {
		if err := os.RemoveAll(sentinel); err != nil {
			fmt.Fprintf(os.Stderr, "home guard: remove sentinel: %v\n", err)
		}
	}()

	if err := os.Unsetenv("TORTUI_HOME"); err != nil {
		fmt.Fprintf(os.Stderr, "home guard: unset TORTUI_HOME: %v\n", err)
		return 1
	}

	for _, name := range homeEnvVars {
		if err := os.Setenv(name, sentinel); err != nil {
			fmt.Fprintf(os.Stderr, "home guard: set %s: %v\n", name, err)
			return 1
		}
	}

	code := m.Run()

	// T-093: the entrypoint's startup/shutdown paths (doctor, completion,
	// --demo) must leave no goroutine behind. goleak.VerifyTestMain would
	// call os.Exit itself and skip the sentinel scan below, so Find is used
	// directly instead.
	if code == 0 {
		if err := goleak.Find(); err != nil {
			fmt.Fprintf(os.Stderr, "goleak: %v\n", err)
			code = 1
		}
	}

	leaked, err := sentinelContents(sentinel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "home guard: scan sentinel: %v\n", err)
		return 1
	}

	if len(leaked) > 0 {
		fmt.Fprintf(os.Stderr, "home guard: tests wrote outside their sandbox (missing sandboxHome(t)?): %v\n", leaked)
		return 1
	}

	return code
}

// sentinelContents lists every path created under root, relative to it.
func sentinelContents(root string) ([]string, error) {
	var leaked []string

	err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if path == root {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		leaked = append(leaked, rel)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}

	return leaked, nil
}
