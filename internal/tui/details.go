// Details screen (T-063): a single search result's full information — the
// AGENT.md §7 "details" screen purpose — reached from the results screen's
// `d` key (ActionDetails, root.go). This file owns everything
// ScreenDetails-specific: its own state (detailsModel), the `u`
// open-source-in-browser action, and the basic engine.Add path enter drives
// from here; root.go only routes key presses and messages into it, the
// same split search.go and results.go already established for their own
// screens.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// detailsModel is ScreenDetails' own state: whichever indexer.Result the
// results screen's `d` key most recently selected. hasResult distinguishes
// "nothing selected yet" (the zero value, e.g. jumping straight to the
// details tab via "3" before ever visiting results) from a genuine, if
// sparsely-populated, Result — a bare zero Result is indistinguishable from
// that case by its fields alone, since every one of them is legitimately
// allowed to be empty (indexer.Result's own doc comment).
type detailsModel struct {
	hasResult bool
	result    indexer.Result
}

// newDetailsModel returns a detailsModel with nothing selected.
func newDetailsModel() detailsModel {
	return detailsModel{}
}

// withResult returns a copy of m showing r.
func (m detailsModel) withResult(r indexer.Result) detailsModel {
	m.hasResult = true
	m.result = r

	return m
}

// handleOpenDetails implements the `d` key (ActionDetails) on the results
// screen: switch to the details screen showing whichever row is currently
// selected in the results table, resolved back to its full indexer.Result
// via m.lastResults — the table itself only carries the row's already
// -rendered cells (components.Row), not the Result it came from. A no-op
// when nothing is selected (no rows in the table).
func (m Model) handleOpenDetails() (tea.Model, tea.Cmd) {
	id := m.results.table.SelectedID()
	if id == "" {
		return m, nil
	}

	for _, r := range m.lastResults {
		if resultRowID(r) == id {
			m.details = m.details.withResult(r)
			m.screen = ScreenDetails

			return m, nil
		}
	}

	return m, nil
}

// addResultMsg carries handleAddFromDetails' engine.Add outcome back into
// Update (root.go, handleAddResult). name is the result's title, captured
// at dispatch time for the confirmation message — the engine itself has no
// notion of "title" until its own metadata arrives, and until then a
// torrent is only ever addressable by the id this message also carries.
type addResultMsg struct {
	id   string
	name string
	err  error
}

// addTorrentCmd returns the tea.Cmd that actually calls eng.Add — off
// Update's own goroutine, per AGENT.md §6.1. Engine.Add itself "returns
// promptly" per its own doc contract (metadata fetch and the download
// happen asynchronously), so this resolves quickly even though it still
// goes through the same async-Cmd path as every other engine call in this
// package.
func addTorrentCmd(eng engine.Engine, src engine.AddSource, name string) tea.Cmd {
	return func() tea.Msg {
		id, err := eng.Add(context.Background(), src)
		return addResultMsg{id: id, name: name, err: err}
	}
}

// handleAddFromDetails implements enter (ActionSelect) on the details
// screen — T-063 acceptance: "enter adds the torrent and switches to the
// downloads screen." Only Magnet/TorrentURL are threaded through; Resolve,
// duplicate-infohash detection, Origin, and destination selection are
// T-070's job (it depends on this task and builds all four on top of this
// same enter key), so this is deliberately the smallest add path T-063's
// own acceptance text asks for, not a preview of T-070's.
func (m Model) handleAddFromDetails() (tea.Model, tea.Cmd) {
	if !m.details.hasResult {
		return m, nil
	}

	if m.eng == nil {
		var cmd tea.Cmd
		m.statusBar, cmd = m.statusBar.Push("no engine configured")

		return m, cmd
	}

	r := m.details.result
	if err := r.Validate(); err != nil {
		var cmd tea.Cmd
		m.statusBar, cmd = m.statusBar.Push(fmt.Sprintf("can't add: %v", err))

		return m, cmd
	}

	src := engine.AddSource{Magnet: r.Magnet, TorrentURL: r.TorrentURL}

	return m, addTorrentCmd(m.eng, src, r.Title)
}

