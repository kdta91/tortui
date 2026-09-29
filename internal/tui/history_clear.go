package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/tui/components"
)

const (
	clearHistoryDialogClear  = 0
	clearHistoryDialogCancel = 1
)

// newClearHistoryDialog builds the search screen's `c` dialog. Cancel is the
// default so an accidental enter changes nothing.
func newClearHistoryDialog() components.Dialog {
	return components.NewDialog("Clear recent searches?", "", []components.DialogOption{
		clearHistoryDialogClear:  {Label: "Clear"},
		clearHistoryDialogCancel: {Label: "Cancel"},
	}, clearHistoryDialogCancel)
}

// historyClearedMsg reports clearHistoryCmd's outcome.
type historyClearedMsg struct{ err error }

// clearHistoryCmd runs HistoryStore.ClearHistory off Update's goroutine: it
// writes bbolt (AGENT.md §6.1, §13).
func clearHistoryCmd(h HistoryStore) tea.Cmd {
	return func() tea.Msg {
		return historyClearedMsg{err: h.ClearHistory()}
	}
}

// handleClearHistory implements `c` on the Search screen: open the confirm
// dialog, or say why not when there is nothing to clear.
func (m Model) handleClearHistory() (tea.Model, tea.Cmd) {
	if m.history == nil || len(m.search.recent) == 0 {
		return m.pushStatus("no recent searches to clear")
	}

	m.clearHistoryConfirm.Message = "Remove all recent searches?"
	m.clearHistoryConfirm = m.clearHistoryConfirm.Open()

	return m, nil
}

// handleClearHistoryConfirmAction routes a key's Action while the dialog
// (ContextClearHistoryConfirm) is open.
func (m Model) handleClearHistoryConfirmAction(action Action) (tea.Model, tea.Cmd) {
	switch action {
	case ActionMoveDown:
		m.clearHistoryConfirm = m.clearHistoryConfirm.MoveNext()
	case ActionMoveUp:
		m.clearHistoryConfirm = m.clearHistoryConfirm.MovePrev()
	case ActionCancel, ActionConfirmNo:
		m.clearHistoryConfirm = m.clearHistoryConfirm.Cancel()
	case ActionConfirmYes:
		var choice int
		m.clearHistoryConfirm, choice = m.clearHistoryConfirm.Confirm()

		if choice != clearHistoryDialogClear || m.history == nil {
			return m, nil
		}

		return m, clearHistoryCmd(m.history)
	default:
		// Every other key is unbound in this context.
	}

	return m, nil
}

// handleHistoryCleared applies clearHistoryCmd's outcome: on success the
// "Recent:" line goes away; on failure the list is left as it was.
func (m Model) handleHistoryCleared(msg historyClearedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m.pushStatus(fmt.Sprintf("couldn't clear recent searches: %v", msg.err))
	}

	m.search.recent = nil

	return m.pushStatus("recent searches cleared")
}

// renderClearHistoryConfirm draws the dialog plus its own key legend.
func (m Model) renderClearHistoryConfirm() string {
	var b strings.Builder

	b.WriteString(m.clearHistoryConfirm.View(m.theme, m.width))
	b.WriteString("\n\n")

	for _, line := range m.keys.HelpFor(ContextClearHistoryConfirm) {
		b.WriteString(m.theme.Muted.Render(line))
		b.WriteString("\n")
	}

	return b.String()
}
