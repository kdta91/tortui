package theme

// GlyphSet is the pair of visual vocabularies tortui draws with: full
// Unicode block/check glyphs, and a plain-ASCII fallback for terminals
// that cannot render them (AGENT.md §14). Any code that hardcodes █, ░, or
// ✓ directly instead of going through a GlyphSet cannot degrade and is a
// bug.
type GlyphSet struct {
	// Full is a filled progress-bar cell.
	Full string
	// Empty is an unfilled progress-bar cell.
	Empty string
	// Check is the trust/boolean affirmative marker (AGENT.md §7's Trust
	// badge uses this for TrustVerified).
	Check string
}

// UnicodeGlyphs is the default glyph set: block characters and a check
// mark.
var UnicodeGlyphs = GlyphSet{Full: "█", Empty: "░", Check: "✓"}

// ASCIIGlyphs is the fallback glyph set for terminals or users that can't
// or don't want Unicode.
var ASCIIGlyphs = GlyphSet{Full: "#", Empty: "-", Check: "+"}

// Glyphs picks the glyph set for a detected Capability. ForceASCII already
// folds into Capability.Unicode inside Detect, so this is a plain lookup.
func Glyphs(capability Capability) GlyphSet {
	if capability.Unicode {
		return UnicodeGlyphs
	}

	return ASCIIGlyphs
}
