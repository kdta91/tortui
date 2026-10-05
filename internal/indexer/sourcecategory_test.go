package indexer

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCleanSourceCategory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "PC/Mac", "PC/Mac"},
		{"trims and collapses space", "  Phone \t\n  Android ", "Phone Android"},
		{"control chars", "A\x00B\x07C\x7fD", "ABCD"},
		{"ansi escape loses its ESC", "\x1b[31mRed\x1b[0m", "[31mRed[0m"},
		{"bidi override and isolates", "a\u202eb\u2066c\u2069d", "abcd"},
		{"zero width and bom", "a\u200bb\u200dc\xef\xbb\xbfd", "abcd"},
		{"variation selector and CGJ", "a\ufe0fb\u034fc", "abc"},
		{"line separators are spaces", "a\u2028b\u2029c", "a b c"},
		{"invalid utf8", "a\xffb", "ab"},
		{"only invisible", "\u202e\u200b\x07 ", ""},
		{"wide text kept", "ソフト", "ソフト"},
	}

	for _, tc := range tests {
		if got := CleanSourceCategory(tc.in); got != tc.want {
			t.Errorf("%s: CleanSourceCategory(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestCleanSourceCategoryCapsLength(t *testing.T) {
	t.Parallel()

	got := CleanSourceCategory(strings.Repeat("é", 10_000))
	if n := utf8.RuneCountInString(got); n != MaxSourceCategoryRunes {
		t.Errorf("got %d runes, want %d", n, MaxSourceCategoryRunes)
	}

	got = CleanSourceCategory(strings.Repeat("ab ", 1000))
	if n := utf8.RuneCountInString(got); n > MaxSourceCategoryRunes || strings.HasSuffix(got, " ") {
		t.Errorf("got %q (%d runes): over the cap or trailing space", got, n)
	}
}
