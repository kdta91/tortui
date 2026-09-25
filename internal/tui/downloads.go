// Downloads screen (T-071): the AGENT.md §7 "downloads" screen — every
// torrent the engine is tracking, drawn in the three-line row layout §7's
// own example specifies (name; progress bar/percentage/transferred/rates/
// peers/ETA; source/destination/added-at plus the action hint line).
// root.go's engineUpdateMsg handling keeps m.torrentStatuses in sync with
// the engine's own coalesced snapshots (Updates(), never polled — AGENT.md
// §6.5) so this file's only job is turning that snapshot into rows and a
// render, the same split every other screen in this package already
// follows. download_actions.go gives the rows pause/resume, remove, and
// open-source behaviour (T-072) and open file/folder (T-073).
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// queueProvider is the narrow slice of the optional engine.Queuer
// (internal/engine/policy.go) this screen needs: the queue's real order.
// Discovered by type assertion, exactly as engine.Queuer's own doc comment
// describes — it is a capability layered on top of the frozen Engine
// contract (AGENT.md §5), not part of it. internal/engine/fake does not
// implement it ("engine/fake does not queue yet", T-034's tracker notes);
// downloadQueueReason falls back to a position derived from the snapshot
// itself when the assertion fails.
type queueProvider interface {
	Queue() []string
}

// seedPolicyProvider is the same kind of optional-capability assertion for
// the seeding policy in effect (internal/engine/anacrolix.Engine.SeedPolicy).
// internal/engine/fake does not implement it either.
type seedPolicyProvider interface {
	SeedPolicy() engine.SeedPolicy
}

// downloadsModel is ScreenDownloads' own state: the cursor over
// downloadRows() (active section first, then completed — the same order
// renderDownloadsScreen draws them in) and which errored torrent's full
// reason, if any, is currently expanded (T-071 acceptance: "errored
// torrents show the reason inline, truncated, expandable").
type downloadsModel struct {
	cursor int
	// expandedErr is the ID of the one torrent whose error line is shown in
	// full rather than truncated, or "" when none is expanded.
	expandedErr string
	// pending holds each torrent's in-flight or just-settled pause/resume
	// (T-072, download_actions.go), keyed by ID. Replaced, never mutated in
	// place (withPending), since Model is a value type.
	pending map[string]pendingToggle
}

// newDownloadsModel returns a downloadsModel with nothing selected or
// expanded.
func newDownloadsModel() downloadsModel {
	return downloadsModel{}
}

// moveCursor shifts the cursor by delta, clamped to [0, count-1] — a
// non-positive count (nothing to select) always resets it to 0, the same
// "nothing selected" state a fresh downloadsModel starts in.
func (m downloadsModel) moveCursor(delta, count int) downloadsModel {
	if count <= 0 {
		m.cursor = 0
		return m
	}

	m.cursor += delta

	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > count-1 {
		m.cursor = count - 1
	}

	return m
}

// clampCursor re-clamps the cursor to a new row count without moving it —
// used after every engineUpdateMsg (root.go), since a torrent completing,
// erroring, or being removed can shrink or reorder the row list out from
// under whatever the cursor was pointing at.
func (m downloadsModel) clampCursor(count int) downloadsModel {
	return m.moveCursor(0, count)
}

// toggleExpanded flips whether id's error line is shown in full. Toggling
// the already-expanded id collapses it again; toggling a different id
// switches which one is expanded (never more than one at a time — AGENT.md
// §7: "never more than one modal deep" is about modals specifically, but
// the same one-thing-open-at-a-time discipline applies here). A blank id is
// a no-op, so a caller need not special-case "nothing selected".
func (m downloadsModel) toggleExpanded(id string) downloadsModel {
	if id == "" {
		return m
	}

	if m.expandedErr == id {
		m.expandedErr = ""
	} else {
		m.expandedErr = id
	}

	return m
}

// isDownloadComplete reports whether s belongs in the downloads screen's
// completed section (T-071 acceptance: "completed torrents move to a
// distinct section"): fully downloaded and either actively seeding, or
// paused because the seed policy was satisfied (internal/engine/anacrolix's
// applyPolicyLocked reports that as StatePaused with Progress still 1, not
// as a distinct state — TorrentStatus has none). An errored torrent is
// never "complete", even one that failed after finishing a download (a
// write failure during seeding, say) — it belongs with the rest of the
// active/attention-needed section instead.
func isDownloadComplete(s engine.TorrentStatus) bool {
	if s.State == engine.StateErrored {
		return false
	}

	return s.State == engine.StateSeeding || (s.State == engine.StatePaused && s.Progress >= 1)
}

