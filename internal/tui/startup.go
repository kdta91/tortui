package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/indexer"
)

// SessionSaver is the subset of internal/lifecycle.Session the add and
// remove flows call once the engine has accepted the change, so a crash
// before the next clean shutdown still resumes what the user just did
// (T-095, AGENT.md §13). nil is valid: nothing is saved until shutdown.
type SessionSaver interface {
	Save() error
}

// WithSessionSaver wires s to be saved after every successful add
// (handleAddResult, after the add flow's own SetTorrent, so the saved record
// keeps the Origin it wrote) and every successful remove
// (handleRemoveResult). The save runs as a tea.Cmd, never inside Update
// (AGENT.md §6.1).
func WithSessionSaver(s SessionSaver) Option {
	return func(m *Model) { m.sessionSaver = s }
}

// WithStartupNotice queues texts on the status bar, in order, as the
// program starts — the composition root's channel for what startup found
// that the user must hear about on the first render, such as resumed
// torrents whose data is missing (lifecycle.ResumeReport.Missing). Empty
// strings are skipped.
func WithStartupNotice(texts ...string) Option {
	return func(m *Model) {
		for _, t := range texts {
			if t != "" {
				m.startupNotices = append(m.startupNotices, t)
			}
		}
	}
}

// WithStartupLatest makes the program run one Latest query against the
// search screen's selected sources as it starts, exactly as pressing `L`
// would, so a fresh install shows the bundled sources' recent additions
// with no input (AGENT.md §1, T-095, DEC-129). It is one fetch per launch,
// never a timer (AGENT.md §6.13). A source set with nothing that serves
// Latest runs nothing and says nothing.
func WithStartupLatest(b bool) Option {
	return func(m *Model) { m.startupLatest = b }
}

// startupLatestMsg asks Update to run WithStartupLatest's one query.
type startupLatestMsg struct{}

// startupCmds returns the one-shot commands Init adds for the startup
// options above: the queued notices and the startup Latest query.
func (m Model) startupCmds() []tea.Cmd {
	var cmds []tea.Cmd

	notices := make([]tea.Cmd, 0, len(m.startupNotices))
	for _, text := range m.startupNotices {
		notices = append(notices, func() tea.Msg { return transientMessageMsg{text: text} })
	}

	// Sequence, not Batch, keeps the notices in the order given.
	if len(notices) > 0 {
		cmds = append(cmds, tea.Sequence(notices...))
	}

	if m.startupLatest {
		cmds = append(cmds, func() tea.Msg { return startupLatestMsg{} })
	}

	return cmds
}

// handleStartupLatest runs the startup Latest query, or does nothing when
// no selected source serves Latest — a startup the user did not ask for
// must not greet them with "select at least one source".
func (m Model) handleStartupLatest() (tea.Model, tea.Cmd) {
	if m.searcher == nil || len(m.search.selectedIDs(indexer.ModeLatest)) == 0 {
		return m, nil
	}

	m, cmd := m.dispatchSearch(true)
	m.startupGen = m.search.generation

	return m, cmd
}

// saveSessionCmd returns the tea.Cmd that saves s, reporting a failure on
// the status bar. nil when no saver is wired.
func saveSessionCmd(s SessionSaver) tea.Cmd {
	if s == nil {
		return nil
	}

	return func() tea.Msg {
		if err := s.Save(); err != nil {
			return transientMessageMsg{text: fmt.Sprintf("couldn't save session: %v", err)}
		}

		return nil
	}
}
