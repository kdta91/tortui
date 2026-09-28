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

// TestStartupLatestRunsOneLatestQuery: WithStartupLatest runs exactly one
// Latest query as the program starts and lands on Results, with no key
// pressed.
func TestStartupLatestRunsOneLatestQuery(t *testing.T) {
	searcher := newStubSearcher(indexerfake.New("alpha", "Alpha", testCaps(true, true), nil))

	m := New(newTestEngine(t), testTheme(), WithSearcher(searcher), WithStartupLatest(true))
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	waitForOutput(t, tm, "Sources queried")

	call, ok := searcher.lastCall()
	if !ok || call.q.Mode != indexer.ModeLatest || call.q.Text != "" {
		t.Fatalf("startup query = %+v (called=%v), want one empty Latest query", call, ok)
	}

	if n := searcher.callCount(); n != 1 {
		t.Fatalf("SearchAll calls = %d, want exactly 1", n)
	}
}

// TestNoStartupLatestByDefault: without the option, nothing is queried.
func TestNoStartupLatestByDefault(t *testing.T) {
	searcher := newStubSearcher(indexerfake.New("alpha", "Alpha", testCaps(true, true), nil))
	m := New(nil, testTheme(), WithSearcher(searcher))

	if cmd := m.Init(); cmd != nil {
		t.Fatalf("Init with no engine and no startup options returned %#v, want nil", cmd)
	}
}

// TestStartupLatestSilentWithoutALatestSource: a source set that cannot
// serve Latest runs nothing and pushes no "select at least one source".
func TestStartupLatestSilentWithoutALatestSource(t *testing.T) {
	searcher := newStubSearcher(indexerfake.New("bravo", "Bravo", testCaps(true, false), nil))
	m := New(newTestEngine(t), testTheme(), WithSearcher(searcher), WithStartupLatest(true))

	updated, cmd := m.Update(startupLatestMsg{})
	if cmd != nil {
		t.Fatalf("startup Latest with no Latest-capable source returned a command")
	}

	if got := updated.(Model).statusBar.View(200, "search", m.theme); strings.Contains(got, "select at least one source") {
		t.Fatalf("status bar = %q, want no prompt", got)
	}
}

// TestStartupLatestResultDoesNotYankTheUser: the startup query's result
// moves the user to Results only if they are still on Search; a normal
// dispatch's result always does.
func TestStartupLatestResultDoesNotYankTheUser(t *testing.T) {
	m := New(newTestEngine(t), testTheme())
	m.search.generation = 1
	m.startupGen = 1
	m.screen = ScreenDownloads

	updated, _ := m.handleSearchResult(searchResultMsg{gen: 1, query: indexer.Query{Mode: indexer.ModeLatest}})
	if got := updated.(Model).screen; got != ScreenDownloads {
		t.Fatalf("screen = %v after the startup result, want Downloads", got)
	}

	m.screen = ScreenSearch

	updated, _ = m.handleSearchResult(searchResultMsg{gen: 1, query: indexer.Query{Mode: indexer.ModeLatest}})
	if got := updated.(Model).screen; got != ScreenResults {
		t.Fatalf("screen = %v after the startup result on Search, want Results", got)
	}

	m.search.generation = 2
	m.screen = ScreenDownloads

	updated, _ = m.handleSearchResult(searchResultMsg{gen: 2, query: indexer.Query{Mode: indexer.ModeLatest}})
	if got := updated.(Model).screen; got != ScreenResults {
		t.Fatalf("screen = %v after a user dispatch, want Results", got)
	}
}
