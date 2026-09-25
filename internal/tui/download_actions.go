// Download actions (T-072): the downloads screen's `p` pause/resume, `x`
// remove-with-confirm, and `u` open-source-page keys, acting on whichever
// row downloads.go's cursor currently selects.
//
// Every engine call runs inside a tea.Cmd, never in Update itself (AGENT.md
// §6.1) — anacrolix's Pause/Resume/Remove take the engine's own lock and
// Remove may delete data from disk. Each action identifies its torrent by
// ID captured at keypress time, never by cursor index, so a snapshot that
// reorders or shrinks the row list between the keypress and the engine call
// cannot redirect the action at a different torrent. A torrent that
// vanished in that window makes the engine call fail, and that failure is
// reported through the status bar like any other — no panic, no stale
// optimistic state left behind.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/tui/components"
)

// pendingToggle is one in-flight (or just-completed) pause/resume: the
// state the row was optimistically moved to, the state it had before (so a
// failed engine call can put it back), and whether the engine call has
// returned successfully yet.
//
// Reconciliation rule: while the engine call is still in flight (settled
// false), no snapshot can reflect it yet, so optimistic is laid over every
// incoming snapshot for this ID. Once the call returns successfully
// (settled true), the very next snapshot is authoritative and the entry is
// dropped — whatever state the engine actually reports (StateQueued rather
// than StateDownloading after a resume, say, or StatePaused again when a
// satisfied seed policy re-applies) replaces the guess.
type pendingToggle struct {
	optimistic engine.State
	prev       engine.State
	settled    bool
}

// pauseResumeResultMsg reports one pause/resume engine call's outcome.
type pauseResumeResultMsg struct {
	id     string
	name   string
	resume bool
	err    error
}

// removeResultMsg reports one remove engine call's outcome.
type removeResultMsg struct {
	id         string
	name       string
	deleteData bool
	err        error
}

// removeTarget is the torrent the remove-confirm dialog is asking about,
// captured when x opened it.
type removeTarget struct {
	id   string
	name string
}

// removeDialog* index newRemoveDialog's Options, in the order T-072's
// acceptance lists them. Cancel is the default: removing is destructive and
// deleting data is irreversible, so enter with no other input does nothing.
const (
	removeDialogKeep   = 0
	removeDialogDelete = 1
	removeDialogCancel = 2
)

// newRemoveDialog builds the `x` confirmation (AGENT.md §7: "always opens a
// confirm dialog offering keep data / delete data"). Message is filled in
// when it opens, since it names the torrent.
func newRemoveDialog() components.Dialog {
	return components.NewDialog("Remove torrent?", "", []components.DialogOption{
		removeDialogKeep:   {Label: "Remove, keep data"},
		removeDialogDelete: {Label: "Remove and delete data"},
		removeDialogCancel: {Label: "Cancel"},
	}, removeDialogCancel)
}

// selectedDownload returns the row the downloads cursor points at, or false
// when there is none (an empty screen).
func (m Model) selectedDownload() (engine.TorrentStatus, bool) {
	rows := m.downloadRows()
	if m.downloads.cursor < 0 || m.downloads.cursor >= len(rows) {
		return engine.TorrentStatus{}, false
	}

	return rows[m.downloads.cursor], true
}

// hasTorrent reports whether id is in the most recent snapshot.
func (m Model) hasTorrent(id string) bool {
	for _, s := range m.torrentStatuses {
		if s.ID == id {
			return true
		}
	}

	return false
}

// withTorrentState returns a copy of statuses with id's State set to state
// (and its transfer rates zeroed when state is StatePaused, matching what
// every engine reports for a paused torrent). The copy matters: the
// original slice is an engine snapshot, and bubbletea models are values —
// mutating it in place would leak into every earlier Model copy.
func withTorrentState(statuses []engine.TorrentStatus, id string, state engine.State) []engine.TorrentStatus {
	out := make([]engine.TorrentStatus, len(statuses))
	copy(out, statuses)

	for i := range out {
		if out[i].ID != id {
			continue
		}

		out[i].State = state
		if state == engine.StatePaused {
			out[i].DownRate, out[i].UpRate = 0, 0
		}
	}

	return out
}

