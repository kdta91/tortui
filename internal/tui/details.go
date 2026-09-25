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
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/store"
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

// addResultMsg carries addTorrentCmd's engine.Add outcome back into Update
// (root.go, handleAddResult). name is the result's title, captured at
// dispatch time for the confirmation message — the engine itself has no
// notion of "title" until its own metadata arrives, and until then a
// torrent is only ever addressable by the id this message also carries.
// indexerID/sourceURL/savePath/magnet/torrentURL are exactly what
// handleAddResult needs to persist a store.TorrentRecord (T-070 acceptance:
// "Origin populated ... persisted via the store") without handleAddResult
// having to reach back into whatever Result produced them.
type addResultMsg struct {
	id         string
	name       string
	err        error
	indexerID  string
	sourceURL  string
	savePath   string
	magnet     string
	torrentURL string
}

// addTorrentCmd returns the tea.Cmd that actually calls eng.Add — off
// Update's own goroutine, per AGENT.md §6.1. Engine.Add itself "returns
// promptly" per its own doc contract (metadata fetch and the download
// happen asynchronously), so this resolves quickly even though it still
// goes through the same async-Cmd path as every other engine call in this
// package.
func addTorrentCmd(eng engine.Engine, src engine.AddSource, name, indexerID, sourceURL string) tea.Cmd {
	return func() tea.Msg {
		id, err := eng.Add(context.Background(), src)
		return addResultMsg{
			id: id, name: name, err: err,
			indexerID: indexerID, sourceURL: sourceURL, savePath: src.SavePath,
			magnet: src.Magnet, torrentURL: src.TorrentURL,
		}
	}
}

// resolveResultMsg carries resolveCmd's indexer.Indexer.Resolve outcome back
// into Update (root.go, handleResolveResult). result is r unchanged on
// failure, so a caller that only inspects it on the success path never sees
// anything but the resolved value.
type resolveResultMsg struct {
	result indexer.Result
	err    error
}

// resolveCmd returns the tea.Cmd that calls ix.Resolve — off Update's own
// goroutine, per AGENT.md §6.1, since a real adapter's Resolve makes a
// network request.
func resolveCmd(ix indexer.Indexer, r indexer.Result) tea.Cmd {
	return func() tea.Msg {
		resolved, err := ix.Resolve(context.Background(), r)
		if err != nil {
			return resolveResultMsg{result: r, err: fmt.Errorf("resolve %q: %w", r.Title, err)}
		}

		return resolveResultMsg{result: resolved}
	}
}

// handleAddFromDetails implements enter (ActionSelect) on the details
// screen — T-063 acceptance: "enter adds the torrent and switches to the
// downloads screen," now via startAdd's full T-070 flow (Resolve, dedup,
// Origin, destination).
func (m Model) handleAddFromDetails() (tea.Model, tea.Cmd) {
	if !m.details.hasResult {
		return m, nil
	}

	return m.startAdd(m.details.result)
}

// handleAddFromResults implements enter (ActionSelect) on the results
// screen (AGENT.md §7: "enter | Add torrent (results)"). It resolves the
// selected table row back to the indexer.Result that produced it — the
// table itself only carries the row's already-rendered cells
// (components.Row) — the same lookup handleOpenDetails uses for the `d`
// key, then runs the same startAdd flow the details screen's enter key
// does. A no-op when nothing is selected.
func (m Model) handleAddFromResults() (tea.Model, tea.Cmd) {
	id := m.results.table.SelectedID()
	if id == "" {
		return m, nil
	}

	for _, r := range m.lastResults {
		if resultRowID(r) == id {
			return m.startAdd(r)
		}
	}

	return m, nil
}

// startAdd is the shared implementation behind enter's "add torrent"
// meaning on both the details and results screens (T-070): a duplicate
// infohash already tracked by the engine selects that existing entry
// instead of adding a second copy; otherwise a Result missing a magnet is
// resolved first (calling the originating source's own Resolve), and only
// then handed to finishAdd.
func (m Model) startAdd(r indexer.Result) (tea.Model, tea.Cmd) {
	if m.eng == nil {
		var cmd tea.Cmd
		m.statusBar, cmd = m.statusBar.Push("no engine configured")

		return m, cmd
	}

	if id, ok := m.duplicateTorrentID(r.InfoHash); ok {
		return m.selectExistingDownload(id, r.Title)
	}

	if strings.TrimSpace(r.Magnet) == "" {
		ix, ok := m.lookupIndexer(r.IndexerID)
		if !ok {
			var cmd tea.Cmd
			m.statusBar, cmd = m.statusBar.Push(fmt.Sprintf("can't add %q: no source available to resolve it", r.Title))

			return m, cmd
		}

		return m, resolveCmd(ix, r)
	}

	return m.finishAdd(r)
}

// handleResolveResult applies resolveCmd's outcome (root.go's Update,
// resolveResultMsg case): a failed Resolve is reported in the status bar
// without leaving the current screen or ever calling engine.Add (T-070
// acceptance: "failures surface as a status-bar error, not a crash"); a
// successful one continues into finishAdd with the now-resolved Result.
func (m Model) handleResolveResult(msg resolveResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		var cmd tea.Cmd
		m.statusBar, cmd = m.statusBar.Push(fmt.Sprintf("couldn't add: %v", msg.err))

		return m, cmd
	}

	return m.finishAdd(msg.result)
}

