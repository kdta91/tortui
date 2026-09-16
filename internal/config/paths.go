package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kdta91/tortui/internal/platform"
)

// tortuiHomeEnv is the environment variable that redirects config, state,
// and download roots to live under one directory, for sandboxed manual
// testing (AGENT.md §15).
const tortuiHomeEnv = "TORTUI_HOME"

// definitionsDirName is the folder inside the config directory that holds
// the user's own indexer definitions (T-023).
const definitionsDirName = "definitions"

// Paths bundles the resolved on-disk locations tortui uses for
// configuration, state, and downloaded data.
type Paths struct {
	// ConfigDir is the directory the config file lives in.
	ConfigDir string
	// ConfigFile is the full path to config.toml.
	ConfigFile string
	// StateDir is the directory for locks, logs, and session data
	// (consumed starting at T-003/T-042).
	StateDir string
	// DownloadDir is the default download destination.
	DownloadDir string

	// DefinitionsDir is the directory holding the user's own scraper
	// source definitions, one *.yml file each. It always sits inside
	// ConfigDir, so it follows --config and $TORTUI_HOME with it, and it
	// is read by internal/indexer/scraper's Loader (T-023). Nothing
	// creates it: a fresh install has no user-supplied source and the
	// lawful defaults are compiled into the binary (T-024).
	DefinitionsDir string
}

// ResolvePaths determines where config, state, and download data live,
// without touching the filesystem.
//
// Precedence:
//  1. $TORTUI_HOME, when set, redirects config, state, and downloads to
//     live under that one directory (AGENT.md §15).
//  2. Otherwise each root is resolved per-OS via internal/platform, which
//     is the only package in this tree allowed to branch on OS (AGENT.md
//     §14).
//
// In both cases, an explicit flagConfigPath (from --config) overrides only
// the config file location, not the state or download roots.
func ResolvePaths(flagConfigPath string) (Paths, error) {
	var p Paths

	if home := os.Getenv(tortuiHomeEnv); home != "" {
		p = Paths{
			ConfigDir:   filepath.Join(home, "config"),
			StateDir:    filepath.Join(home, "state"),
			DownloadDir: filepath.Join(home, "downloads"),
		}
	} else {
		configDir, err := platform.ConfigDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve config directory: %w", err)
		}

		stateDir, err := platform.StateDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve state directory: %w", err)
		}

		downloadDir, err := platform.DownloadDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve download directory: %w", err)
		}

		p = Paths{ConfigDir: configDir, StateDir: stateDir, DownloadDir: downloadDir}
	}

	p.ConfigFile = filepath.Join(p.ConfigDir, "config.toml")

	if flagConfigPath != "" {
		p.ConfigFile = flagConfigPath
		p.ConfigDir = filepath.Dir(flagConfigPath)
	}

	p.DefinitionsDir = filepath.Join(p.ConfigDir, definitionsDirName)

	return p, nil
}
