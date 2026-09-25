package platform

import (
	"os/exec"
	"strings"
	"testing"
)

// TestOpenURLRefusesNonHTTPSchemes proves OpenURL rejects anything that
// isn't an absolute http(s) URL before it would ever reach the per-OS
// openURL implementation and shell out to a real process. It does not just
// trust OpenURL's own early return: runCommandFunc (the seam every
// openURL implementation calls through) is swapped for a guard that fails
// the test immediately if it is ever invoked, so a mutation that weakens
// or removes the scheme gate is caught here rather than by a real `open`/
// `xdg-open`/`rundll32` process actually launching for attacker-controlled
// input — exactly what happened against `file:///etc/passwd` during PR #42
// review mutation testing before this guard existed.
func TestOpenURLRefusesNonHTTPSchemes(t *testing.T) {
	orig := runCommandFunc
	t.Cleanup(func() { runCommandFunc = orig })

	runCommandFunc = func(cmd *exec.Cmd) error {
		t.Fatalf("runCommandFunc called with %v — a refused input reached the real command instead of being rejected first", cmd.Args)
		return nil
	}

	cases := []string{
		"",
		"not a url at all",
		"file:///etc/passwd",
		"javascript:alert(1)",
		"ftp://example.org/file",
		"http://", // no host
		"relative/path",
	}

	for _, raw := range cases {
		if err := OpenURL(raw); err == nil {
			t.Errorf("OpenURL(%q) = nil error, want a refusal", raw)
		}
	}
}

// TestOpenURLRefusalMentionsInput confirms the rejection error is
// diagnosable — it names what was refused, not just "invalid". Guarded the
// same way TestOpenURLRefusesNonHTTPSchemes is: a real command must never
// run for input OpenURL is meant to reject.
func TestOpenURLRefusalMentionsInput(t *testing.T) {
	orig := runCommandFunc
	t.Cleanup(func() { runCommandFunc = orig })

	runCommandFunc = func(cmd *exec.Cmd) error {
		t.Fatalf("runCommandFunc called with %v — a refused input reached the real command", cmd.Args)
		return nil
	}

	err := OpenURL("ftp://example.org/file")
	if err == nil {
		t.Fatal("OpenURL(ftp url) = nil error, want a refusal")
	}

	if !strings.Contains(err.Error(), "ftp://example.org/file") {
		t.Errorf("OpenURL error = %q, want it to name the refused url", err.Error())
	}
}
