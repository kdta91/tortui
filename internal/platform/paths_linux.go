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
// files on Linux: $XDG_CONFIG_HOME/tortui when XDG_CONFIG_HOME is set to an
// absolute path, otherwise ~/.config/tortui.
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
// logs, session data) on Linux: $XDG_STATE_HOME/tortui when set to an
// absolute path, otherwise ~/.local/state/tortui.
func StateDir() (string, error) {
	if v := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(v) {
		return filepath.Join(v, "tortui"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, ".local", "state", "tortui"), nil
}

// DownloadDir returns the default destination for downloaded torrents on
// Linux: $XDG_DOWNLOAD_DIR/tortui when set, otherwise ~/Downloads/tortui.
func DownloadDir() (string, error) {
	if v := os.Getenv("XDG_DOWNLOAD_DIR"); filepath.IsAbs(v) {
		return filepath.Join(v, "tortui"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, "Downloads", "tortui"), nil
}
