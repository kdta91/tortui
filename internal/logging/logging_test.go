package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{in: "", want: slog.LevelInfo},
		{in: "info", want: slog.LevelInfo},
		{in: "INFO", want: slog.LevelInfo},
		{in: "debug", want: slog.LevelDebug},
		{in: "warn", want: slog.LevelWarn},
		{in: "warning", want: slog.LevelWarn},
		{in: "error", want: slog.LevelError},
		{in: "  error  ", want: slog.LevelError},
		{in: "bogus", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseLevel(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseLevel(%q) error = nil, want error", tt.in)
				}

				return
			}

			if err != nil {
				t.Fatalf("ParseLevel(%q) unexpected error: %v", tt.in, err)
			}

			if got != tt.want {
				t.Fatalf("ParseLevel(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestResolveLevelPrecedence(t *testing.T) {
	t.Run("config only", func(t *testing.T) {
		if got := ResolveLevel("warn", ""); got != "warn" {
			t.Fatalf("ResolveLevel(warn, \"\") = %q, want warn", got)
		}
	})

	t.Run("flag overrides config", func(t *testing.T) {
		if got := ResolveLevel("warn", "error"); got != "error" {
			t.Fatalf("ResolveLevel(warn, error) = %q, want error", got)
		}
	})

	t.Run("env overrides config but not flag", func(t *testing.T) {
		t.Setenv(EnvLevelVar, "debug")

		if got := ResolveLevel("warn", ""); got != "debug" {
			t.Fatalf("ResolveLevel with env set = %q, want debug", got)
		}

		if got := ResolveLevel("warn", "error"); got != "error" {
			t.Fatalf("ResolveLevel with env and flag set = %q, want flag to win (error)", got)
		}
	})

	t.Run("nothing set", func(t *testing.T) {
		if got := ResolveLevel("", ""); got != "" {
			t.Fatalf("ResolveLevel(\"\", \"\") = %q, want empty (New defaults it to info)", got)
		}
	})
}

func TestResolveFilePrecedence(t *testing.T) {
	t.Run("config only", func(t *testing.T) {
		if got := ResolveFile("/cfg/tortui.log", ""); got != "/cfg/tortui.log" {
			t.Fatalf("ResolveFile = %q, want config value", got)
		}
	})

	t.Run("flag overrides config", func(t *testing.T) {
		if got := ResolveFile("/cfg/tortui.log", "/flag/tortui.log"); got != "/flag/tortui.log" {
			t.Fatalf("ResolveFile = %q, want flag value", got)
		}
	})

	t.Run("env overrides config but not flag", func(t *testing.T) {
		t.Setenv(EnvFileVar, "/env/tortui.log")

		if got := ResolveFile("/cfg/tortui.log", ""); got != "/env/tortui.log" {
			t.Fatalf("ResolveFile with env set = %q, want env value", got)
		}

		if got := ResolveFile("/cfg/tortui.log", "/flag/tortui.log"); got != "/flag/tortui.log" {
			t.Fatalf("ResolveFile with env and flag set = %q, want flag to win", got)
		}
	})
}

func TestNewDefaultsFileUnderStateDir(t *testing.T) {
	dir := t.TempDir()

	logger, closer, err := New(Options{StateDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })

	logger.Info("hello")

	if err := closer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, defaultFileName))
	if err != nil {
		t.Fatalf("expected default log file under StateDir: %v", err)
	}

	if !strings.Contains(string(data), "hello") {
		t.Fatalf("log file %q does not contain expected record", data)
	}
}

func TestNewHonoursExplicitFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.log")

	logger, closer, err := New(Options{File: path})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	logger.Info("explicit path")

	if err := closer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected explicit log file to exist: %v", err)
	}
}

func TestNewRespectsLevel(t *testing.T) {
	dir := t.TempDir()

	logger, closer, err := New(Options{StateDir: dir, Level: "warn"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	logger.Debug("should not appear")
	logger.Info("also should not appear")
	logger.Warn("should appear")

	if err := closer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, defaultFileName))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}

	content := string(data)
	if strings.Contains(content, "should not appear") || strings.Contains(content, "also should not appear") {
		t.Fatalf("log file contains a below-threshold record: %s", content)
	}

	if !strings.Contains(content, "should appear") {
		t.Fatalf("log file missing the at-threshold record: %s", content)
	}
}

func TestNewRejectsInvalidLevel(t *testing.T) {
	if _, _, err := New(Options{StateDir: t.TempDir(), Level: "not-a-level"}); err == nil {
		t.Fatalf("New with invalid level: error = nil, want error")
	}
}

// TestNoStdoutStderrLeakAcrossSimulatedRun is the acceptance test for
// T-003's core requirement: nothing tortui logs ever reaches stdout or
// stderr, at any level, in a structured attr, nested inside a group, bound
// via With, or embedded in free text — because the TUI owns the terminal
// (AGENT.md §3, §13).
func TestNoStdoutStderrLeakAcrossSimulatedRun(t *testing.T) {
	dir := t.TempDir()

	origStdout, origStderr := os.Stdout, os.Stderr
	origDefault := slog.Default()

	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe (stdout): %v", err)
	}

	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe (stderr): %v", err)
	}

	os.Stdout = wOut
	os.Stderr = wErr

	t.Cleanup(func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
		slog.SetDefault(origDefault)
	})

	logger, closer, err := New(Options{StateDir: dir, Level: "debug"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Simulate a representative run: every severity, a structured attr
	// carrying a URL with an embedded credential, a nested group carrying
	// a cookie and an API key, a logger derived via With, and the
	// package-level slog functions (which route through whatever New just
	// installed as the process default).
	logger.Debug("probing indexer", slog.String("indexer_url", "https://real-indexer.example/search?apikey=DEBUG-SECRET"))
	logger.Info("search dispatched", slog.Group("source",
		slog.String("cookie", "session=abc123"),
		slog.String("api_key", "sk-live-999"),
	))
	derived := logger.With(slog.String("source_url", "https://real-indexer.example/feed?apikey=WITH-SECRET"))
	derived.Warn("fallback path taken")
	slog.Error("indexer failed", slog.String("reason", "timeout")) // package-level, exercises SetDefault

	if err := closer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := wOut.Close(); err != nil {
		t.Fatalf("close stdout pipe writer: %v", err)
	}

	if err := wErr.Close(); err != nil {
		t.Fatalf("close stderr pipe writer: %v", err)
	}

	outData, err := io.ReadAll(rOut)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}

	errData, err := io.ReadAll(rErr)
	if err != nil {
		t.Fatalf("read captured stderr: %v", err)
	}

	if len(outData) != 0 {
		t.Fatalf("stdout leaked %d bytes: %q", len(outData), outData)
	}

	if len(errData) != 0 {
		t.Fatalf("stderr leaked %d bytes: %q", len(errData), errData)
	}

	// Sanity check: prove the empty stdout/stderr isn't simply because
	// nothing was logged at all, and that the file sink both received the
	// records and masked the secrets embedded in them.
	logData, err := os.ReadFile(filepath.Join(dir, defaultFileName))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}

	content := string(logData)

	for _, want := range []string{"probing indexer", "search dispatched", "fallback path taken", "indexer failed"} {
		if !strings.Contains(content, want) {
			t.Fatalf("log file missing expected record %q:\n%s", want, content)
		}
	}

	for _, secret := range []string{"DEBUG-SECRET", "abc123", "sk-live-999", "WITH-SECRET", "real-indexer.example"} {
		if strings.Contains(content, secret) {
			t.Fatalf("log file leaked secret %q:\n%s", secret, content)
		}
	}
}
