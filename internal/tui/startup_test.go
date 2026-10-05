package tui

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/charmbracelet/x/exp/teatest"

	"github.com/kdta91/tortui/internal/indexer"
	indexerfake "github.com/kdta91/tortui/internal/indexer/fake"
)

// countingSaver is a SessionSaver that counts Save calls and returns err.
type countingSaver struct {
	mu    sync.Mutex
	saves int
	err   error
}

func (s *countingSaver) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.saves++

	return s.err
}

func (s *countingSaver) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.saves
}

// busyStatusBar pushes a message so the next Push queues instead of
// returning a real-time tick command, leaving only the command under test
// in a handler's tea.Batch.
func busyStatusBar(m Model) Model {
	m.statusBar, _ = m.statusBar.Push("busy")
	return m
}

// TestSessionSavedAfterSuccessfulAdd: the add flow's result handler saves
// the session once the engine accepted the torrent — after its own
// SetTorrent, since the save runs as the Cmd the handler returns.
func TestSessionSavedAfterSuccessfulAdd(t *testing.T) {
	saver := &countingSaver{}
	m := busyStatusBar(New(newTestEngine(t), testTheme(), WithSessionSaver(saver)))

	_, cmd := m.handleAddResult(addResultMsg{id: "t1", name: "t1.iso"})
	if saver.count() != 0 {
		t.Fatal("Save ran inside Update; it must run as a tea.Cmd")
	}

	if cmd == nil {
		t.Fatal("handleAddResult returned no command; want the session save")
	}

	if msg := cmd(); msg != nil {
		t.Fatalf("save cmd returned %#v, want nil on success", msg)
	}

	if saver.count() != 1 {
		t.Fatalf("Save calls = %d, want 1", saver.count())
	}
}

func TestSessionNotSavedAfterFailedAdd(t *testing.T) {
	saver := &countingSaver{}
	m := busyStatusBar(New(newTestEngine(t), testTheme(), WithSessionSaver(saver)))

	_, cmd := m.handleAddResult(addResultMsg{name: "t1.iso", err: errors.New("refused")})
	if cmd != nil {
		_ = cmd()
	}

	if saver.count() != 0 {
		t.Fatalf("Save calls = %d after a failed add, want 0", saver.count())
	}
}

func TestSessionSavedAfterSuccessfulRemove(t *testing.T) {
	saver := &countingSaver{}
	m := busyStatusBar(New(newTestEngine(t), testTheme(), WithSessionSaver(saver)))

	_, cmd := m.handleRemoveResult(removeResultMsg{id: "t1", name: "t1.iso"})
	if cmd == nil {
		t.Fatal("handleRemoveResult returned no command; want the session save")
	}

	_ = cmd()

	if saver.count() != 1 {
		t.Fatalf("Save calls = %d, want 1", saver.count())
	}
}

func TestSessionNotSavedAfterFailedRemove(t *testing.T) {
	saver := &countingSaver{}
	m := busyStatusBar(New(newTestEngine(t), testTheme(), WithSessionSaver(saver)))

	_, cmd := m.handleRemoveResult(removeResultMsg{id: "t1", name: "t1.iso", err: errors.New("gone")})
	if cmd != nil {
		_ = cmd()
	}

	if saver.count() != 0 {
		t.Fatalf("Save calls = %d after a failed remove, want 0", saver.count())
	}
}

// TestSessionSaveFailureIsReported: a failed save is surfaced on the status
// bar rather than swallowed (AGENT.md §6.9).
func TestSessionSaveFailureIsReported(t *testing.T) {
	msg := saveSessionCmd(&countingSaver{err: errors.New("disk full")})()

	tm, ok := msg.(transientMessageMsg)
	if !ok || !strings.Contains(tm.text, "couldn't save session: disk full") {
		t.Fatalf("save failure message = %#v", msg)
	}

	if saveSessionCmd(nil) != nil {
		t.Fatal("saveSessionCmd(nil) must be nil")
	}
}

// TestStartupNoticeShowsOnFirstRender: WithStartupNotice's text reaches the
// status bar through Init, with no key pressed.
func TestStartupNoticeShowsOnFirstRender(t *testing.T) {
	const notice = "1 resumed download has missing data"

	m := New(newTestEngine(t), testTheme(), WithStartupNotice(notice))
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	waitForOutput(t, tm, notice)
}

// TestStartupRunsNoQuery: the program opens on the Search screen and sends
// no query of any kind to a source until the user acts (T-9011). The startup
// notice proves Init has run and the first frames rendered.
func TestStartupRunsNoQuery(t *testing.T) {
	const notice = "startup settled"

	searcher := newStubSearcher(indexerfake.New("alpha", "Alpha", testCaps(true, true), nil))

	m := New(newTestEngine(t), testTheme(), WithSearcher(searcher), WithStartupNotice(notice))
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	waitForOutput(t, tm, notice)

	if n := searcher.callCount(); n != 0 {
		t.Fatalf("SearchAll calls after startup = %d, want 0", n)
	}

	if m.screen != ScreenSearch {
		t.Fatalf("initial screen = %v, want Search", m.screen)
	}
}

// TestLatestKeyStillRunsLatest: L after startup runs one Latest query, as
// before T-9011.
func TestLatestKeyStillRunsLatest(t *testing.T) {
	searcher := newStubSearcher(indexerfake.New("alpha", "Alpha", testCaps(true, true), nil))

	m := New(newTestEngine(t), testTheme(), WithSearcher(searcher))
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	tm.Send(keyRune("L"))
	waitForOutput(t, tm, "Sources queried")

	call, ok := searcher.lastCall()
	if !ok || call.q.Mode != indexer.ModeLatest || call.q.Text != "" {
		t.Fatalf("L query = %+v (called=%v), want one empty Latest query", call, ok)
	}

	if n := searcher.callCount(); n != 1 {
		t.Fatalf("SearchAll calls = %d, want exactly 1", n)
	}
}

// TestSearchResultAlwaysMovesToResults: a completed dispatch lands on
// Results wherever the user is when it arrives (T-9020: from Downloads too,
// so a stay-put guard coming back fails here).
func TestSearchResultAlwaysMovesToResults(t *testing.T) {
	for _, from := range []Screen{ScreenSearch, ScreenDownloads} {
		m := New(newTestEngine(t), testTheme())
		m.search.generation = 1
		m.screen = from

		updated, _ := m.handleSearchResult(searchResultMsg{gen: 1, query: indexer.Query{Mode: indexer.ModeLatest}})
		if got := updated.(Model).screen; got != ScreenResults {
			t.Fatalf("from %v: screen = %v, want Results", from, got)
		}
	}
}
