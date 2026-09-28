package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

// captureOutput runs fn with a writable pipe as *os.File and returns whatever
// was written to it.
func captureOutput(t *testing.T, fn func(out *os.File) int) (string, int) {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	code := fn(w)

	if err := w.Close(); err != nil {
		t.Fatalf("close write end: %v", err)
	}

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}

	if err := r.Close(); err != nil {
		t.Fatalf("close read end: %v", err)
	}

	return string(data), code
}

func TestRunVersion(t *testing.T) {
	out, code := captureOutput(t, func(w *os.File) int {
		return run([]string{"--version"}, w)
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	if !strings.Contains(out, "tortui") || !strings.Contains(out, version) {
		t.Fatalf("output %q does not contain version info", out)
	}
}

// TestRunDefaultRefusesNonInteractiveOutput: a plain `tortui` is the real
// app now (T-095). On a pipe it refuses before anything is written — no
// config, no log, no lock.
func TestRunDefaultRefusesNonInteractiveOutput(t *testing.T) {
	home := sandboxHome(t)

	out, code := captureOutput(t, func(w *os.File) int {
		return run(nil, w)
	})

	if code != 1 {
		t.Fatalf("exit code = %d, want 1 for non-interactive output", code)
	}

	if !strings.Contains(out, "refusing to start") {
		t.Fatalf("output %q does not mention refusing to start", out)
	}

	if strings.Contains(out, "not yet implemented") {
		t.Fatalf("output %q still carries the pre-T-095 stub", out)
	}

	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("read sandbox: %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("a refused start wrote %d entries under TORTUI_HOME, want 0", len(entries))
	}
}

func TestRunInvalidFlag(t *testing.T) {
	_, code := captureOutput(t, func(w *os.File) int {
		return run([]string{"--not-a-real-flag"}, w)
	})

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

// TestRunLogLevelFlagInvalid confirms the flag is actually wired into
// logging.ParseLevel — not merely accepted and ignored — by asserting a
// nonsense level is rejected rather than silently passed through.
func TestRunLogLevelFlagInvalid(t *testing.T) {
	_, code := captureOutput(t, func(w *os.File) int {
		return run([]string{"--log-level=not-a-real-level"}, w)
	})

	if code != 1 {
		t.Fatalf("exit code = %d, want 1 for an invalid --log-level value", code)
	}
}
