package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/engine"
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

	showHelp    bool
	quitConfirm bool

	activeDownloads int
}

// New builds a Model wired to eng (typically a real engine in production,
// internal/engine/fake in tests) and th, starting on ScreenSearch with no
// modal open.
func New(eng engine.Engine, th theme.Theme) Model {
	return Model{
		eng:    eng,
		theme:  th,
		keys:   NewKeyMap(),
		screen: ScreenSearch,
	}
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

		return m, waitForEngineUpdate(m.eng)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// context reports which Context governs key lookups right now: whichever
// modal is open, or the current screen.
func (m Model) context() Context {
	switch {
	case m.quitConfirm:
		return ContextQuitConfirm
	case m.showHelp:
		return ContextHelp
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
		return m, tea.Quit
	case ActionConfirmNo, ActionCancel:
		m.quitConfirm = false
		m.showHelp = false

		return m, nil
	case ActionHelp:
		m.showHelp = !m.showHelp

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
		// task does not implement (T-052-T-054, T-060-T-080). No-op here.
		return m, nil
	}
}

// handleQuit implements AGENT.md §7's "quit (prompts if downloads active)":
// quitting is immediate when nothing is actively transferring, and opens a
// one-shot confirmation otherwise.
func (m Model) handleQuit() (tea.Model, tea.Cmd) {
	if m.activeDownloads > 0 && !m.quitConfirm {
		m.quitConfirm = true
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

	switch m.context() {
	case ContextHelp:
		return m.renderHelp()
	case ContextQuitConfirm:
		return m.renderQuitConfirm()
	default:
		return m.renderScreen()
	}
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

// renderQuitConfirm draws the one-shot "active downloads" quit prompt.
func (m Model) renderQuitConfirm() string {
	var b strings.Builder

	b.WriteString(m.theme.Error.Render("Quit tortui?"))
	b.WriteString("\n")
	b.WriteString(m.theme.Foreground.Render(itoa(min9(m.activeDownloads)) + " active download(s) will stop."))
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
