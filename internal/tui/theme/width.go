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

// Wrap breaks s into lines that each fit within w terminal columns (via
// Width), breaking on whitespace like an ordinary word-wrap and never
// dropping any content — unlike Truncate, which is for a fixed-shape field
// that can afford to lose its tail, Wrap is for unbounded, attacker-
// influenced text (a torrent title, a source URL) where the whole value
// must remain visible, just reflowed across more than one line.
//
// A single "word" (a run of non-whitespace) wider than w — the common case
// for a URL with no natural break point — is hard-broken at the width
// itself, on a grapheme-cluster boundary, exactly like Truncate never
// splits a cluster in two; any given line is only ever wider than w when w
// itself is too narrow to hold even one grapheme cluster.
//
// w <= 0 returns []string{s} unwrapped — the same "no WindowSizeMsg
// observed yet" tolerance every other width-driven render in this package
// already has. Empty s returns []string{""}, so a caller can always render
// at least one line.
func Wrap(s string, w int) []string {
	if w <= 0 {
		return []string{s}
	}

	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}

	var lines []string

	var cur strings.Builder

	curWidth := 0

	flush := func() {
		lines = append(lines, cur.String())
		cur.Reset()
		curWidth = 0
	}

	for _, word := range words {
		wordWidth := Width(word)

		if wordWidth > w {
			if curWidth > 0 {
				flush()
			}

			chunks, remainder := hardBreak(word, w)
			lines = append(lines, chunks...)
			cur.WriteString(remainder)
			curWidth = Width(remainder)

			continue
		}

		sep := 0
		if curWidth > 0 {
			sep = 1
		}

		if curWidth+sep+wordWidth > w {
			flush()
			sep = 0
		}

		if sep > 0 {
			cur.WriteString(" ")
		}

		cur.WriteString(word)
		curWidth += sep + wordWidth
	}

	if curWidth > 0 || len(lines) == 0 {
		lines = append(lines, cur.String())
	}

	return lines
}

// hardBreak splits word into as many full-width (<= w columns, grapheme-
// safe) chunks as fit, returning them as full plus whatever's left over as
// remainder — the leftover is handed back rather than appended to full so
// Wrap can keep packing short words onto it, the same as it would onto any
// other under-width line.
func hardBreak(word string, w int) (full []string, remainder string) {
	var b strings.Builder

	used := 0
	gr := uniseg.NewGraphemes(word)

	for gr.Next() {
		cw := gr.Width()
		if used+cw > w {
			full = append(full, b.String())
			b.Reset()
			used = 0
		}

		b.WriteString(gr.Str())
		used += cw
	}

	return full, b.String()
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
