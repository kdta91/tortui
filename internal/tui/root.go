package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/tui/components"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// Model is the top-level bubbletea program: it owns which Screen is
// current, the global keymap, the help overlay, and the one cross-cutting
// piece of behaviour that belongs to the root rather than any single
// screen — refusing to quit out from under an active download without
// confirmation (AGENT.md §7).
//
// Model imports only engine.Engine (an interface, AGENT.md §4) so it runs
// end-to-end against internal/engine/fake with no real BitTorrent swarm.
// Every screen's actual content is a placeholder; see the package doc
// comment in keymap.go for which task owns which screen.
type Model struct {
	eng   engine.Engine
	theme theme.Theme
	keys  KeyMap

	screen Screen
	width  int
	height int

	// selection is a generic, screen-agnostic cursor position: "whichever
	// item is highlighted in the currently active screen." No screen has
	// content yet (search T-060, results T-061, and so on each own their
	// real list), so this is deliberately just an integer, moved by the
	// existing ActionMoveUp/ActionMoveDown bindings — it exists so T-054's
	// "esc always cancels; focus returns to the originating screen and
	// selection" is genuinely testable now rather than deferred, and a
	// later screen is free to replace it with its own bounded,
	// data-backed cursor.
	selection int

	showHelp bool
	// quitConfirm is root's one Dialog instance (T-054): the "quit with
	// active downloads?" prompt. It used to be a bare bool (T-051); it is
	// a components.Dialog now so the generic modal component has one real
	// caller inside this task's own scope, per T-054's acceptance
	// criteria, without building ahead into T-072's remove-confirm flow or
	// T-074's destination picker, which get their own Dialog instances
	// when those tasks land.
	quitConfirm components.Dialog
	// errorDetail is true while the status bar's source-error detail panel
	// (T-052, ContextErrorDetail) is open. This is an info panel, not a
	// confirm dialog, so it stays a plain bool rather than a Dialog.
	errorDetail bool

	activeDownloads int

	// Banner, when non-empty, renders as one fixed line above every screen
	// and modal, in the theme's accent style. It is a generic, root-owned
	// affordance — no screen or dialog reads or sets it — that exists for
	// --demo mode (T-056, AGENT.md §15) to make it unmistakable that the
	// data on screen is synthetic. Production wiring leaves it empty.
	Banner string

	// statusBar is the AGENT.md §7 footer: active-download count and
	// aggregate rate (kept in sync from engineUpdateMsg), the most recent
	// search fan-out's source-error state (sourceStatusMsg — no screen
	// sends one yet; T-060/T-061 will), and the transient-message queue
	// (transientMessageMsg / components.TickMsg).
	statusBar components.StatusBar
}

// New builds a Model wired to eng (typically a real engine in production,
// internal/engine/fake in tests) and th, starting on ScreenSearch with no
// modal open.
func New(eng engine.Engine, th theme.Theme) Model {
	return Model{
		eng:         eng,
		theme:       th,
		keys:        NewKeyMap(),
		screen:      ScreenSearch,
		statusBar:   components.New(),
		quitConfirm: newQuitDialog(),
	}
}

// quitDialogCancel and quitDialogQuit index newQuitDialog's Options.
// Cancel is the default (index 0): quitting mid-download is destructive,
// so the safer choice is what enter picks with no other input.
const (
	quitDialogCancel = 0
	quitDialogQuit   = 1
)

// newQuitDialog builds the "quit with active downloads?" confirmation as a
// components.Dialog (T-054) — AGENT.md §7's "quit ... prompts if downloads
// active," now expressed through the one generic modal mechanism instead
// of a one-off bool, per T-054's note to prefer that where it fits
// cleanly. Message is filled in per-render by renderQuitConfirm, since it
// names the live active-download count.
func newQuitDialog() components.Dialog {
	return components.NewDialog("Quit tortui?", "", []components.DialogOption{
		quitDialogCancel: {Label: "Cancel"},
		quitDialogQuit:   {Label: "Quit"},
	}, quitDialogCancel)
}

// engineUpdateMsg carries one coalesced snapshot from engine.Engine.Updates.
type engineUpdateMsg struct {
	statuses []engine.TorrentStatus
	closed   bool
}

