package platform

import (
	"errors"
	"os/exec"
	"testing"
)

// TestOpenURLRunsOpenWithTheURLArgument substitutes runCommandFunc (the seam
// openURL calls through) so this test observes the exact command OpenURL
// would run on macOS without actually launching a browser — the real
// runCommandFunc can't be exercised directly in a test process the way
// fdlimit_darwin_test.go's TestRaiseFDLimitRaisesWhenSoftIsBelowHardCeiling
// explains for its own seam.
func TestOpenURLRunsOpenWithTheURLArgument(t *testing.T) {
	orig := runCommandFunc
	t.Cleanup(func() { runCommandFunc = orig })

	var gotPath string
	var gotArgs []string
	runCommandFunc = func(cmd *exec.Cmd) error {
		gotPath = cmd.Path
		gotArgs = cmd.Args
		return nil
	}

	const url = "https://example.org/torrents/1"
	if err := OpenURL(url); err != nil {
		t.Fatalf("OpenURL(%q) error = %v", url, err)
	}

	if !hasSuffix(gotPath, "open") {
		t.Errorf("command path = %q, want it to resolve the %q binary", gotPath, "open")
	}

	if len(gotArgs) != 2 || gotArgs[0] != "open" || gotArgs[1] != url {
		t.Errorf("command args = %v, want [%q %q]", gotArgs, "open", url)
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

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
