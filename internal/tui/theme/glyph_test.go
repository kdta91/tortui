package theme

import "testing"

func TestGlyphsUnicodeVsASCII(t *testing.T) {
	unicode := Glyphs(Capability{Unicode: true})
	if unicode != UnicodeGlyphs {
		t.Fatalf("Glyphs(Unicode: true) = %+v, want UnicodeGlyphs", unicode)
	}

	ascii := Glyphs(Capability{Unicode: false})
	if ascii != ASCIIGlyphs {
		t.Fatalf("Glyphs(Unicode: false) = %+v, want ASCIIGlyphs", ascii)
	}
}

func TestGlyphSetsAreSingleCellEach(t *testing.T) {
	for name, set := range map[string]GlyphSet{"unicode": UnicodeGlyphs, "ascii": ASCIIGlyphs} {
		for field, glyph := range map[string]string{"Full": set.Full, "Empty": set.Empty, "Check": set.Check} {
			if w := Width(glyph); w != 1 {
				t.Errorf("%s.%s = %q has width %d, want 1", name, field, glyph, w)
			}
		}
	}
}
