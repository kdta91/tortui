package tui

import (
	"strings"

	"github.com/kdta91/tortui/internal/tui/theme"
)

// LegalNotice is the AGENT.md §2 legal-notice line: "ship a legal-notice
// line in README.md and in the first-run screen." Both surfaces read from
// this one string so they can never drift apart — README.md quotes it
// verbatim in its own "Legal notice" section (T-090).
const LegalNotice = "tortui is a BitTorrent client and search front-end. It does not host, " +
	"index, or distribute any content. You are responsible for what you search for and " +
	"download, and for complying with the terms of service of any indexer you configure " +
	"and with the copyright law of your jurisdiction."

// firstRunSourcesNotice explains what ships in the binary. tortui bundles a
// small set of unambiguously lawful default sources (AGENT.md §2, §16,
// docs/bundled-sources.md) — public archives and dataset repositories that
// officially distribute their own content over BitTorrent — enabled with no
// setup step. Anything beyond that is user-supplied: there is no other
// preconfigured list, so most users will want to add their own from
// Settings.
const firstRunSourcesNotice = "A couple of lawful default sources (public archives, research " +
	"datasets) are enabled out of the box, so search works immediately. Beyond those, tortui " +
	"ships no other endpoints or presets — add your own indexer from Settings (press 5, then " +
	"a) or by editing config.toml. See README.md for how."

// WithFirstRun marks the Model to open on ContextFirstRun instead of
// ScreenSearch: a one-time welcome overlay carrying the sources notice
// above and LegalNotice, dismissed by any key. The composition root
// (Backlog T-950 — no such root exists yet, see T-090's notes) is expected
// to pass this only when internal/config.LoadResult.FirstRun is true, i.e.
// exactly the run that just wrote a default config; every later launch
// omits it and starts on ScreenSearch as before.
func WithFirstRun(b bool) Option {
	return func(m *Model) { m.firstRun = b }
}

// renderFirstRun draws the welcome overlay: the bundled-sources notice, the
// legal notice, and a footer telling the user any key continues. Lines wrap
// to m.width via theme.Wrap so it stays legible at 80x24 (AGENT.md §7).
func (m Model) renderFirstRun() string {
	var b strings.Builder

	b.WriteString(m.theme.Accent.Render("Welcome to tortui"))
	b.WriteString("\n\n")

	for _, line := range theme.Wrap(firstRunSourcesNotice, m.width) {
		b.WriteString(m.theme.Foreground.Render(line))
		b.WriteString("\n")
	}

	b.WriteString("\n")

	for _, line := range theme.Wrap(LegalNotice, m.width) {
		b.WriteString(m.theme.Muted.Render(line))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(m.theme.Dim.Render("press any key to continue"))

	return b.String()
}