// handleAddResult applies addTorrentCmd's outcome (root.go's Update,
// addResultMsg case). A failed Add is reported in the status bar without
// leaving the details screen — switching to an empty downloads screen on a
// failed add would be a worse outcome than staying put with a readable
// error. A successful Add switches to the downloads screen (the acceptance
// text's "switches to the downloads screen") and pushes a confirmation
// naming what was added.
func (m Model) handleAddResult(msg addResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		var cmd tea.Cmd
		m.statusBar, cmd = m.statusBar.Push(fmt.Sprintf("couldn't add torrent: %v", msg.err))

		return m, cmd
	}

	m.screen = ScreenDownloads

	var cmd tea.Cmd
	m.statusBar, cmd = m.statusBar.Push("added " + msg.name)

	return m, cmd
}

// openSourceCmd returns the tea.Cmd that calls open(rawURL) and reports any
// failure via the status bar's transient-message queue (root.go's
// transientMessageMsg), the same pattern every other fallible action in
// this package already uses. Returning a nil tea.Msg on success is a
// deliberate no-op: there is nothing more to report than what already
// happened (a browser window opened outside this process).
func openSourceCmd(open openURLFunc, rawURL string) tea.Cmd {
	return func() tea.Msg {
		if err := open(rawURL); err != nil {
			return transientMessageMsg{text: fmt.Sprintf("couldn't open source page: %v", err)}
		}

		return nil
	}
}

// handleOpenSource implements `u` (ActionOpenSource) on the details screen
// — T-063 acceptance: "u opens the source page in the system browser via
// internal/platform." The actual OS call happens off Update's own goroutine
// through m.openURL (platform.OpenURL by default, overridable via
// WithOpenURL — root.go), since shelling out to open/xdg-open/rundll32 is
// exactly the I/O AGENT.md §6.1 forbids inside Update itself.
func (m Model) handleOpenSource() (tea.Model, tea.Cmd) {
	if !m.details.hasResult {
		return m, nil
	}

	url := strings.TrimSpace(m.details.result.SourceURL)
	if url == "" {
		var cmd tea.Cmd
		m.statusBar, cmd = m.statusBar.Push("no source page for this result")

		return m, cmd
	}

	return m, openSourceCmd(m.openURL, url)
}

// detailsFiles reads r's file list back out of indexer.ExtraKeyFiles: one
// path per line, blank lines dropped, leading/trailing whitespace trimmed
// off each. nil (not an empty, non-nil slice) when the key is absent or
// blank, so renderDetailsScreen's "not available from this source" check
// (len(files) == 0) reads the same for "the adapter never set this" and
// "the adapter set it to nothing" — both mean the source publishes no file
// list for this result.
func detailsFiles(r indexer.Result) []string {
	raw := r.Extra[indexer.ExtraKeyFiles]
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	lines := strings.Split(raw, "\n")
	files := make([]string, 0, len(lines))

	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			files = append(files, l)
		}
	}

	return files
}

// formatPublishedDate renders r.Published as an absolute date (unlike
// results.go's formatAge, which renders every date on that screen relative
// to now — the details screen's single result has room for the real date,
// and "3h" tells you nothing you didn't already know from the results
// table). The zero time (no Published date published by the source) reads
// as "unknown" rather than a nonsensical 0001-01-01.
func formatPublishedDate(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}

	return t.Format("2006-01-02 15:04")
}

// detailsOrDash renders s, or "-" when it is empty or only whitespace — the
// details screen's convention for "this field is legitimately unset"
// (indexer.Result's own doc comment: "a source that does not publish a
// field leaves it at its zero value rather than guessing").
func detailsOrDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}

	return s
}

// detailsInfoHashText renders r.InfoHash, or a reason it's empty:
// indexer.Result's own doc comment says InfoHash "may be empty until
// Resolve" — a search result reaching this screen has generally not been
// resolved yet (that happens on add, T-070), so this is the expected state
// far more often than a real gap.
func detailsInfoHashText(hash string) string {
	if strings.TrimSpace(hash) == "" {
		return "not yet resolved"
	}

	return hash
}

// detailsTrustText renders r.Trust as the word form (Trust.String(),
// capitalised) rather than the results table's compact Badge() — a detail
// view has room to say "Trusted" instead of "TR", and unlike Badge(),
// String() actually distinguishes TrustUnknown ("unknown") from TrustNone
// ("none") instead of rendering both as the identical blank badge.
func detailsTrustText(t indexer.Trust) string {
	if t == indexer.TrustVIP {
		return "VIP"
	}

	s := t.String()
	if s == "" {
		return s
	}

	return strings.ToUpper(s[:1]) + s[1:]
}

