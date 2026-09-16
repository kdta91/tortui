package theme

import (
	"io"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Theme is a Palette turned into ready-to-render lipgloss styles for one
// detected Capability. Two Themes built from the same Palette but
// different Capabilities render differently — that's the whole point:
// truecolor hex in Palette degrades to 256, 16, or no colour at all
// depending on what the terminal can do, and at ColorNone no style in a
// Theme ever attaches a colour, so rendering it emits zero escape codes
// (AGENT.md T-050's NO_COLOR requirement).
type Theme struct {
	// Name is the palette name this Theme was built from (see Names,
	// Lookup). It is always one of Names(), even when the requested name
	// was not — New falls back to DefaultThemeName.
	Name string
	// Glyphs is the glyph set matching the Capability New was built with.
	Glyphs GlyphSet

	Accent     lipgloss.Style
	Foreground lipgloss.Style
	Muted      lipgloss.Style
	Dim        lipgloss.Style
	Error      lipgloss.Style
	Success    lipgloss.Style

	// Border draws the box AGENT.md §7 allows around the focused pane and
	// modals — nowhere else. It carries the palette's accent colour (never
	// a second, border-specific colour) and picks rounded Unicode corners
	// or the plain-ASCII fallback the same way GlyphSet does, so a modal
	// never emits a raw escape sequence or a hardcoded border glyph
	// outside this package (T-054).
	Border lipgloss.Style
}

// New builds a Theme from a built-in palette name and a detected
// Capability. An unrecognised name falls back to DefaultThemeName rather
// than failing — internal/config already rejects an empty theme string,
// and T-054's settings screen validates a chosen name against Names()
// before it reaches here, so New's fallback exists only to keep this
// package safe to call with an unchecked value, never as the primary way
// a bad name gets caught.
func New(name string, capability Capability) Theme {
	palette, ok := Lookup(name)
	if !ok {
		name = DefaultThemeName
		palette = builtinPalettes[DefaultThemeName]
	}

	// The renderer's writer is never used to emit anything — styles are
	// rendered later, by their own Render calls — so io.Discard is fine
	// here. What matters is the explicit colour profile: it is exactly
	// the level Detect already resolved, so no style construction below
	// re-derives capability from the environment.
	renderer := lipgloss.NewRenderer(io.Discard)
	renderer.SetColorProfile(profileFor(capability.Color))

	mono := capability.Color == ColorNone

	style := func(hex string) lipgloss.Style {
		s := renderer.NewStyle()
		if mono {
			// Never attach a colour at ColorNone, even though an Ascii
			// termenv profile would already turn any colour into a
			// no-op: a golden test asserts zero escape codes under
			// NO_COLOR, and this makes that guarantee obvious in code
			// rather than relying on a downstream library's behaviour.
			return s
		}

		return s.Foreground(lipgloss.Color(hex))
	}

	border := renderer.NewStyle().Border(borderStyle(capability.Unicode)).Padding(0, 1)
	if !mono {
		border = border.BorderForeground(lipgloss.Color(palette.Accent))
	}

	return Theme{
		Name:       name,
		Glyphs:     Glyphs(capability),
		Accent:     style(palette.Accent),
		Foreground: style(palette.Foreground),
		Muted:      style(palette.Muted),
		Dim:        style(palette.Dim),
		Error:      style(palette.Error),
		Success:    style(palette.Success),
		Border:     border,
	}
}

// borderStyle picks the border glyph set for a detected Unicode capability:
// rounded corners when safe, the plain-ASCII fallback otherwise — the same
// split GlyphSet draws for progress bars and the trust badge (AGENT.md
// §14's "Glyph fallback").
func borderStyle(unicode bool) lipgloss.Border {
	if unicode {
		return lipgloss.RoundedBorder()
	}

	return lipgloss.ASCIIBorder()
}

func profileFor(l ColorLevel) termenv.Profile {
	switch l {
	case ColorTrue:
		return termenv.TrueColor
	case Color256:
		return termenv.ANSI256
	case Color16:
		return termenv.ANSI
	default: // ColorNone
		return termenv.Ascii
	}
}