// partitionDownloads splits statuses, in their given order, into the active
// and completed sections T-071 requires.
func partitionDownloads(statuses []engine.TorrentStatus) (active, completed []engine.TorrentStatus) {
	for _, s := range statuses {
		if isDownloadComplete(s) {
			completed = append(completed, s)
		} else {
			active = append(active, s)
		}
	}

	return active, completed
}

// downloadRows is every tracked torrent in the fixed order
// renderDownloadsScreen draws them: active section first, then completed —
// what the cursor (downloadsModel.cursor) indexes into.
func (m Model) downloadRows() []engine.TorrentStatus {
	active, completed := partitionDownloads(m.torrentStatuses)

	rows := make([]engine.TorrentStatus, 0, len(active)+len(completed))
	rows = append(rows, active...)
	rows = append(rows, completed...)

	return rows
}

// handleToggleDownloadDetail implements enter (ActionSelect) on the
// downloads screen (root.go): toggle whether the currently selected row's
// error line, if it has one, is shown in full. A no-op when nothing is
// selected (an empty downloads screen).
func (m Model) handleToggleDownloadDetail() (tea.Model, tea.Cmd) {
	rows := m.downloadRows()
	if m.downloads.cursor < 0 || m.downloads.cursor >= len(rows) {
		return m, nil
	}

	m.downloads = m.downloads.toggleExpanded(rows[m.downloads.cursor].ID)

	return m, nil
}

// downloadOrigin resolves a torrent's provenance for its row's Source
// field: the live engine.Origin when the engine reports one, falling back
// to the store.TorrentRecord the add flow persisted (details.go,
// handleAddResult, T-070). The fallback matters for every torrent added
// this session: engine.AddSource is a frozen §5 contract with no Origin
// field, so Engine.Add itself never populates TorrentStatus.Origin — only a
// real engine's Resumer does, from a *previous* restart's saved session
// (internal/engine/resume.go). Without this fallback, a freshly added
// torrent's Source column would read empty until the next restart, even
// though the store already knows where it came from (found in review of
// T-070, PR #43). addedAt is the zero time when no record is found.
func (m Model) downloadOrigin(s engine.TorrentStatus) (indexerID string, addedAt time.Time) {
	indexerID = s.Origin.IndexerID

	if m.torrentStore == nil {
		return indexerID, time.Time{}
	}

	rec, ok := m.torrentStore.GetTorrent(s.ID)
	if !ok {
		return indexerID, time.Time{}
	}

	if indexerID == "" {
		indexerID = rec.IndexerID
	}

	return indexerID, rec.AddedAt
}

// downloadQueueReason implements "queued torrents show their position and
// why they are waiting" (T-034/T-071 acceptance): the real queue order via
// the optional queueProvider assertion when the engine offers one, or a
// position derived from how many other rows in statuses are also
// StateQueued otherwise (internal/engine/fake does not implement
// queueProvider today).
func downloadQueueReason(eng engine.Engine, statuses []engine.TorrentStatus, id string) string {
	if qp, ok := eng.(queueProvider); ok {
		queue := qp.Queue()
		for i, qid := range queue {
			if qid == id {
				return fmt.Sprintf("queued — position %d of %d, waiting for a download slot to free up", i+1, len(queue))
			}
		}
	}

	pos, total := 0, 0
	for _, s := range statuses {
		if s.State != engine.StateQueued {
			continue
		}
		total++
		if s.ID == id {
			pos = total
		}
	}

	if pos == 0 {
		pos, total = 1, 1
	}

	return fmt.Sprintf("queued — position %d of %d, waiting to start", pos, total)
}

// downloadSeedPolicyText implements "the row states the seeding policy in
// effect" (T-071 acceptance) via the optional seedPolicyProvider assertion
// (internal/engine/anacrolix.Engine.SeedPolicy); internal/engine/fake does
// not implement it, so a fake-backed screen names that plainly rather than
// guessing at a policy nothing reported.
func downloadSeedPolicyText(eng engine.Engine) string {
	if sp, ok := eng.(seedPolicyProvider); ok {
		return sp.SeedPolicy().String()
	}

	return "seeding policy not reported by this engine"
}