// detailsFieldLine renders one "Label: value" line, label muted and value
// in the theme's foreground style — the same two-tone convention
// renderErrorDetail (root.go) already uses for its own label/value pairs.
// It is for a field whose value has a known, bounded shape (a size, a
// category token, a date) — never for unbounded, attacker-influenced text
// like a title or a URL, which writeWrappedTitle/writeWrappedField exist
// for instead (PR #42 review: this file used to run the *whole* rendered
// body through truncateLines, silently dropping the tail of a title or
// source URL wider than the terminal — AC1, "full title").
func detailsFieldLine(th theme.Theme, label, value string) string {
	return th.Muted.Render(label+": ") + th.Foreground.Render(value)
}

// writeWrappedTitle writes title to b, word-wrapped to width columns
// (theme.Wrap) with style applied per line — unlike detailsFieldLine's
// fixed-shape fields, a title is attacker-influenced text of unbounded
// length (AGENT.md's own note on Result.Title: "measure it with
// rivo/uniseg and never with len") that must stay fully visible rather
// than losing its tail to Truncate. Wrapping the *plain* text first, then
// styling each already-measured line, avoids measuring ANSI escape bytes
// as if they were display columns — the same order renderResultsScreen's
// header already uses (Truncate before Render, never after).
func writeWrappedTitle(b *strings.Builder, style lipgloss.Style, title string, width int) {
	for i, line := range theme.Wrap(title, width) {
		if i > 0 {
			b.WriteString("\n")
		}

		b.WriteString(style.Render(line))
	}
}

// writeWrappedField writes label as its own muted heading line, then value
// word-wrapped (theme.Wrap) across one or more two-space-indented lines
// underneath — the details screen's convention for an unbounded,
// attacker-influenced value (today: the source URL) that must never lose
// content to Truncate, unlike detailsFieldLine's inline fixed-shape
// fields. width is the full screen width; the wrap target is width-2 to
// leave room for the indent, matching the file list's own two-space
// indent below.
func writeWrappedField(b *strings.Builder, th theme.Theme, label, value string, width int) {
	b.WriteString(th.Muted.Render(label + ":"))

	for _, line := range theme.Wrap(value, width-2) {
		b.WriteString("\n")
		b.WriteString(th.Foreground.Render("  " + line))
	}
}

// renderDetailsScreen draws ScreenDetails' real body: every field T-063's
// acceptance text names (full title, size, category, uploader, trust,
// published date, source URL, infohash) plus the file list or its "not
// available" fallback. Pure: reads m and returns a string, no I/O, no
// mutation (AGENT.md §6.8).
func (m Model) renderDetailsScreen() string {
	th := m.theme

	if !m.details.hasResult {
		return theme.Truncate(
			th.Muted.Render("No result selected — press d on the results screen to view one."),
			m.width,
		)
	}

	r := m.details.result

	var b strings.Builder

	writeWrappedTitle(&b, th.Accent, r.Title, m.width)
	b.WriteString("\n\n")
	b.WriteString(detailsFieldLine(th, "Size", formatSize(r.SizeBytes)))
	b.WriteString("\n")
	b.WriteString(detailsFieldLine(th, "Category", r.Category.String()))
	b.WriteString("\n")
	b.WriteString(detailsFieldLine(th, "Trust", detailsTrustText(r.Trust)))
	b.WriteString("\n")
	b.WriteString(detailsFieldLine(th, "Uploader", detailsOrDash(r.Uploader)))
	b.WriteString("\n")
	b.WriteString(detailsFieldLine(th, "Published", formatPublishedDate(r.Published)))
	b.WriteString("\n")
	writeWrappedField(&b, th, "Source", detailsOrDash(r.SourceURL), m.width)
	b.WriteString("\n")
	b.WriteString(detailsFieldLine(th, "Infohash", detailsInfoHashText(r.InfoHash)))
	b.WriteString("\n\n")
	b.WriteString(th.Muted.Render("Files"))
	b.WriteString("\n")

	files := detailsFiles(r)
	if len(files) == 0 {
		b.WriteString(th.Muted.Render("  not available from this source"))
	} else {
		for i, f := range files {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(th.Foreground.Render("  " + f))
		}
	}

	return truncateLines(b.String(), m.width)
}
