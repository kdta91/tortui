// Package theme is tortui's terminal-capability and styling layer: colour
// degradation (truecolor → 256 → 16 → none), NO_COLOR/CLICOLOR_FORCE
// handling, Unicode/ASCII glyph selection, and grapheme-aware width
// measurement (AGENT.md §7, §14). It has no dependency on bubbletea, the
// indexer, or the engine — it is pure styling and detection, safe for any
// screen package to import.
package theme

import "sort"

// Palette is the fixed set of semantic colours every theme supplies: a
// single accent plus foreground, muted, dim, error, and success. AGENT.md
// §7 pins this list exactly — "One accent colour ... No second accent" —
// so Palette deliberately has no room for a theme to add one.
type Palette struct {
	// Accent is the single colour used to draw attention: the focused
	// pane, the selected row, primary emphasis. Never a second accent.
	Accent string
	// Foreground is the default text colour.
	Foreground string
	// Muted is secondary text: labels, less important columns.
	Muted string
	// Dim is the least prominent text: disabled state, decorative rules.
	Dim string
	// Error marks failed sources, errored torrents, and validation
	// problems.
	Error string
	// Success marks completed downloads and passed validation.
	Success string
}

// DefaultThemeName is the built-in theme used when config.toml sets no
// theme, or sets one Lookup does not recognise.
const DefaultThemeName = "default"

// builtinPalettes are tortui's shipped themes (T-050: "at least two
// built-in themes selectable from config"). Colours are truecolor hex so
// New can exercise real profile-based degradation rather than starting
// from an already-quantised value.
var builtinPalettes = map[string]Palette{
	DefaultThemeName: {
		Accent:     "#5FD7FF",
		Foreground: "#E4E4E4",
		Muted:      "#9B9B9B",
		Dim:        "#5A5A5A",
		Error:      "#FF6B6B",
		Success:    "#6BCB77",
	},
	"dusk": {
		Accent:     "#FFB86B",
		Foreground: "#EDEDED",
		Muted:      "#A39C9C",
		Dim:        "#5F5757",
		Error:      "#FF5C7A",
		Success:    "#8FD97A",
	},
}

// Names returns the built-in theme names in sorted order, for the
// settings screen's theme picker (T-054) and help text.
func Names() []string {
	names := make([]string, 0, len(builtinPalettes))
	for name := range builtinPalettes {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// Lookup returns the named built-in palette. ok is false for a name that
// is not one of Names(), which lets a caller that needs to reject a bad
// config value (T-054's settings validation) do so explicitly instead of
// silently substituting the default.
func Lookup(name string) (Palette, bool) {
	p, ok := builtinPalettes[name]
	return p, ok
}
