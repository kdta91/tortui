package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
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

// startupCmds returns the one-shot commands Init adds for the startup
// option above: the queued notices. Nothing queries a source at launch
// (T-9011, DEC-137): the program opens on an empty Search screen.
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

	return cmds
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
