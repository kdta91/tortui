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

func TestRunDefault(t *testing.T) {
	out, code := captureOutput(t, func(w *os.File) int {
		return run(nil, w)
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	if !strings.Contains(out, "not yet implemented") {
		t.Fatalf("output %q does not mention stub state", out)
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
