package theme

import (
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// ColorLevel is the degraded set of colour depths a terminal may render,
// ordered richest to none (AGENT.md §14: "Truecolor → 256 → 16
// degradation via lipgloss profile detection").
type ColorLevel int

const (
	// ColorNone means no colour at all: NO_COLOR is set, TERM=dumb, or
	// stdout is not a terminal and CLICOLOR_FORCE was not set. Rendering
	// at this level must produce zero escape codes.
	ColorNone ColorLevel = iota
	// Color16 is the 4-bit ANSI palette.
	Color16
	// Color256 is the 8-bit ANSI256 palette.
	Color256
	// ColorTrue is 24-bit truecolor.
	ColorTrue
)

// String names the level for logging and the doctor command (T-055).
func (l ColorLevel) String() string {
	switch l {
	case ColorNone:
		return "none"
	case Color16:
		return "16"
	case Color256:
		return "256"
	case ColorTrue:
		return "truecolor"
	default:
		return "unknown"
	}
}

// Capability is what tortui detected about the terminal it is about to
// draw into: how much colour to use, which glyph set fits, and whether a
// TUI can run here at all.
type Capability struct {
	// Color is the usable colour depth.
	Color ColorLevel
	// Unicode is true when block glyphs (█░) and the check mark (✓) are
	// safe to draw; false selects the ASCII fallback (AGENT.md §14).
	Unicode bool
	// Interactive is false when the TUI must not start at all: TERM=dumb
	// or stdout is not a TTY. Callers (main.go, internal/tui's root
	// command — T-051) are responsible for checking this before entering
	// bubbletea's alternate screen and for printing RefusalMessage and
	// exiting 1 when it is false.
	Interactive bool
}

// DetectOptions lets a caller steer Detect explicitly, which is what makes
// it testable without mutating process-global environment variables or
// requiring a real terminal.
type DetectOptions struct {
	// Out is the stream capability is measured against — typically
	// os.Stdout. A nil Out defaults to os.Stdout. Only an *os.File can
	// ever be judged a TTY; anything else (a buffer, a pipe used as an
	// io.Writer) is treated as non-interactive, matching "piping the
	// binary must not produce escape-sequence garbage" (AGENT.md §14).
	Out io.Writer
	// Environ overrides the process environment for TERM, NO_COLOR,
	// CLICOLOR_FORCE, COLORTERM, and locale lookups. A nil Environ reads
	// the real process environment via os.Getenv/os.Environ.
	Environ termenv.Environ
	// ForceASCII mirrors --ascii / ascii = true in config.toml: it always
	// wins over detected Unicode capability.
	ForceASCII bool
	// AssumeTTY, when true, tells colour-profile detection to treat Out
	// as an interactive terminal regardless of what isCharDevice(Out)
	// reports. It exists so tests can exercise TERM/COLORTERM-driven
	// colour degradation (truecolor → 256 → 16 → none) without a real
	// terminal attached; production callers leave it false and let
	// Capability.Interactive's real stream check stand. It never affects
	// Capability.Interactive itself.
	AssumeTTY bool
}

// realEnviron reads the actual process environment. It is the default
// used by Detect and satisfies termenv.Environ so lipgloss's profile
// detection reads the same variables it would without DetectOptions.
type realEnviron struct{}

func (realEnviron) Getenv(key string) string { return os.Getenv(key) }
func (realEnviron) Environ() []string        { return os.Environ() }

// Detect inspects the environment and stream in opts and reports what
// tortui can safely render. It never itself prints anything or exits —
// see Capability.Interactive and RefusalMessage.
func Detect(opts DetectOptions) Capability {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}

	environ := opts.Environ
	if environ == nil {
		environ = realEnviron{}
	}

	termenvOpts := []termenv.OutputOption{termenv.WithEnvironment(environ)}
	if opts.AssumeTTY {
		termenvOpts = append(termenvOpts, termenv.WithTTY(true))
	}

	renderer := lipgloss.NewRenderer(out, termenvOpts...)

	term := environ.Getenv("TERM")
	interactive := term != "dumb" && isCharDevice(out)

	return Capability{
		Color:       colorLevelFor(renderer.ColorProfile()),
		Unicode:     !opts.ForceASCII && unicodeCapable(environ),
		Interactive: interactive,
	}
}

// RefusalMessage is the one-line explanation to print, and then exit 1,
// when Capability.Interactive is false (AGENT.md §14: "TERM=dumb or a
// non-TTY stdout → do not start the TUI. Print a one-line explanation and
// exit 1."). Printing and exiting is main.go/root's job (T-051); this
// package only supplies the wording so it is defined once.
func RefusalMessage() string {
	return "tortui: refusing to start — stdout is not an interactive terminal (TERM=dumb or output is redirected/piped)"
}

func colorLevelFor(p termenv.Profile) ColorLevel {
	switch p {
	case termenv.TrueColor:
		return ColorTrue
	case termenv.ANSI256:
		return Color256
	case termenv.ANSI:
		return Color16
	default: // termenv.Ascii, or any future/unknown value
		return ColorNone
	}
}

// unicodeCapable reports whether the environment's locale claims UTF-8.
// It checks LC_ALL, then LC_CTYPE, then LANG — the same precedence order
// as libc's locale resolution — and stops at the first one that is set,
// even if empty variables further down the list would say otherwise. When
// none of the three is set at all (common on Windows Terminal and many
// minimal containers), it assumes UTF-8 rather than degrading everyone to
// ASCII by default; --ascii/ascii=true remains the explicit override.
func unicodeCapable(environ termenv.Environ) bool {
	for _, key := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := environ.Getenv(key); v != "" {
			upper := strings.ToUpper(v)
			return strings.Contains(upper, "UTF-8") || strings.Contains(upper, "UTF8")
		}
	}

	return true
}

func isCharDevice(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}

	info, err := f.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice != 0
}