// waitForEngineUpdate returns a tea.Cmd that performs exactly one receive
// from eng.Updates() and reports it as a message. Update re-issues this
// after every engineUpdateMsg, which is the standard bubbletea pattern for
// subscribing to a channel without ever blocking inside Update itself
// (AGENT.md §6.1) — Init/Update never call Updates() more than once
// in-flight.
func waitForEngineUpdate(eng engine.Engine) tea.Cmd {
	return func() tea.Msg {
		statuses, ok := <-eng.Updates()
		if !ok {
			return engineUpdateMsg{closed: true}
		}

		return engineUpdateMsg{statuses: statuses}
	}
}

// countActive reports how many statuses are neither errored nor sitting at
// 100% with no seeding — anything the user would reasonably call "an
// active download" for the purposes of the quit-confirmation prompt.
func countActive(statuses []engine.TorrentStatus) int {
	n := 0

	for _, s := range statuses {
		switch s.State {
		case engine.StateDownloading, engine.StateChecking, engine.StateQueued, engine.StateSeeding:
			n++
		case engine.StatePaused, engine.StateErrored:
			// Not actively transferring; does not block quit.
		}
	}

	return n
}

// aggregateRates sums DownRate and UpRate across every tracked torrent, for
// the status bar's "aggregate down/up rate" (AGENT.md §7).
func aggregateRates(statuses []engine.TorrentStatus) (down, up int64) {
	for _, s := range statuses {
		down += s.DownRate
		up += s.UpRate
	}

	return down, up
}

// sourceStatusMsg reports one search fan-out's per-source outcome for the
// status bar's error indicator (AGENT.md §6.3: "2/4 sources failed"; see
// DEC-092 for the actual expand key). No screen sends this yet — T-060's
// search and T-061's results table are what will actually call
// indexer.Registry.SearchAll and translate its []SourceError into this —
// but T-052 wires and tests the plumbing directly, the same pattern T-051
// used for engineUpdateMsg before any screen produced one.
type sourceStatusMsg struct {
	total  int
	failed []string
}

// transientMessageMsg requests a queued, timed status-bar message (e.g. "◆
// added ubuntu-24.04.iso", a save-settings confirmation, or a one-line
// error toast). Later tasks send this after an action completes; T-052
// wires and tests the queue/timeout mechanism (components.StatusBar) by
// sending it directly.
type transientMessageMsg struct{ text string }

// Init subscribes to the engine's update stream. It performs no other I/O
// and blocks on nothing itself — the actual channel receive happens inside
// the tea.Cmd returned by waitForEngineUpdate, on bubbletea's own goroutine
// (AGENT.md §6.1).
func (m Model) Init() tea.Cmd {
	if m.eng == nil {
		return nil
	}

	return waitForEngineUpdate(m.eng)
}

// Update handles one message. It performs no I/O of its own — every
// side-effecting action is returned as a tea.Cmd for bubbletea to run
// (AGENT.md §6.1) — and every field it changes is plain state, so nothing
// here can outlive a resize or a screen switch as a stale cached value
// (AGENT.md's "no cached widths survive a resize").
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		return m, nil

	case engineUpdateMsg:
		if msg.closed {
			return m, nil
		}

		m.activeDownloads = countActive(msg.statuses)
		m.statusBar.ActiveDownloads = m.activeDownloads
		m.statusBar.DownRate, m.statusBar.UpRate = aggregateRates(msg.statuses)

		return m, waitForEngineUpdate(m.eng)

	case sourceStatusMsg:
		m.statusBar.SourcesTotal = msg.total
		m.statusBar.FailedSources = msg.failed

		if len(msg.failed) == 0 {
			m.errorDetail = false
		}

		return m, nil

	case transientMessageMsg:
		var cmd tea.Cmd
		m.statusBar, cmd = m.statusBar.Push(msg.text)

		return m, cmd

	case components.TickMsg:
		var cmd tea.Cmd
		m.statusBar, cmd = m.statusBar.Update(msg)

		return m, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// context reports which Context governs key lookups right now: whichever
// modal is open, or the current screen.
func (m Model) context() Context {
	switch {
	case m.quitConfirm.IsOpen():
		return ContextQuitConfirm
	case m.showHelp:
		return ContextHelp
	case m.errorDetail:
		return ContextErrorDetail
	default:
		return screenContext(m.screen)
	}
}

