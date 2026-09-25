// Package platform holds the per-OS integration points that AGENT.md §14
// requires to stay behind build tags: nothing in this tree may branch on
// runtime.GOOS outside this package.
package platform

import "os/exec"

// runCommandFunc is the seam openURL calls through instead of cmd.Run
// directly — an unexported package variable, the same pattern
// fdlimit_darwin.go's getrlimitFunc/setrlimitFunc use, so a test in this
// package can assert the exact command openURL would have run without
// actually launching a real browser.
var runCommandFunc = func(cmd *exec.Cmd) error { return cmd.Run() }

// openURL runs `xdg-open <rawURL>` — the freedesktop.org URL-open verb
// (AGENT.md §13). rawURL has already been validated as an absolute http(s)
// URL by OpenURL; this never builds a shell command line from it.
func openURL(rawURL string) error {
	return runCommandFunc(exec.Command("xdg-open", rawURL))
}