// withPending returns a copy of the pending map with id set to p, or with id
// removed when p is nil — copy-on-write for the same value-semantics reason
// as withTorrentState.
func withPending(pending map[string]pendingToggle, id string, p *pendingToggle) map[string]pendingToggle {
	out := make(map[string]pendingToggle, len(pending)+1)
	for k, v := range pending {
		out[k] = v
	}

	if p == nil {
		delete(out, id)
	} else {
		out[id] = *p
	}

	return out
}

// reconcilePending applies the pendingToggle rule to a fresh snapshot: it
// returns the statuses to display and the pending set to keep. Unsettled
// entries are laid over the snapshot; settled entries are dropped, the
// snapshot winning; entries for an ID the snapshot no longer contains are
// dropped too, since there is no row left to lay anything over.
func reconcilePending(statuses []engine.TorrentStatus, pending map[string]pendingToggle) ([]engine.TorrentStatus, map[string]pendingToggle) {
	if len(pending) == 0 {
		return statuses, pending
	}

	present := make(map[string]bool, len(statuses))
	for _, s := range statuses {
		present[s.ID] = true
	}

	kept := make(map[string]pendingToggle, len(pending))

	for id, p := range pending {
		if !present[id] || p.settled {
			continue
		}

		statuses = withTorrentState(statuses, id, p.optimistic)
		kept[id] = p
	}

	return statuses, kept
}

// pauseResumeCmd performs the engine call off Update's goroutine.
func pauseResumeCmd(eng engine.Engine, id, name string, resume bool) tea.Cmd {
	return func() tea.Msg {
		var err error
		if resume {
			err = eng.Resume(id)
		} else {
			err = eng.Pause(id)
		}

		return pauseResumeResultMsg{id: id, name: name, resume: resume, err: err}
	}
}

// removeCmd performs the engine call off Update's goroutine. Containment of
// any data deletion inside the known destination roots is the engine's own
// job (AGENT.md §6.11/§6.12, internal/engine/anacrolix), checked there at
// the moment of deletion; the TUI passes only the ID.
func removeCmd(eng engine.Engine, id, name string, deleteData bool) tea.Cmd {
	return func() tea.Msg {
		err := eng.Remove(id, deleteData)

		return removeResultMsg{id: id, name: name, deleteData: deleteData, err: err}
	}
}

// pushStatus queues a transient status-bar message and returns its tick
// command.
func (m Model) pushStatus(text string) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.statusBar, cmd = m.statusBar.Push(text)

	return m, cmd
}

// handlePauseResume implements `p` on the downloads screen: resume a paused
// torrent, pause anything else, updating the row optimistically so the
// keypress is visible before the engine's next snapshot arrives.
func (m Model) handlePauseResume() (tea.Model, tea.Cmd) {
	s, ok := m.selectedDownload()
	if !ok {
		return m, nil
	}

	if s.State == engine.StateErrored {
		return m.pushStatus(fmt.Sprintf("%s has errored — nothing to pause or resume", s.Name))
	}

	// A second p before the first engine call returns is ignored rather
	// than issued: the two calls would run on separate goroutines with no
	// ordering guarantee, so a fast pause-then-resume could land at the
	// engine as resume-then-pause.
	if p, inFlight := m.downloads.pending[s.ID]; inFlight && !p.settled {
		return m.pushStatus(fmt.Sprintf("%s: still applying the previous pause/resume", s.Name))
	}

	resume := s.State == engine.StatePaused

	optimistic := engine.StatePaused
	if resume {
		optimistic = engine.StateDownloading
		if s.Progress >= 1 {
			optimistic = engine.StateSeeding
		}
	}

	m.downloads.pending = withPending(m.downloads.pending, s.ID, &pendingToggle{
		optimistic: optimistic,
		prev:       s.State,
	})
	m.torrentStatuses = withTorrentState(m.torrentStatuses, s.ID, optimistic)

	return m, pauseResumeCmd(m.eng, s.ID, s.Name, resume)
}

// handlePauseResumeResult settles or rolls back the optimistic update
// handlePauseResume made.
func (m Model) handlePauseResumeResult(msg pauseResumeResultMsg) (tea.Model, tea.Cmd) {
	p, ok := m.downloads.pending[msg.id]

	if msg.err == nil {
		if ok {
			p.settled = true
			m.downloads.pending = withPending(m.downloads.pending, msg.id, &p)
		}

		return m, nil
	}

	m.downloads.pending = withPending(m.downloads.pending, msg.id, nil)
	if ok && m.hasTorrent(msg.id) {
		m.torrentStatuses = withTorrentState(m.torrentStatuses, msg.id, p.prev)
	}

	verb := "pause"
	if msg.resume {
		verb = "resume"
	}

	return m.pushStatus(fmt.Sprintf("couldn't %s %s: %v", verb, msg.name, msg.err))
}

