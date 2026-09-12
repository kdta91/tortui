// Package platform holds the per-OS integration points that AGENT.md §14
// requires to stay behind build tags: nothing in this tree may branch on
// runtime.GOOS outside this package.
package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ConfigDir returns the root directory tortui uses for its configuration
// files on Windows: %AppData%\tortui.
func ConfigDir() (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return "", errors.New("%AppData% is not set")
	}

	return filepath.Join(appData, "tortui"), nil
}

// StateDir returns the root directory tortui uses for state files (locks,
// logs, session data) on Windows: %LocalAppData%\tortui.
func StateDir() (string, error) {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		return "", errors.New("%LocalAppData% is not set")
	}

	return filepath.Join(localAppData, "tortui"), nil
}

// DownloadDir returns the default destination for downloaded torrents on
// Windows: the user's Downloads folder, under their profile directory.
func DownloadDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, "Downloads", "tortui"), nil
}