// downloadSourceText renders the Source field: the indexer id, or a plain
// "unknown source" when neither the live Origin nor a store record named
// one (a torrent added from a bare magnet the user typed directly, for
// instance — indexer.Result's own convention of never fabricating a value
// for an unpublished field, applied here to provenance instead).
func downloadSourceText(indexerID string) string {
	if strings.TrimSpace(indexerID) == "" {
		return "unknown source"
	}

	return indexerID
}

// downloadAddedText renders when a torrent was added, or "-" when that is
// not known (no TorrentStore wired, or no record for this ID yet).
func downloadAddedText(t time.Time) string {
	if t.IsZero() {
		return "-"
	}

	return t.Format("15:04")
}

// downloadDestText renders a torrent's destination (T-071 acceptance: "each
// row shows its destination, since destinations are per-torrent"). Empty
// reads as "(default)" — engine.AddSource.SavePath's own documented
// meaning for an unset destination — rather than a blank field a user could
// mistake for a rendering bug.
func downloadDestText(path string) string {
	if strings.TrimSpace(path) == "" {
		return "(default)"
	}

	return path
}

// errorLineTruncateWidth is downloadErrorLine's truncation budget for a
// collapsed error line — fixed rather than tied to terminal width, so
// "truncated" (T-071 acceptance) stays meaningfully distinct from the
// per-line terminal-width clipping every screen's render already gets from
// truncateLines, even on a very wide terminal.
const errorLineTruncateWidth = 60

// downloadErrorLine renders an errored torrent's reason: truncated by
// default, or in full when expanded is true (downloadsModel.expandedErr,
// toggled by enter — handleToggleDownloadDetail). A nil Err (not expected
// for StateErrored, but engine.TorrentStatus does not forbid it) reads as
// "unknown error" rather than panicking on a nil dereference.
func downloadErrorLine(s engine.TorrentStatus, expanded bool) string {
	msg := "unknown error"
	if s.Err != nil {
		msg = s.Err.Error()
	}

	if expanded {
		return "error: " + msg
	}

	return "error: " + theme.Truncate(msg, errorLineTruncateWidth) + "  (enter to expand)"
}

// downloadActionHint renders the row's action-hint line (T-071 acceptance:
// row layout includes "the action hint line") — which of open/folder/
// pause-or-resume/remove actually apply to s's current state. The keys
// themselves are the existing ScreenDownloads bindings (keymap.go); T-072/
// T-073 give them real behaviour. Opening a file or its folder makes no
// sense before any data exists (StateQueued/StateChecking), so those two
// are left off until there is something to open.
func downloadActionHint(s engine.TorrentStatus) string {
	var parts []string

	if s.State != engine.StateQueued && s.State != engine.StateChecking {
		parts = append(parts, "[o]pen", "[f]older")
	}

	switch s.State {
	case engine.StatePaused:
		parts = append(parts, "[p] resume")
	case engine.StateErrored:
		parts = append(parts, "[enter] error detail")
	default:
		parts = append(parts, "[p]ause")
	}

	parts = append(parts, "[x] remove")

	return strings.Join(parts, " ")
}

// downloadBarWidth is the fixed cell count of the progress bar every row
// draws (AGENT.md §7: "progress bars use block glyphs, never ASCII
// brackets").
const downloadBarWidth = 20

// renderProgressBar draws progress (clamped to [0,1]) as downloadBarWidth
// cells of g.Full/g.Empty — the theme's own glyph set, so it degrades to
// the plain-ASCII fallback the same way every other glyph in this package
// does (AGENT.md §14), never a hardcoded █/░.
func renderProgressBar(g theme.GlyphSet, progress float64) string {
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}

	filled := int(progress*float64(downloadBarWidth) + 0.5)
	if filled > downloadBarWidth {
		filled = downloadBarWidth
	}

	return strings.Repeat(g.Full, filled) + strings.Repeat(g.Empty, downloadBarWidth-filled)
}

// formatRate renders bps as a compact human-readable rate — the same
// convention components.StatusBar's own formatRate uses, duplicated here
// rather than exported across the package boundary for one small formatter
// (the same call T-937's tracker notes make for the indexer adapters'
// infohash helpers).
func formatRate(bps int64) string {
	const unit = 1024

	switch {
	case bps >= unit*unit*unit:
		return fmt.Sprintf("%.1f GB/s", float64(bps)/float64(unit*unit*unit))
	case bps >= unit*unit:
		return fmt.Sprintf("%.1f MB/s", float64(bps)/float64(unit*unit))
	case bps >= unit:
		return fmt.Sprintf("%.0f KB/s", float64(bps)/float64(unit))
	default:
		return fmt.Sprintf("%d B/s", bps)
	}
}

