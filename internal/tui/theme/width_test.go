package theme

import "testing"

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
