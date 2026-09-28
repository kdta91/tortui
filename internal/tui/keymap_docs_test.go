package tui

import (
	"os"
	"strings"
	"testing"
)

// readmePath is ../../README.md relative to this package — this package's
// own tests already run with the package directory as the working
// directory, so this is the same relative path every other package would
// use to reach the repo root from two levels down.
const readmePath = "../../README.md"

const (
	keymapMarkerStart = "<!-- keymap:start -->"
	keymapMarkerEnd   = "<!-- keymap:end -->"
)

// extractBetween returns the trimmed text strictly between the first start
// and end markers in s, or an error naming which marker is missing.
func extractBetween(s, start, end string) (string, error) {
	i := strings.Index(s, start)
	if i < 0 {
		return "", errMarkerNotFound(start)
	}

	rest := s[i+len(start):]

	j := strings.Index(rest, end)
	if j < 0 {
		return "", errMarkerNotFound(end)
	}

	return strings.TrimSpace(rest[:j]), nil
}

type errMarkerNotFound string

func (e errMarkerNotFound) Error() string { return "marker not found: " + string(e) }

// TestReadmeKeysTableMatchesGlobalBindings is T-090's direct fix for a
// README table that was typed by hand and drifted from GlobalBindings'
// actual Help strings (found in review of PR #52): it reads README.md,
// extracts the block between the keymap markers, and fails with a diff the
// moment it stops being exactly GlobalKeymapReadmeTable()'s output — the
// same data Model.handleKey and the `?` overlay are built from — so the
// table can never silently drift again.
func TestReadmeKeysTableMatchesGlobalBindings(t *testing.T) {
	raw, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read %s: %v", readmePath, err)
	}

	got, err := extractBetween(string(raw), keymapMarkerStart, keymapMarkerEnd)
	if err != nil {
		t.Fatalf("README.md: %v", err)
	}

	want := GlobalKeymapReadmeTable()

	if got != want {
		t.Fatalf("README.md's keymap table has drifted from GlobalBindings().\n\nREADME has:\n%s\n\nGlobalKeymapReadmeTable() wants:\n%s", got, want)
	}
}

// TestReadmeLegalNoticeMatchesConstant is a cheap drift check for the other
// half of T-090's first-run acceptance criterion — "carries the same legal
// notice as the README" — the direction round-tripped: README's own Legal
// notice section must still contain LegalNotice's exact sentence, modulo
// the Markdown bold markers and line-wrapping around it, so editing one
// without the other fails a test instead of silently drifting apart.
func TestReadmeLegalNoticeMatchesConstant(t *testing.T) {
	raw, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read %s: %v", readmePath, err)
	}

	readme := normalizeProse(string(raw))
	want := normalizeProse(LegalNotice)

	if !strings.Contains(readme, want) {
		t.Fatalf("README.md's Legal notice section does not contain LegalNotice verbatim (modulo whitespace/markdown bold).\n\nLegalNotice:\n%s", want)
	}
}

// normalizeProse strips Markdown bold markers and collapses all whitespace
// (including newlines, so a line-wrapped README paragraph still compares
// equal to the single-line Go constant it was copied from) to single
// spaces.
func normalizeProse(s string) string {
	s = strings.ReplaceAll(s, "**", "")

	return strings.Join(strings.Fields(s), " ")
}
