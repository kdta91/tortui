// Package logging builds tortui's process-wide slog logger: a masking,
// rotating, file-only sink. log/slog's zero-value default handler writes to
// stderr, which would corrupt the TUI once it takes over the terminal
// (AGENT.md §3), so New installs the logger it builds as slog's default —
// nothing in the process should reach for slog without going through this
// package first.
//
// Callers resolve the effective level and file path themselves, applying
// the precedence in ResolveLevel and ResolveFile (config < environment <
// --log-level/--log-file flag), then pass the result to New. This package
// does not read a config file or a flag.FlagSet directly, so it stays
// usable before the composition root that owns those exists.
package logging

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// EnvLevelVar is the environment variable that overrides config.toml's log
// level, one tier below the --log-level flag (see ResolveLevel).
const EnvLevelVar = "TORTUI_LOG_LEVEL"

// EnvFileVar is the environment variable that overrides config.toml's log
// file path, one tier below the --log-file flag (see ResolveFile).
const EnvFileVar = "TORTUI_LOG_FILE"

// defaultFileName is the log file name used under Options.StateDir when
// Options.File is empty.
const defaultFileName = "tortui.log"

// Options configures the logger New builds.
type Options struct {
	// StateDir is the per-OS state directory (internal/platform.StateDir())
	// the log file lives under when File is empty. Required unless File is
	// set.
	StateDir string

	// File is an absolute path to the log file. Empty resolves to
	// <StateDir>/tortui.log. Callers apply the config/env/flag precedence
	// themselves via ResolveFile before setting this.
	File string

	// Level is the minimum severity to record: "debug", "info", "warn", or
	// "error" (case-insensitive). Empty defaults to "info". Callers apply
	// the config/env/flag precedence themselves via ResolveLevel before
	// setting this.
	Level string

	// MaxSizeBytes is the rotation threshold in bytes. Zero uses
	// DefaultMaxSizeBytes.
	MaxSizeBytes int64

	// MaxBackups is the number of rotated backup files retained. Zero uses
	// DefaultMaxBackups.
	MaxBackups int
}

// ParseLevel parses a level string ("debug", "info", "warn"/"warning", or
// "error", case-insensitively; surrounding whitespace ignored) into a
// slog.Level. An empty string parses as slog.LevelInfo. Anything else is a
// reported error rather than a silent fallback.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("logging: invalid level %q (want debug, info, warn, or error)", s)
	}
}

// ResolveLevel picks the effective level string from config.toml's
// log_level, the TORTUI_LOG_LEVEL environment variable, and the
// --log-level flag, in increasing order of priority: the flag wins over
// the environment variable, which wins over the config file (DEC-024). An
// empty result (nothing set anywhere) is passed through as-is; ParseLevel
// and New both treat an empty string as "info".
func ResolveLevel(configLevel, flagLevel string) string {
	if flagLevel != "" {
		return flagLevel
	}

	if v := os.Getenv(EnvLevelVar); v != "" {
		return v
	}

	return configLevel
}

// ResolveFile picks the effective log file path from config.toml's
// log_file, the TORTUI_LOG_FILE environment variable, and the --log-file
// flag, with the same precedence as ResolveLevel: flag, then environment
// variable, then config file (DEC-024). An empty result is passed through
// as-is; New resolves it to <StateDir>/tortui.log.
func ResolveFile(configFile, flagFile string) string {
	if flagFile != "" {
		return flagFile
	}

	if v := os.Getenv(EnvFileVar); v != "" {
		return v
	}

	return configFile
}

// New builds tortui's file-backed, masking, rotating slog logger and
// installs it as slog's process-wide default. The returned io.Closer must
// be closed on shutdown to flush and release the log file; Close is
// idempotent.
//
// New never writes to stdout or stderr and never leaves slog's original
// stderr-writing default handler active — every caller of the package-level
// slog functions (slog.Info, and so on) is redirected to the file sink
// installed here, from the moment New returns (AGENT.md §3, §13).
func New(opts Options) (*slog.Logger, io.Closer, error) {
	level, err := ParseLevel(opts.Level)
	if err != nil {
		return nil, nil, err
	}

	path := opts.File
	if path == "" {
		if opts.StateDir == "" {
			return nil, nil, errors.New("logging: Options.StateDir or Options.File must be set")
		}

		path = filepath.Join(opts.StateDir, defaultFileName)
	}

	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, nil, fmt.Errorf("create log directory %s: %w", dir, err)
		}
	}

	maxSize := opts.MaxSizeBytes
	if maxSize <= 0 {
		maxSize = DefaultMaxSizeBytes
	}

	maxBackups := opts.MaxBackups
	if maxBackups <= 0 {
		maxBackups = DefaultMaxBackups
	}

	rw, err := newRotatingWriter(path, maxSize, maxBackups)
	if err != nil {
		return nil, nil, err
	}

	handler := newMaskingHandler(slog.NewJSONHandler(rw, &slog.HandlerOptions{Level: level}))
	logger := slog.New(handler)
	slog.SetDefault(logger)

	return logger, rw, nil
}