// finishAdd validates r (Resolve may have left it without a usable link),
// re-checks for a duplicate infohash (Resolve is exactly the step that most
// often discovers one), and dispatches the actual engine.Add with the
// resolved destination (T-070: "the resolved absolute path goes into
// AddSource.SavePath" — T-074 replaces resolveSavePath's default-only
// answer with an interactive picker on top of this same flow).
func (m Model) finishAdd(r indexer.Result) (tea.Model, tea.Cmd) {
	if id, ok := m.duplicateTorrentID(r.InfoHash); ok {
		return m.selectExistingDownload(id, r.Title)
	}

	if err := r.Validate(); err != nil {
		var cmd tea.Cmd
		m.statusBar, cmd = m.statusBar.Push(fmt.Sprintf("can't add: %v", err))

		return m, cmd
	}

	src := engine.AddSource{
		Magnet:     r.Magnet,
		TorrentURL: r.TorrentURL,
		SavePath:   m.resolveSavePath(),
	}

	return m, addTorrentCmd(m.eng, src, r.Title, r.IndexerID, r.SourceURL)
}

// resolveSavePath is T-070's own answer to "where does this torrent's data
// go": the configured default download directory, cleaned, or "" (the
// engine's own configured-default fallback, per AddSource.SavePath's doc)
// when none was wired in via WithDownloadDir. T-074 replaces this with a
// per-torrent destination the user actually chose, without this flow's
// callers (startAdd/finishAdd) needing to change.
func (m Model) resolveSavePath() string {
	if strings.TrimSpace(m.downloadDir) == "" {
		return ""
	}

	return filepath.Clean(m.downloadDir)
}

// lookupIndexer finds the indexer.Indexer that produced a Result, by the
// IndexerID it carries, via the same Searcher the search screen dispatches
// against (search.go). false when no searcher is wired, or the id names no
// registered source — a source that existed at search time but has since
// been removed, for instance.
func (m Model) lookupIndexer(id string) (indexer.Indexer, bool) {
	if m.searcher == nil {
		return nil, false
	}

	return m.searcher.Get(id)
}

// duplicateTorrentID reports the engine ID of an already-tracked torrent
// whose InfoHash matches infoHash, case-insensitively (hex infohashes from
// different sources are not guaranteed the same case). An empty infoHash
// never matches — most Results reach here unresolved, and treating an
// unknown infohash as "duplicate of everything" would be wrong, not
// conservative.
func (m Model) duplicateTorrentID(infoHash string) (string, bool) {
	infoHash = strings.TrimSpace(infoHash)
	if infoHash == "" || m.eng == nil {
		return "", false
	}

	for _, s := range m.eng.List() {
		if s.InfoHash != "" && strings.EqualFold(s.InfoHash, infoHash) {
			return s.ID, true
		}
	}

	return "", false
}

// selectExistingDownload implements T-070's "duplicate infohash ... selects
// the existing row instead of adding twice": switch to the downloads
// screen and point the root's generic selection cursor (root.go's
// Model.selection — "a later screen is free to replace it with its own
// bounded, data-backed cursor") at the matching torrent, in the same
// sorted-by-ID order downloadIndexOf defines, rather than calling
// engine.Add a second time.
func (m Model) selectExistingDownload(id, title string) (tea.Model, tea.Cmd) {
	m.screen = ScreenDownloads
	m.selection = m.downloadIndexOf(id)

	var cmd tea.Cmd
	m.statusBar, cmd = m.statusBar.Push("already downloading: " + title)

	return m, cmd
}

// downloadIndexOf returns id's position in m.eng.List() sorted by ID — a
// fixed, deterministic order good enough for selectExistingDownload's
// stopgap cursor ahead of T-071's real, data-backed downloads table. 0 when
// id is not found (never expected: the caller just read it off the same
// List()).
func (m Model) downloadIndexOf(id string) int {
	statuses := m.eng.List()
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].ID < statuses[j].ID })

	for i, s := range statuses {
		if s.ID == id {
			return i
		}
	}

	return 0
}

// handleAddResult applies addTorrentCmd's outcome (root.go's Update,
// addResultMsg case). A failed Add is reported in the status bar without
// leaving the current screen — switching to an empty downloads screen on a
// failed add would be a worse outcome than staying put with a readable
// error. A successful Add persists a store.TorrentRecord when a
// TorrentStore is wired (T-070 acceptance: "Origin populated ... persisted
// via the store" — a persistence failure is reported but never blocks the
// add itself, which already succeeded), switches to the downloads screen,
// and pushes a confirmation naming what was added.
func (m Model) handleAddResult(msg addResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		var cmd tea.Cmd
		m.statusBar, cmd = m.statusBar.Push(fmt.Sprintf("couldn't add torrent: %v", msg.err))

		return m, cmd
	}

	var persistErrCmd tea.Cmd

	if m.torrentStore != nil {
		rec := store.TorrentRecord{
			ID:         msg.id,
			IndexerID:  msg.indexerID,
			SourceURL:  msg.sourceURL,
			AddedAt:    time.Now(),
			SavePath:   msg.savePath,
			Name:       msg.name,
			Magnet:     msg.magnet,
			TorrentURL: msg.torrentURL,
		}

		if err := m.torrentStore.SetTorrent(rec); err != nil {
			m.statusBar, persistErrCmd = m.statusBar.Push(fmt.Sprintf("couldn't save torrent record: %v", err))
		}
	}

	m.screen = ScreenDownloads

	var cmd tea.Cmd
	m.statusBar, cmd = m.statusBar.Push("added " + msg.name)

	return m, tea.Batch(persistErrCmd, cmd)
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