// handleRemove implements `x` on the downloads screen: open the
// keep/delete/cancel dialog for the selected torrent.
func (m Model) handleRemove() (tea.Model, tea.Cmd) {
	s, ok := m.selectedDownload()
	if !ok {
		return m, nil
	}

	m.removeTarget = removeTarget{id: s.ID, name: s.Name}
	m.removeConfirm.Message = fmt.Sprintf("Remove %q from tortui?", s.Name)
	m.removeConfirm = m.removeConfirm.Open()

	return m, nil
}

// handleRemoveConfirmAction routes a key's Action while the remove dialog
// (ContextRemoveConfirm) is open.
func (m Model) handleRemoveConfirmAction(action Action) (tea.Model, tea.Cmd) {
	switch action {
	case ActionMoveDown:
		m.removeConfirm = m.removeConfirm.MoveNext()
	case ActionMoveUp:
		m.removeConfirm = m.removeConfirm.MovePrev()
	case ActionCancel, ActionConfirmNo:
		m.removeConfirm = m.removeConfirm.Cancel()
		m.removeTarget = removeTarget{}
	case ActionConfirmYes:
		var choice int
		m.removeConfirm, choice = m.removeConfirm.Confirm()
		target := m.removeTarget
		m.removeTarget = removeTarget{}

		if choice != removeDialogKeep && choice != removeDialogDelete {
			return m, nil
		}

		if !m.hasTorrent(target.id) {
			return m.pushStatus(fmt.Sprintf("%s is no longer in the download list", target.name))
		}

		return m, removeCmd(m.eng, target.id, target.name, choice == removeDialogDelete)
	default:
		// Every other key is unbound in ContextRemoveConfirm; nothing else
		// reaches here.
	}

	return m, nil
}

// handleRemoveResult reports a remove engine call's outcome. The row itself
// disappears on the engine's next snapshot.
func (m Model) handleRemoveResult(msg removeResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m.pushStatus(fmt.Sprintf("couldn't remove %s: %v", msg.name, msg.err))
	}

	m.downloads.pending = withPending(m.downloads.pending, msg.id, nil)
	if m.downloads.expandedErr == msg.id {
		m.downloads.expandedErr = ""
	}

	if msg.deleteData {
		return m.pushStatus(fmt.Sprintf("removed %s and deleted its data", msg.name))
	}

	return m.pushStatus(fmt.Sprintf("removed %s (data kept)", msg.name))
}

// downloadSourceURL resolves a torrent's source page: the live Origin when
// the engine reports one, else the record the add flow persisted — the same
// fallback downloadOrigin uses, for the same reason (Engine.Add never
// populates Origin for a torrent added this session).
func (m Model) downloadSourceURL(s engine.TorrentStatus) string {
	if u := strings.TrimSpace(s.Origin.SourceURL); u != "" {
		return u
	}

	if m.torrentStore == nil {
		return ""
	}

	rec, ok := m.torrentStore.GetTorrent(s.ID)
	if !ok {
		return ""
	}

	return strings.TrimSpace(rec.SourceURL)
}

// handleOpenDownloadSource implements `u` on the downloads screen: open the
// selected torrent's source page in the system browser, through the same
// m.openURL seam (platform.OpenURL by default) the details screen uses.
func (m Model) handleOpenDownloadSource() (tea.Model, tea.Cmd) {
	s, ok := m.selectedDownload()
	if !ok {
		return m, nil
	}

	u := m.downloadSourceURL(s)
	if u == "" {
		return m.pushStatus(fmt.Sprintf("no source page for %s", s.Name))
	}

	return m, openSourceCmd(m.openURL, u)
}

// renderRemoveConfirm draws the remove dialog plus its own key bindings,
// the same layout renderQuitConfirm uses.
func (m Model) renderRemoveConfirm() string {
	var b strings.Builder

	b.WriteString(m.removeConfirm.View(m.theme, m.width))
	b.WriteString("\n\n")

	for _, line := range m.keys.HelpFor(ContextRemoveConfirm) {
		b.WriteString(m.theme.Muted.Render(line))
		b.WriteString("\n")
	}

	return b.String()
}
