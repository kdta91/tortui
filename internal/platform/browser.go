// Package platform holds the per-OS integration points that AGENT.md §14
// requires to stay behind build tags: nothing in this tree may branch on
// runtime.GOOS outside this package.
package platform

import (
	"fmt"
	"net/url"
)

// OpenURL opens rawURL in the user's default system browser — the `u`
// keybind's "open the source page in the system browser" (AGENT.md §7,
// T-063). It shells out to the OS's own URL-open verb (`open` on macOS,
// `xdg-open` on Linux, `rundll32 url.dll,FileProtocolHandler` on Windows —
// the three AGENT.md §13 names), each invoked via exec.Command with an
// argument slice, never a shell (AGENT.md §13: "never pass user text to a
// shell").
//
// rawURL must parse as an absolute http or https URL; anything else —
// unparsable text, a relative reference, a file:// or other scheme — is
// refused before any process is started. This is deliberately narrower than
// AGENT.md §13's file-open hazard (no destination-root containment check
// applies here, since nothing about a browser URL touches the filesystem),
// but it closes the one real risk specific to this call: a source's
// SourceURL is attacker-influenced text, exactly like every other Result
// field, and a file:// or other non-http(s) scheme handed to the OS's open
// verb could reach local files or trigger another registered handler
// instead of a web page.
func OpenURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("platform: parse url %q: %w", rawURL, err)
	}

	if u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("platform: refusing to open non-http(s) url %q", rawURL)
	}

	if err := openURL(rawURL); err != nil {
		return fmt.Errorf("platform: open %q: %w", rawURL, err)
	}

	return nil
}
