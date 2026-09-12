// Package platform holds the per-OS integration points that AGENT.md §14
// requires to stay behind build tags: nothing in this tree may branch on
// runtime.GOOS outside this package.
package platform

import (
	"path/filepath"

	"github.com/adrg/xdg"
)

// ConfigDir returns the root directory tortui uses for its configuration
// files on Linux: $XDG_CONFIG_HOME/tortui when XDG_CONFIG_HOME is set to an
// absolute path, otherwise ~/.config/tortui.
//
// This delegates to github.com/adrg/xdg (AGENT.md §3): on Linux its base
// directory resolution is exactly the XDG Base Directory Specification
// tortui targets here, unlike its macOS and Windows defaults, which diverge
// from this project's chosen conventions — see DEC-022 for why only the
// Linux file uses it. xdg.Reload() re-reads the environment on every call,
// which is what makes this safe to exercise with t.Setenv in tests.
func ConfigDir() (string, error) {
	xdg.Reload()
	return filepath.Join(xdg.ConfigHome, "tortui"), nil
}

// StateDir returns the root directory tortui uses for state files (locks,
// logs, session data) on Linux: $XDG_STATE_HOME/tortui when set to an
// absolute path, otherwise ~/.local/state/tortui.
func StateDir() (string, error) {
	xdg.Reload()
	return filepath.Join(xdg.StateHome, "tortui"), nil
}

// DownloadDir returns the default destination for downloaded torrents on
// Linux: $XDG_DOWNLOAD_DIR/tortui when set, otherwise ~/Downloads/tortui
// (or the user's `user-dirs.dirs`-configured download folder, when present).
func DownloadDir() (string, error) {
	xdg.Reload()
	return filepath.Join(xdg.UserDirs.Download, "tortui"), nil
}
