package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// configFileMode is the permission mode config.toml is always written with.
// The file may hold API keys and cookies (AGENT.md §6.6), so it is never
// group- or world-readable.
const configFileMode = 0o600

// LoadResult is the outcome of loading — or first-run initializing — the
// configuration.
type LoadResult struct {
	// Config is the loaded (or freshly defaulted) configuration.
	Config Config
	// Paths is where config, state, and download data were resolved to.
	Paths Paths
	// FirstRun is true when no config file existed and tortui just wrote
	// one with defaults.
	FirstRun bool
	// Problems lists every validation issue found in the loaded file, each
	// naming the offending key (including unknown keys the schema does not
	// recognize). Empty means the config is clean. Never populated when
	// FirstRun is true, since a freshly written default config is always
	// valid.
	Problems []string
	// Warnings lists file-level issues that are not key-specific, such as
	// an existing config file being group- or world-readable.
	Warnings []string
}

// Load resolves paths, then reads config.toml from disk — writing a
// default file first on a first run — and returns the fully-populated
// configuration plus any validation problems found.
//
// flagConfigPath, when non-empty, overrides the config file location (the
// --config flag); it does not affect state or download root resolution.
//
// Load never returns a validation problem as a Go error: a malformed value
// for a known key — including an unusable download_dir — is reported in
// Problems so the caller can decide how to surface it. A Go error is
// reserved for conditions the caller cannot recover a Config from at all:
// the file's contents could not be parsed as TOML, or paths could not be
// resolved at all.
func Load(flagConfigPath string) (LoadResult, error) {
	paths, err := ResolvePaths(flagConfigPath)
	if err != nil {
		return LoadResult{}, err
	}

	info, statErr := os.Stat(paths.ConfigFile)
	switch {
	case statErr == nil:
		// fall through to the load-existing-file path below.
	case errors.Is(statErr, os.ErrNotExist):
		cfg := Default(paths.DownloadDir)

		if err := writeDefault(paths.ConfigFile, cfg); err != nil {
			return LoadResult{}, fmt.Errorf("write default config: %w", err)
		}

		if err := os.MkdirAll(cfg.DownloadDir, 0o755); err != nil {
			return LoadResult{}, fmt.Errorf("create download directory: %w", err)
		}

		return LoadResult{Config: cfg, Paths: paths, FirstRun: true}, nil
	default:
		return LoadResult{}, fmt.Errorf("stat config file %s: %w", paths.ConfigFile, statErr)
	}

	var warnings []string
	if info.Mode().Perm()&0o077 != 0 {
		warnings = append(warnings, fmt.Sprintf(
			"config file %s is readable by other users on this machine (mode %s); it may contain API keys or cookies",
			paths.ConfigFile, info.Mode().Perm()))
	}

	cfg := Default(paths.DownloadDir)

	md, err := toml.DecodeFile(paths.ConfigFile, &cfg)
	if err != nil {
		return LoadResult{}, fmt.Errorf("parse config file %s: %w", paths.ConfigFile, err)
	}

	var problems []string
	for _, key := range md.Undecoded() {
		problems = append(problems, fmt.Sprintf("unknown config key: %s", key.String()))
	}

	problems = append(problems, cfg.Validate()...)

	// A download_dir problem is already reported via Validate above; don't
	// also fail Load trying to create an empty or otherwise unusable path.
	if cfg.DownloadDir != "" {
		if err := os.MkdirAll(cfg.DownloadDir, 0o755); err != nil {
			problems = append(problems, fmt.Sprintf("download_dir: could not create %q: %v", cfg.DownloadDir, err))
		}
	}

	return LoadResult{Config: cfg, Paths: paths, Problems: problems, Warnings: warnings}, nil
}

// writeDefault encodes cfg as TOML and writes it to path atomically: a
// temp file in the same directory, fsync, then rename, at mode 0600
// (AGENT.md §6.6; full crash-safety coverage lands in T-042).
func writeDefault(path string, cfg Config) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".config-*.toml.tmp")
	if err != nil {
		return fmt.Errorf("create temp config file: %w", err)
	}
	tmpPath := tmp.Name()

	if err := writeAndSync(tmp, buf.Bytes()); err != nil {
		// Best-effort cleanup; the write/sync error above is what matters.
		_ = os.Remove(tmpPath)

		return err
	}

	if err := os.Chmod(tmpPath, configFileMode); err != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("chmod temp config file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("rename temp config file into place: %w", err)
	}

	return nil
}

// writeAndSync writes data to f, fsyncs it, and closes it, joining any
// close error with an earlier write or sync error rather than discarding
// it.
func writeAndSync(f *os.File, data []byte) (err error) {
	defer func() {
		if cerr := f.Close(); cerr != nil {
			err = errors.Join(err, fmt.Errorf("close temp config file: %w", cerr))
		}
	}()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write temp config file: %w", err)
	}

	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync temp config file: %w", err)
	}

	return nil
}
