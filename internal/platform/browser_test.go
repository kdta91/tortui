package platform

import (
	"strings"
	"testing"
)

// TestOpenURLRefusesNonHTTPSchemes proves OpenURL rejects anything that
// isn't an absolute http(s) URL before it would ever reach the per-OS
// openURL implementation and shell out to a real process — this runs on
// every OS without needing the runCommandFunc seam, since a scheme this
// invalid never gets that far.
func TestOpenURLRefusesNonHTTPSchemes(t *testing.T) {
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
// diagnosable — it names what was refused, not just "invalid".
func TestOpenURLRefusalMentionsInput(t *testing.T) {
	err := OpenURL("ftp://example.org/file")
	if err == nil {
		t.Fatal("OpenURL(ftp url) = nil error, want a refusal")
	}

	if !strings.Contains(err.Error(), "ftp://example.org/file") {
		t.Errorf("OpenURL error = %q, want it to name the refused url", err.Error())
	}
}
