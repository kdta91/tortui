package theme

import (
	"strings"

	"github.com/rivo/uniseg"
)

// Width returns the terminal display width of s, measured grapheme
// cluster by grapheme cluster via rivo/uniseg (AGENT.md §14). Torrent
// titles routinely contain CJK text, emoji, and combining marks; neither
// len() nor utf8.RuneCountInString reports the right number of terminal
// columns for those, and a column built from either misaligns.
func Width(s string) int {
	return uniseg.StringWidth(s)
}

// Pad right-pads s with spaces until it measures w terminal columns wide
// (via Width), so mixed CJK/ASCII/emoji columns line up in a table. If s
// already measures at or beyond w, it is returned unchanged — Pad never
// truncates; call Truncate first when the value must fit exactly.
func Pad(s string, w int) string {
	n := Width(s)
	if n >= w {
		return s
	}

	return s + strings.Repeat(" ", w-n)
}

// Truncate shortens s to fit within w terminal columns (via Width),
// appending a three-dot ellipsis when content is cut off. It always cuts
// on a grapheme-cluster boundary, so it never splits a multi-rune emoji
// or a combining-mark sequence in two.
func Truncate(s string, w int) string {
	if Width(s) <= w {
		return s
	}

	if w <= 0 {
		return ""
	}

	const ellipsis = "..."
	if w <= len(ellipsis) {
		return ellipsis[:w]
	}

	limit := w - len(ellipsis)

	var b strings.Builder

	used := 0
	gr := uniseg.NewGraphemes(s)

	for gr.Next() {
		cw := gr.Width()
		if used+cw > limit {
			break
		}

		b.WriteString(gr.Str())
		used += cw
	}

	b.WriteString(ellipsis)

	return b.String()
}