// formatETA renders a TorrentStatus.ETA: "unknown" for the documented -1
// sentinel, "done" for exactly 0 (already complete), otherwise the largest
// couple of units that fit (AGENT.md §7 example: "ETA 2m").
func formatETA(d time.Duration) string {
	if d < 0 {
		return "unknown"
	}
	if d == 0 {
		return "done"
	}

	d = d.Round(time.Second)

	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		h := int(d.Hours())
		mins := int(d.Minutes()) - h*60
		return fmt.Sprintf("%dh%dm", h, mins)
	}
}

// downloadProgressLine draws a non-queued row's progress line: the bar,
// percentage, transferred/total, down/up rate, peer count, and ETA — the
// full field list T-071's row-layout acceptance names apart from name and
// the action hint.
func downloadProgressLine(th theme.Theme, s engine.TorrentStatus) string {
	bar := renderProgressBar(th.Glyphs, s.Progress)
	pct := fmt.Sprintf("%3.0f%%", s.Progress*100)

	transferred := "-"
	if s.TotalBytes > 0 {
		transferred = formatSize(s.DownloadedBytes) + "/" + formatSize(s.TotalBytes)
	}

	rates := fmt.Sprintf("down %s up %s", formatRate(s.DownRate), formatRate(s.UpRate))
	peers := fmt.Sprintf("%d peers", s.Peers)
	eta := "ETA " + formatETA(s.ETA)

	return strings.Join([]string{bar, pct, transferred, rates, peers, eta}, "  ")
}

// renderDownloadRow draws one torrent's full three-plus-line block: name,
// progress (or queue reason), source/destination/added-at, the seeding
// policy line for a completed row, an error line for an errored one, and
// the action hint.
func (m Model) renderDownloadRow(s engine.TorrentStatus, selected bool) string {
	th := m.theme

	nameStyle := th.Foreground
	if selected {
		nameStyle = th.Accent
	}

	var b strings.Builder

	b.WriteString(nameStyle.Render(s.Name))
	b.WriteString("\n")

	if s.State == engine.StateQueued {
		b.WriteString(th.Muted.Render(downloadQueueReason(m.eng, m.torrentStatuses, s.ID)))
	} else {
		b.WriteString(th.Foreground.Render(downloadProgressLine(th, s)))
	}
	b.WriteString("\n")

	indexerID, addedAt := m.downloadOrigin(s)
	b.WriteString(th.Muted.Render(fmt.Sprintf(
		"%s · added %s · dest %s",
		downloadSourceText(indexerID), downloadAddedText(addedAt), downloadDestText(s.SavePath),
	)))

	if isDownloadComplete(s) {
		b.WriteString("\n")
		b.WriteString(th.Success.Render(downloadSeedPolicyText(m.eng)))
	}

	if s.State == engine.StateErrored {
		b.WriteString("\n")
		b.WriteString(th.Error.Render(downloadErrorLine(s, m.downloads.expandedErr == s.ID)))
	}

	b.WriteString("\n")
	b.WriteString(th.Dim.Render(downloadActionHint(s)))

	return b.String()
}

// renderDownloadsScreen draws ScreenDownloads' real body: the active
// section, then the completed section, each only when non-empty, or an
// empty state when nothing has ever been added. Pure: reads m and returns a
// string, no I/O, no mutation (AGENT.md §6.8).
func (m Model) renderDownloadsScreen() string {
	th := m.theme

	active, completed := partitionDownloads(m.torrentStatuses)
	if len(active) == 0 && len(completed) == 0 {
		return theme.Truncate(
			th.Muted.Render("No downloads yet — add a result from search or details."),
			m.width,
		)
	}

	var b strings.Builder

	cursor := 0

	writeSection := func(title string, statuses []engine.TorrentStatus) {
		if len(statuses) == 0 {
			return
		}

		if b.Len() > 0 {
			b.WriteString("\n\n")
		}

		b.WriteString(th.Muted.Render(title))

		for _, s := range statuses {
			b.WriteString("\n\n")
			b.WriteString(m.renderDownloadRow(s, cursor == m.downloads.cursor))
			cursor++
		}
	}

	writeSection(fmt.Sprintf("Active (%d)", len(active)), active)
	writeSection(fmt.Sprintf("Completed (%d)", len(completed)), completed)

	return truncateLines(b.String(), m.width)
}
