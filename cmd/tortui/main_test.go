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

// TestRunLogLevelFlag is the direct regression test for PR #3 QA finding
// 1: --log-level used to be rejected outright ("flag provided but not
// defined", exit 2). It must now be accepted and its resolved value must
// be observable.
func TestRunLogLevelFlag(t *testing.T) {
	out, code := captureOutput(t, func(w *os.File) int {
		return run([]string{"--log-level=debug"}, w)
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, output = %q", code, out)
	}

	if !strings.Contains(out, `log level="debug"`) {
		t.Fatalf("output %q does not reflect the --log-level flag", out)
	}
}

// TestRunLogFileFlag is the direct regression test for the --log-file half
// of the same finding. The value is an opaque flag string here — run never
// touches the filesystem with it — so a bare filename is used rather than a
// path, to keep this test free of any platform path-separator assumption
// (T-005).
func TestRunLogFileFlag(t *testing.T) {
	out, code := captureOutput(t, func(w *os.File) int {
		return run([]string{"--log-file=custom.log"}, w)
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, output = %q", code, out)
	}

	if !strings.Contains(out, `log file="custom.log"`) {
		t.Fatalf("output %q does not reflect the --log-file flag", out)
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

// TestRunLogLevelEnvVarLowerPrecedenceThanFlag confirms the flag continues
// to win over TORTUI_LOG_LEVEL, matching logging.ResolveLevel's documented
// precedence, now that both are reachable from the CLI.
func TestRunLogLevelEnvVarLowerPrecedenceThanFlag(t *testing.T) {
	t.Setenv("TORTUI_LOG_LEVEL", "warn")

	out, code := captureOutput(t, func(w *os.File) int {
		return run([]string{"--log-level=error"}, w)
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, output = %q", code, out)
	}

	if !strings.Contains(out, `log level="error"`) {
		t.Fatalf("output %q does not show the flag winning over the env var", out)
	}
}

// TestRunDefaultLogLevelEmpty confirms no flag/env/config input still
// resolves to an empty string end to end (New/ParseLevel default that to
// "info"; ResolveLevel itself passes an all-empty result through as-is).
func TestRunDefaultLogLevelEmpty(t *testing.T) {
	out, code := captureOutput(t, func(w *os.File) int {
		return run(nil, w)
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	if !strings.Contains(out, `log level=""`) {
		t.Fatalf("output %q does not show the expected empty default", out)
	}
}