// handleKey looks up msg's Action in the current Context and applies it.
// Screen-routing and the two root-owned modals (help, quit-confirm) are
// handled here; every other Action belongs to a screen this task does not
// implement and is a deliberate no-op — a later task gives it behaviour by
// handling it in that screen's own Update, not by changing this switch.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	action, ok := m.keys.Lookup(m.context(), key)
	if !ok {
		return m, nil
	}

	switch action {
	case ActionQuit:
		return m.handleQuit()
	case ActionConfirmYes:
		// y/enter is its own direct hotkey for "quit" here, independent of
		// the dialog's j/k cursor (not bound inside ContextQuitConfirm at
		// all — see quitConfirmBindings) — there's nothing left to clean
		// up in m.quitConfirm since the program is exiting.
		return m, tea.Quit
	case ActionConfirmNo, ActionCancel:
		// esc (or n) always cancels: the dialog closes (its own cursor
		// resets to Default), the help overlay and error-detail panel
		// close, and — because none of this touches m.screen or
		// m.selection — focus and selection are exactly what they were
		// before the modal opened (T-054 acceptance).
		m.quitConfirm = m.quitConfirm.Cancel()
		m.showHelp = false
		m.errorDetail = false

		return m, nil
	case ActionHelp:
		m.showHelp = !m.showHelp

		return m, nil
	case ActionToggleErrorDetail:
		return m.handleToggleErrorDetail()
	case ActionMoveDown:
		m.selection++
		return m, nil
	case ActionMoveUp:
		if m.selection > 0 {
			m.selection--
		}

		return m, nil
	case ActionNextScreen:
		m.screen = m.screen.next()
		return m, nil
	case ActionPrevScreen:
		m.screen = m.screen.prev()
		return m, nil
	case ActionGotoSearch:
		m.screen = ScreenSearch
		return m, nil
	case ActionGotoResults:
		m.screen = ScreenResults
		return m, nil
	case ActionGotoDetails:
		m.screen = ScreenDetails
		return m, nil
	case ActionGotoDownloads:
		m.screen = ScreenDownloads
		return m, nil
	case ActionGotoSettings:
		m.screen = ScreenSettings
		return m, nil
	default:
		// ActionFocusSearch, ActionLatest, ActionRefresh, ActionMoveUp/Down,
		// ActionSelect, ActionDetails, sort, open-file/folder/source,
		// pause/resume, and remove all belong to screens/components this
		// task does not implement (T-053, T-054, T-060-T-080). No-op here.
		return m, nil
	}
}

// handleToggleErrorDetail implements T-052's status-bar error indicator:
// "e" opens the detail panel only when at least one source has actually
// failed; tab or esc from inside the panel (ContextErrorDetail) always
// closes it (see DEC-092 for why tab, not the screen-cycle key everywhere
// else).
func (m Model) handleToggleErrorDetail() (tea.Model, tea.Cmd) {
	if m.errorDetail {
		m.errorDetail = false
		return m, nil
	}

	if len(m.statusBar.FailedSources) > 0 {
		m.errorDetail = true
	}

	return m, nil
}

// handleQuit implements AGENT.md §7's "quit (prompts if downloads active)":
// quitting is immediate when nothing is actively transferring, and opens a
// one-shot confirmation otherwise.
func (m Model) handleQuit() (tea.Model, tea.Cmd) {
	if m.activeDownloads > 0 && !m.quitConfirm.IsOpen() {
		// Guarded by IsOpen just above, so this Open never hits the
		// already-open, logged-no-op branch through normal key handling —
		// that branch exists for a caller bug, not this call site.
		m.quitConfirm = m.quitConfirm.Open()
		return m, nil
	}

	return m, tea.Quit
}

// View renders the current state. It is pure — it reads m and returns a
// string, with no I/O and no mutation (AGENT.md §6.8) — and every width
// used below comes from m.width/m.height as last reported by
// tea.WindowSizeMsg, never from a cached value computed at construction, so
// a resize always takes effect on the very next render.
func (m Model) View() string {
	if m.width <= 0 {
		return ""
	}

	var body string

	switch m.context() {
	case ContextHelp:
		body = m.renderHelp()
	case ContextQuitConfirm:
		body = m.renderQuitConfirm()
	case ContextErrorDetail:
		body = m.renderErrorDetail()
	default:
		body = m.renderScreen()
	}

	out := body + "\n\n" + m.renderStatusBar()

	if m.Banner != "" {
		out = m.theme.Accent.Render(theme.Truncate(m.Banner, m.width)) + "\n\n" + out
	}

	return out
}

