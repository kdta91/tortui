package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	indexerfake "github.com/kdta91/tortui/internal/indexer/fake"
	"github.com/kdta91/tortui/internal/store"
)

func newClearTestModel(t *testing.T, hist *stubHistory) Model {
	t.Helper()

	searcher := newStubSearcher(indexerfake.New("alpha", "Alpha", testCaps(true, true), nil))
	m := New(newTestEngine(t), testTheme(), WithSearcher(searcher), WithHistory(hist))
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	return updated.(Model)
}

func keyThen(t *testing.T, m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	t.Helper()

	updated, cmd := m.Update(msg)

	return updated.(Model), cmd
}

func historyWith(texts ...string) *stubHistory {
	h := &stubHistory{}
	for _, tx := range texts {
		h.entries = append(h.entries, store.HistoryEntry{Text: tx})
	}

	return h
}

func TestClearHistoryConfirmClearsAndHidesRecent(t *testing.T) {
	hist := historyWith("ubuntu", "debian")
	m := newClearTestModel(t, hist)

	if !strings.Contains(m.View(), "Recent:") {
		t.Fatalf("precondition: View() lacks Recent line:\n%s", m.View())
	}

	m, cmd := keyThen(t, m, keyRune("c"))
	if cmd != nil || m.context() != ContextClearHistoryConfirm {
		t.Fatalf("c: context = %v, cmd = %v; want the confirm dialog and no cmd", m.context(), cmd)
	}

	if hist.clearCalls != 0 {
		t.Fatal("ClearHistory ran before the user confirmed")
	}

	// Move off the default (Cancel) onto Clear, then confirm.
	m, _ = keyThen(t, m, keyRune("k"))
	m, cmd = keyThen(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("confirm must return a tea.Cmd: the bbolt write must not run inside Update")
	}

	if hist.clearCalls != 0 {
		t.Fatal("ClearHistory ran inside Update instead of the returned tea.Cmd")
	}

	msg := cmd()
	if hist.clearCalls != 1 {
		t.Fatalf("clearCalls = %d after running the cmd, want 1", hist.clearCalls)
	}

	updated, _ := m.Update(msg)
	m = updated.(Model)

	if strings.Contains(m.View(), "Recent:") {
		t.Fatalf("Recent line still shown after clear:\n%s", m.View())
	}

	if len(hist.ListHistory()) != 0 {
		t.Fatal("history not empty after clear")
	}
}

func TestClearHistoryCancelKeepsHistory(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEsc}, keyRune("n"), {Type: tea.KeyEnter}} {
		hist := historyWith("ubuntu")
		m := newClearTestModel(t, hist)

		m, _ = keyThen(t, m, keyRune("c"))
		m, cmd := keyThen(t, m, key)

		if cmd != nil {
			t.Fatalf("key %q: unexpected cmd", key.String())
		}

		if m.context() != screenContext(ScreenSearch) {
			t.Fatalf("key %q: dialog still open (%v)", key.String(), m.context())
		}

		if hist.clearCalls != 0 || len(hist.ListHistory()) != 1 {
			t.Fatalf("key %q: cancel changed history", key.String())
		}

		if !strings.Contains(m.View(), "Recent: ubuntu") {
			t.Fatalf("key %q: Recent line lost:\n%s", key.String(), m.View())
		}
	}
}

func TestClearHistoryFailureKeepsRecentAndReports(t *testing.T) {
	hist := historyWith("ubuntu")
	hist.clearErr = errors.New("disk full")
	m := newClearTestModel(t, hist)

	updated, _ := m.Update(historyClearedMsg{err: hist.clearErr})
	m = updated.(Model)

	view := m.View()
	if !strings.Contains(view, "couldn't clear recent") || !strings.Contains(view, "Recent: ubuntu") {
		t.Fatalf("View() = %q, want the error and the unchanged Recent line", view)
	}
}

func TestClearHistoryWithNothingToClearOpensNoDialog(t *testing.T) {
	m := newClearTestModel(t, historyWith())

	m, _ = keyThen(t, m, keyRune("c"))
	if m.context() != screenContext(ScreenSearch) {
		t.Fatalf("context = %v, want no dialog when history is empty", m.context())
	}
}

func TestClearHistoryKeyTypesIntoQueryWhileEditing(t *testing.T) {
	hist := historyWith("ubuntu")
	m := newClearTestModel(t, hist)

	m, _ = keyThen(t, m, keyRune("/"))
	m, _ = keyThen(t, m, keyRune("c"))

	if m.search.query != "c" || m.context() != screenContext(ScreenSearch) {
		t.Fatalf("query = %q, context = %v; c must type while editing", m.search.query, m.context())
	}
}

func TestSearchScreenShowsSourceToggleHint(t *testing.T) {
	m := newClearTestModel(t, historyWith())

	if !strings.Contains(m.View(), "space on a source row toggles it in or out of the search") {
		t.Fatalf("View() lacks the source-toggle hint:\n%s", m.View())
	}

	if strings.Contains(m.View(), "c clear recent") {
		t.Fatal("clear hint shown with no history")
	}

	m = newClearTestModel(t, historyWith("x"))
	if !strings.Contains(m.View(), "c clear recent") {
		t.Fatalf("View() lacks the clear hint with history:\n%s", m.View())
	}
}

func TestClearHistoryHelpOverlayListsKey(t *testing.T) {
	m := newClearTestModel(t, historyWith("x"))
	m, _ = keyThen(t, m, keyRune("?"))

	if !strings.Contains(m.View(), "clear recent searches") {
		t.Fatalf("help overlay lacks the clear-history binding:\n%s", m.View())
	}

	if got := strings.Count(m.View(), "\n") + 1; got > 24 {
		t.Fatalf("search help overlay renders %d lines, over the 24-line floor", got)
	}
}
