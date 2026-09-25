package platform

import (
	"errors"
	"os/exec"
	"testing"
)

// TestOpenURLRunsXDGOpenWithTheURLArgument substitutes runCommandFunc (the
// seam openURL calls through) so this test observes the exact command
// OpenURL would run on Linux without actually launching a browser.
func TestOpenURLRunsXDGOpenWithTheURLArgument(t *testing.T) {
	orig := runCommandFunc
	t.Cleanup(func() { runCommandFunc = orig })

	var gotArgs []string
	runCommandFunc = func(cmd *exec.Cmd) error {
		gotArgs = cmd.Args
		return nil
	}

	const url = "https://example.org/torrents/1"
	if err := OpenURL(url); err != nil {
		t.Fatalf("OpenURL(%q) error = %v", url, err)
	}

	if len(gotArgs) != 2 || gotArgs[0] != "xdg-open" || gotArgs[1] != url {
		t.Errorf("command args = %v, want [%q %q]", gotArgs, "xdg-open", url)
	}
}

// TestOpenURLWrapsCommandFailure confirms a failure to launch the browser
// (binary missing, refused, etc.) is reported as an error rather than
// silently swallowed (AGENT.md §6.9).
func TestOpenURLWrapsCommandFailure(t *testing.T) {
	orig := runCommandFunc
	t.Cleanup(func() { runCommandFunc = orig })

	wantErr := errors.New("boom")
	runCommandFunc = func(cmd *exec.Cmd) error { return wantErr }

	err := OpenURL("https://example.org")
	if err == nil {
		t.Fatal("OpenURL() error = nil, want the wrapped command failure")
	}

	if !errors.Is(err, wantErr) {
		t.Errorf("OpenURL() error = %v, want it to wrap %v", err, wantErr)
	}
}