// renderStatusBar draws the AGENT.md §7 footer via components.StatusBar,
// always reading m.screen/m.width fresh so a resize or a screen switch
// takes effect on the very next render (no cached value, per T-051's
// invariant).
func (m Model) renderStatusBar() string {
	return m.statusBar.View(m.width, m.screen.String(), m.theme)
}

// renderErrorDetail draws the status bar's expanded source-error panel:
// every failed/skipped source by id, plus this context's own bindings
// (tab to collapse, esc to close — DEC-092).
func (m Model) renderErrorDetail() string {
	var b strings.Builder

	b.WriteString(m.theme.Error.Render(
		fmt.Sprintf("%d/%d sources failed", len(m.statusBar.FailedSources), m.statusBar.SourcesTotal),
	))
	b.WriteString("\n\n")

	for _, line := range m.statusBar.DetailLines() {
		b.WriteString(m.theme.Foreground.Render(line))
		b.WriteString("\n")
	}

	b.WriteString("\n")

	for _, line := range m.keys.HelpFor(ContextErrorDetail) {
		b.WriteString(m.theme.Muted.Render(line))
		b.WriteString("\n")
	}

	return b.String()
}

// renderScreen draws the tab bar and the current screen's placeholder body.
func (m Model) renderScreen() string {
	var b strings.Builder

	b.WriteString(m.renderTabs())
	b.WriteString("\n\n")
	b.WriteString(m.renderPlaceholderBody())

	return b.String()
}

// renderTabs draws the five screen names, the current one accented, joined
// by the theme's dim style — one line, degrading to the theme's own
// no-colour behaviour under NO_COLOR (AGENT.md §14) since it uses Theme's
// styles rather than any hardcoded escape.
func (m Model) renderTabs() string {
	labels := make([]string, 0, len(screenOrder))

	for i, s := range screenOrder {
		label := fmtTabLabel(i+1, s)
		if s == m.screen {
			label = m.theme.Accent.Render(label)
		} else {
			label = m.theme.Muted.Render(label)
		}

		labels = append(labels, label)
	}

	return strings.Join(labels, m.theme.Dim.Render(" · "))
}

func fmtTabLabel(n int, s Screen) string {
	return strings.ToUpper(s.String()[:1]) + s.String()[1:] + " [" + itoa(n) + "]"
}

// itoa avoids pulling in strconv for a single-digit screen index (1-5).
func itoa(n int) string {
	if n < 0 || n > 9 {
		return "?"
	}

	return string(rune('0' + n))
}

// renderPlaceholderBody draws the current screen's placeholder content —
// real content is a later task's job (see keymap.go's Screen.placeholderTask).
func (m Model) renderPlaceholderBody() string {
	body := m.screen.String() + " screen — placeholder, see " + m.screen.placeholderTask()
	return theme.Truncate(body, m.width)
}

// renderHelp draws the ? overlay: every binding live in the current screen
// plus the help-overlay's own close binding, generated entirely from
// KeyMap.HelpFor so there is no separately maintained help text to drift
// out of sync (T-051 acceptance).
func (m Model) renderHelp() string {
	var b strings.Builder

	b.WriteString(m.theme.Accent.Render("Keys"))
	b.WriteString("\n\n")

	for _, line := range m.keys.HelpFor(screenContext(m.screen)) {
		b.WriteString(m.theme.Foreground.Render(line))
		b.WriteString("\n")
	}

	for _, line := range m.keys.HelpFor(ContextHelp) {
		b.WriteString(m.theme.Foreground.Render(line))
		b.WriteString("\n")
	}

	return b.String()
}

// renderQuitConfirm draws the one-shot "active downloads" quit prompt
// through components.Dialog (T-054): the box itself, plus this context's
// own key bindings underneath, generated the same way every other modal in
// this file already does.
func (m Model) renderQuitConfirm() string {
	dialog := m.quitConfirm
	dialog.Message = itoa(min9(m.activeDownloads)) + " active download(s) will stop."

	var b strings.Builder

	b.WriteString(dialog.View(m.theme, m.width))
	b.WriteString("\n\n")

	for _, line := range m.keys.HelpFor(ContextQuitConfirm) {
		b.WriteString(m.theme.Muted.Render(line))
		b.WriteString("\n")
	}

	return b.String()
}

// min9 clamps to itoa's single-digit range so the quit prompt never renders
// a "?" for a realistic active-download count; anything at or above 9 is
// still meaningfully "several", not a value worth a second digit here.
func min9(n int) int {
	if n > 9 {
		return 9
	}

	return n
}
