// Package platform holds the per-OS integration points that AGENT.md §14
// requires to stay behind build tags: nothing in this tree may branch on
// runtime.GOOS outside this package.
package platform

import (
	"fmt"
	"os"
	"path/filepath"
)

// ConfigDir returns the root directory tortui uses for its configuration
// files on macOS: $XDG_CONFIG_HOME/tortui when XDG_CONFIG_HOME is set to an
// absolute path, otherwise ~/.config/tortui. macOS intentionally uses the
// XDG convention rather than ~/Library/Application Support — see DEC-005.
//
// This is implemented directly against os.Getenv/os.UserHomeDir rather than
// github.com/adrg/xdg: that library's own macOS default for ConfigHome is
// ~/Library/Application Support, not ~/.config, so using it here would mean
// either silently reintroducing the path DEC-005 rejected or bypassing its
// default resolution entirely — see DEC-022.
func ConfigDir() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(v) {
		return filepath.Join(v, "tortui"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ".config", "tortui"), nil
}

// StateDir returns the root directory tortui uses for state files (locks,
// logs, session data) on macOS. Per AGENT.md §14 this co-locates with the
// config directory rather than using a separate state location.
func StateDir() (string, error) {
	return ConfigDir()
}

// DownloadDir returns the default destination for downloaded torrents on
// macOS: ~/Downloads/tortui. Unlike Linux, AGENT.md §14 does not document an
// $XDG_DOWNLOAD_DIR override for macOS, so none is applied here.
func DownloadDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, "Downloads", "tortui"), nil
}
