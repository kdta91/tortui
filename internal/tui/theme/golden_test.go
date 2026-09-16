package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// updateGoldenEnv regenerates golden fixtures under testdata/ when set to
// "1". A regenerated file must be inspected before committing (AGENT.md
// §15: "A golden-file diff is a real failure — inspect the diff, do not
// regenerate blindly").
const updateGoldenEnv = "UPDATE_GOLDEN"

func compareGolden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name)

	if os.Getenv(updateGoldenEnv) == "1" {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatalf("writing golden file %s: %v", path, err)
		}

		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file %s: %v (run `%s=1 go test ./internal/tui/theme/...` to create it)", path, err, updateGoldenEnv)
	}

	if got != string(want) {
		t.Fatalf("golden mismatch for %s:\n--- want ---\n%s--- got ---\n%s", path, want, got)
	}
}

// TestGoldenNoColorZeroEscapeCodes is the acceptance criterion's golden
// test: under NO_COLOR, every semantic style in a Theme must render with
// zero ANSI escape codes — a fully monochrome render, not merely a
// degraded-color one.
func TestGoldenNoColorZeroEscapeCodes(t *testing.T) {
	environ := stubEnviron{"NO_COLOR": "1", "TERM": "xterm-256color"}
	capability := Detect(DetectOptions{Out: tempFile(t), Environ: environ})

	th := New(DefaultThemeName, capability)

	var b strings.Builder
	b.WriteString(th.Accent.Bold(true).Render("Accent") + "\n")
	b.WriteString(th.Foreground.Render("Foreground") + "\n")
	b.WriteString(th.Muted.Render("Muted") + "\n")
	b.WriteString(th.Dim.Render("Dim") + "\n")
	b.WriteString(th.Error.Render("Error") + "\n")
	b.WriteString(th.Success.Render("Success") + "\n")

	got := b.String()

	if strings.ContainsRune(got, '\x1b') {
		t.Fatalf("rendered output under NO_COLOR contains an escape code:\n%q", got)
	}

	compareGolden(t, "nocolor_render.golden", got)
}

// TestGoldenResultsRowCJKEmojiAlignment is the acceptance criterion's
// other golden test: a results row containing CJK text and an emoji must
// keep every column at its declared terminal width, so the Size and S/L
// columns start in the same place on every row regardless of how many
// bytes or runes the Title text took to get there. The "|" separators in
// the golden fixture are the visible proof — they line up vertically in
// any monospace font.
func TestGoldenResultsRowCJKEmojiAlignment(t *testing.T) {
	const (
		titleW = 22
		sizeW  = 8
		slW    = 8
	)

	type row struct{ title, size, sl string }

	rows := []row{
		{title: "示例种子🎬.iso", size: "4.70 GB", sl: "128/4"},
		{title: "debian-13.0.0-amd64-netinst.iso", size: "702 MB", sl: "3421/12"},
		{title: "sample.txt", size: "12 B", sl: "1/0"},
	}

	var b strings.Builder

	for _, r := range rows {
		title := Pad(Truncate(r.title, titleW), titleW)
		size := Pad(Truncate(r.size, sizeW), sizeW)
		sl := Pad(Truncate(r.sl, slW), slW)

		if w := Width(title); w != titleW {
			t.Fatalf("title column width = %d, want %d for %q", w, titleW, r.title)
		}

		if w := Width(size); w != sizeW {
			t.Fatalf("size column width = %d, want %d for %q", w, sizeW, r.size)
		}

		if w := Width(sl); w != slW {
			t.Fatalf("s/l column width = %d, want %d for %q", w, slW, r.sl)
		}

		line := strings.Join([]string{title, size, sl}, "|")

		if got, want := Width(line), titleW+1+sizeW+1+slW; got != want {
			t.Fatalf("row width = %d, want %d for line %q", got, want, line)
		}

		b.WriteString(line + "\n")
	}

	got := b.String()

	// The defining assertion: every row's separators sit at the same
	// byte-independent visual column, i.e. every line has identical
	// terminal width even though "示例种子🎬.iso" and
	// "debian-13.0.0-amd64-netinst.iso" are wildly different in bytes and
	// runes.
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	for i, line := range lines {
		if w := Width(line); w != titleW+1+sizeW+1+slW {
			t.Fatalf("line %d width = %d, want %d: %q", i, w, titleW+1+sizeW+1+slW, line)
		}
	}

	compareGolden(t, "results_row.golden", got)
}
