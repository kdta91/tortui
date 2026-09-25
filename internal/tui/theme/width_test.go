package theme

import (
	"strings"
	"testing"
)

func TestWidthASCII(t *testing.T) {
	if w := Width("hello"); w != 5 {
		t.Fatalf("Width(\"hello\") = %d, want 5", w)
	}
}

func TestWidthCJKIsDoubleWidth(t *testing.T) {
	// Each of these three characters is a full-width CJK ideograph and
	// should measure 2 terminal columns, not 1 (len() and
	// utf8.RuneCountInString both get this wrong).
	if w := Width("示例种"); w != 6 {
		t.Fatalf("Width(\"示例种\") = %d, want 6", w)
	}
}

func TestWidthEmoji(t *testing.T) {
	// A single emoji grapheme cluster renders as one double-width cell in
	// virtually every modern terminal.
	if w := Width("🎬"); w != 2 {
		t.Fatalf("Width(\"🎬\") = %d, want 2", w)
	}
}

func TestPadPadsToWidth(t *testing.T) {
	padded := Pad("hi", 5)
	if got := Width(padded); got != 5 {
		t.Fatalf("Width(Pad(\"hi\", 5)) = %d, want 5", got)
	}

	if padded != "hi   " {
		t.Fatalf("Pad(\"hi\", 5) = %q, want \"hi   \"", padded)
	}
}

func TestPadNeverTruncates(t *testing.T) {
	if got := Pad("hello world", 3); got != "hello world" {
		t.Fatalf("Pad(\"hello world\", 3) = %q, want unchanged", got)
	}
}

func TestPadWithCJKAccountsForDoubleWidth(t *testing.T) {
	// "示例" is 4 columns wide (2 chars * 2 columns); padding to 10 should
	// add 6 spaces, not 8 (which len()-based padding would produce since
	// each rune is 3 UTF-8 bytes but 2 display columns).
	padded := Pad("示例", 10)
	if got := Width(padded); got != 10 {
		t.Fatalf("Width(Pad(\"示例\", 10)) = %d, want 10", got)
	}
}

func TestTruncateShortensAndAddsEllipsis(t *testing.T) {
	got := Truncate("hello world", 8)
	if w := Width(got); w != 8 {
		t.Fatalf("Width(Truncate(...)) = %d, want 8", w)
	}

	if got != "hello..." {
		t.Fatalf("Truncate(\"hello world\", 8) = %q, want \"hello...\"", got)
	}
}

func TestTruncateNoopWhenShortEnough(t *testing.T) {
	if got := Truncate("hi", 10); got != "hi" {
		t.Fatalf("Truncate(\"hi\", 10) = %q, want unchanged", got)
	}
}

func TestTruncateNeverSplitsAGraphemeCluster(t *testing.T) {
	// "🎬" is a single grapheme cluster occupying 2 columns; truncating to
	// a width that would bisect it must drop the whole cluster rather
	// than emit half of it.
	got := Truncate("a🎬bcdef", 4)
	if w := Width(got); w > 4 {
		t.Fatalf("Width(Truncate(...)) = %d, want <= 4", w)
	}

	for _, r := range got {
		if r == '�' {
			t.Fatalf("Truncate produced a replacement character, a split cluster: %q", got)
		}
	}
}

// TestWrapEveryLineFitsAndContentSurvives is Wrap's core contract: unlike
// Truncate, nothing is ever dropped — joining every returned line
// reproduces every word of the input — and every line fits within the
// requested width.
func TestWrapEveryLineFitsAndContentSurvives(t *testing.T) {
	text := strings.Repeat("word ", 20) + "tail"

	lines := Wrap(text, 20)
	if len(lines) < 2 {
		t.Fatalf("Wrap produced %d line(s), want more than one for text this long", len(lines))
	}

	for _, line := range lines {
		if w := Width(line); w > 20 {
			t.Errorf("Wrap line %q is %d columns wide, want <= 20", line, w)
		}
	}

	joined := strings.Join(lines, " ")
	for _, word := range strings.Fields(text) {
		if !strings.Contains(joined, word) {
			t.Errorf("Wrap dropped word %q; joined output = %q", word, joined)
		}
	}
}

func TestWrapNoopWhenShortEnough(t *testing.T) {
	got := Wrap("hi there", 80)
	if len(got) != 1 || got[0] != "hi there" {
		t.Fatalf("Wrap(\"hi there\", 80) = %v, want [\"hi there\"]", got)
	}
}

// TestWrapHardBreaksAnUnbrokenTokenLongerThanWidth is the URL case: a
// single "word" (no whitespace) longer than w has no natural break point,
// so Wrap must hard-break it at the width itself rather than either
// overflowing the line or dropping the tail (which Truncate would do).
func TestWrapHardBreaksAnUnbrokenTokenLongerThanWidth(t *testing.T) {
	url := "https://example.org/" + strings.Repeat("a", 100)

	lines := Wrap(url, 30)
	if len(lines) < 4 {
		t.Fatalf("Wrap produced %d line(s) for a %d-column token at width 30, want several", len(lines), Width(url))
	}

	for _, line := range lines {
		if w := Width(line); w > 30 {
			t.Errorf("Wrap line %q is %d columns wide, want <= 30", line, w)
		}
	}

	if joined := strings.Join(lines, ""); joined != url {
		t.Fatalf("Wrap(%q, 30) joined = %q, want the original token reproduced exactly", url, joined)
	}
}

// TestWrapNeverSplitsAGraphemeCluster mirrors
// TestTruncateNeverSplitsAGraphemeCluster for the hard-break path.
func TestWrapNeverSplitsAGraphemeCluster(t *testing.T) {
	run := strings.Repeat("🎬", 10) // a single unbroken word, 20 columns wide

	lines := Wrap(run, 5)
	for _, line := range lines {
		for _, r := range line {
			if r == '�' {
				t.Fatalf("Wrap produced a replacement character, a split cluster: %q", line)
			}
		}
	}

	if joined := strings.Join(lines, ""); joined != run {
		t.Fatalf("Wrap(%q, 5) joined = %q, want the original text reproduced exactly", run, joined)
	}
}

func TestWrapNonPositiveWidthReturnsUnwrapped(t *testing.T) {
	got := Wrap("hello world", 0)
	if len(got) != 1 || got[0] != "hello world" {
		t.Fatalf("Wrap(..., 0) = %v, want the input unwrapped", got)
	}
}

func TestWrapEmptyStringReturnsOneEmptyLine(t *testing.T) {
	got := Wrap("", 10)
	if len(got) != 1 || got[0] != "" {
		t.Fatalf("Wrap(\"\", 10) = %v, want one empty line", got)
	}
}
