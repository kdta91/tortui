package main

import (
	"os"
	"strings"
	"testing"
)

// TestRunDemoRefusesNonInteractiveOutput confirms `tortui --demo` follows
// the same non-TTY refusal every TUI launch must (AGENT.md §14): the test
// harness's pipe is never a terminal, so this exercises the real refusal
// path rather than ever starting a bubbletea program in the test process.
func TestRunDemoRefusesNonInteractiveOutput(t *testing.T) {
	out, code := captureOutput(t, func(w *os.File) int {
		return run([]string{"--demo"}, w)
	})

	if code != 1 {
		t.Fatalf("exit code = %d, want 1 for non-interactive output", code)
	}

	if !strings.Contains(out, "refusing to start") {
		t.Fatalf("output %q does not mention refusing to start", out)
	}
}
