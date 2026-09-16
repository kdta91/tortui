package theme

import (
	"strings"
	"testing"
)

func TestNewUnknownNameFallsBackToDefault(t *testing.T) {
	th := New("no-such-theme", Capability{Color: ColorTrue, Unicode: true})
	if th.Name != DefaultThemeName {
		t.Fatalf("New(\"no-such-theme\", ...).Name = %q, want %q", th.Name, DefaultThemeName)
	}
}

func TestNewSetsGlyphsFromCapability(t *testing.T) {
	th := New(DefaultThemeName, Capability{Unicode: false})
	if th.Glyphs != ASCIIGlyphs {
		t.Fatalf("Glyphs = %+v, want ASCIIGlyphs when Capability.Unicode is false", th.Glyphs)
	}

	th = New(DefaultThemeName, Capability{Unicode: true})
	if th.Glyphs != UnicodeGlyphs {
		t.Fatalf("Glyphs = %+v, want UnicodeGlyphs when Capability.Unicode is true", th.Glyphs)
	}
}

func TestNewAtColorNoneEmitsNoEscapeCodes(t *testing.T) {
	th := New(DefaultThemeName, Capability{Color: ColorNone})

	for name, style := range map[string]struct{ render func(...string) string }{
		"Accent":     {th.Accent.Render},
		"Foreground": {th.Foreground.Render},
		"Muted":      {th.Muted.Render},
		"Dim":        {th.Dim.Render},
		"Error":      {th.Error.Render},
		"Success":    {th.Success.Render},
	} {
		got := style.render("x")
		if strings.ContainsRune(got, '\x1b') {
			t.Errorf("%s.Render(\"x\") at ColorNone = %q, contains an escape code", name, got)
		}

		if got != "x" {
			t.Errorf("%s.Render(\"x\") at ColorNone = %q, want plain %q", name, got, "x")
		}
	}
}

func TestNewAtTrueColorAttachesColor(t *testing.T) {
	th := New(DefaultThemeName, Capability{Color: ColorTrue})

	got := th.Accent.Render("x")
	if !strings.ContainsRune(got, '\x1b') {
		t.Fatalf("Accent.Render(\"x\") at ColorTrue = %q, want an escape code", got)
	}
}

func TestNewDifferentThemesHaveDifferentAccents(t *testing.T) {
	names := Names()
	if len(names) < 2 {
		t.Fatalf("need at least 2 built-in themes to compare, got %v", names)
	}

	a, _ := Lookup(names[0])
	b, _ := Lookup(names[1])

	if a.Accent == b.Accent {
		t.Fatalf("themes %q and %q share the same accent colour %q", names[0], names[1], a.Accent)
	}
}
