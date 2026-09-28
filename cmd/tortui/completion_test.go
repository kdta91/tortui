package main

import (
	"flag"
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
	fs, _ := globalFlagSet(os.Stderr)
	names := flagNames(fs)

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

// TestRunAndDoctorAcceptEveryAdvertisedFlag is the regression test for
// review finding 1 on PR #54: run() and runDoctor() used to build their own,
// separately hand-written flag.FlagSet, so a flag renamed in main.go's copy
// (e.g. --demo -> --fake) was never caught here — completion.go's
// globalFlagSet/doctorFlagSet kept advertising the old name in every
// generated script while run() silently accepted the new one instead.
//
// Now that main.go's run() and doctor.go's runDoctor() both call
// globalFlagSet/doctorFlagSet directly (no flag is declared twice), this
// walks every name completion advertises and feeds it to the real run()/
// runDoctor(), asserting the exit code is never 2 ("flag provided but not
// defined") — the failure mode that let --demo drift unnoticed. A bool flag
// is passed bare; anything else gets an empty value via "=".
func TestRunAndDoctorAcceptEveryAdvertisedFlag(t *testing.T) {
	globalFS, _ := globalFlagSet(os.Stderr)
	for _, arg := range flagArgs(globalFS) {
		t.Run("run/"+arg, func(t *testing.T) {
			_, code := captureOutput(t, func(w *os.File) int {
				return run([]string{arg}, w)
			})
			if code == 2 {
				t.Fatalf("run(%q) returned exit code 2 (flag not recognized) — globalFlagSet and run() have diverged", arg)
			}
		})
	}

	doctorFS, _ := doctorFlagSet(os.Stderr)
	for _, arg := range flagArgs(doctorFS) {
		t.Run("doctor/"+arg, func(t *testing.T) {
			_, code := captureOutput(t, func(w *os.File) int {
				return run([]string{"doctor", arg}, w)
			})
			if code == 2 {
				t.Fatalf("run(doctor %q) returned exit code 2 (flag not recognized) — doctorFlagSet and runDoctor() have diverged", arg)
			}
		})
	}
}

// flagArgs renders every flag in fs as a ready-to-pass CLI argument: a bare
// "--name" for a bool flag (flag.Value implementations that also implement
// the unexported boolFlag interface accept no "="value), or "--name=" for
// anything else.
func flagArgs(fs *flag.FlagSet) []string {
	type boolFlag interface {
		IsBoolFlag() bool
	}

	var args []string
	fs.VisitAll(func(f *flag.Flag) {
		if bf, ok := f.Value.(boolFlag); ok && bf.IsBoolFlag() {
			args = append(args, "--"+f.Name)
			return
		}
		args = append(args, "--"+f.Name+"=")
	})

	return args
}
