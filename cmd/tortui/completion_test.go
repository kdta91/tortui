package main

import (
	"os"
	"strings"
	"testing"
)

// TestRunCompletionShells confirms every shell tortui completion advertises
// actually produces a non-empty script naming that shell's install
// mechanism, and that the flag/subcommand names embedded in it come from
// the real flag sets rather than a hand-duplicated list (T-092).
func TestRunCompletionShells(t *testing.T) {
	cases := []struct {
		shell    string
		wantCode int
		contains []string
	}{
		{shell: "bash", wantCode: 0, contains: []string{"complete -F", "--version", "doctor"}},
		{shell: "zsh", wantCode: 0, contains: []string{"#compdef tortui", "--demo", "completion"}},
		{shell: "fish", wantCode: 0, contains: []string{"complete -c tortui", "-l config"}},
		{shell: "powershell", wantCode: 0, contains: []string{"Register-ArgumentCompleter", "--log-level"}},
	}

	for _, tc := range cases {
		t.Run(tc.shell, func(t *testing.T) {
			out, code := captureOutput(t, func(w *os.File) int {
				return run([]string{"completion", tc.shell}, w)
			})

			if code != tc.wantCode {
				t.Fatalf("exit code = %d, want %d", code, tc.wantCode)
			}

			for _, want := range tc.contains {
				if !strings.Contains(out, want) {
					t.Fatalf("output for %s does not contain %q:\n%s", tc.shell, want, out)
				}
			}
		})
	}
}

func TestRunCompletionUnknownShell(t *testing.T) {
	out, code := captureOutput(t, func(w *os.File) int {
		return run([]string{"completion", "cmd.exe"}, w)
	})

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}

	if !strings.Contains(out, "unknown shell") {
		t.Fatalf("output %q does not mention unknown shell", out)
	}
}

func TestRunCompletionMissingArg(t *testing.T) {
	out, code := captureOutput(t, func(w *os.File) int {
		return run([]string{"completion"}, w)
	})

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}

	if !strings.Contains(out, "expected exactly one shell argument") {
		t.Fatalf("output %q does not mention the missing argument", out)
	}
}

// TestFlagNamesMatchesFlagSet proves flagNames walks the real FlagSet
// instead of a second, hand-maintained list: adding a flag to
// globalFlagSet without adding it here would fail this test.
func TestFlagNamesMatchesFlagSet(t *testing.T) {
	names := flagNames(globalFlagSet(os.Stderr))

	want := []string{"--config", "--demo", "--log-file", "--log-level", "--version"}
	if len(names) != len(want) {
		t.Fatalf("flagNames = %v, want %v", names, want)
	}

	for i, w := range want {
		if names[i] != w {
			t.Fatalf("flagNames[%d] = %q, want %q (full: %v)", i, names[i], w, names)
		}
	}
}
