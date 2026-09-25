package tui

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/engine/fake"
	"github.com/kdta91/tortui/internal/store"
)

// recordingEngine wraps *fake.Engine, recording every Pause/Resume/Remove
// call and optionally failing them, so a test can assert exactly what
// reached the engine.
type recordingEngine struct {
	*fake.Engine

	mu        sync.Mutex
	calls     []string
	failPause error
}

func (e *recordingEngine) record(c string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, c)
}

func (e *recordingEngine) Pause(id string) error {
	e.record("pause " + id)
	if e.failPause != nil {
		return e.failPause
	}
	return e.Engine.Pause(id)
}

func (e *recordingEngine) Resume(id string) error {
	e.record("resume " + id)
	return e.Engine.Resume(id)
}

func (e *recordingEngine) Remove(id string, deleteData bool) error {
	if deleteData {
		e.record("remove-delete " + id)
	} else {
		e.record("remove-keep " + id)
	}
	return e.Engine.Remove(id, deleteData)
}

func (e *recordingEngine) Calls() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.calls...)
}

// newDownloadsTestModel returns a Model on ScreenDownloads whose snapshot is
// eng's current List().
func newDownloadsTestModel(t *testing.T, eng engine.Engine, opts ...Option) Model {
	t.Helper()

	m := New(eng, testTheme(), opts...)
	m.screen = ScreenDownloads
	m.width, m.height = 100, 40

	updated, _ := m.Update(engineUpdateMsg{statuses: eng.List()})

	return updated.(Model)
}

// press routes key through handleKey and returns the updated Model and cmd.
func press(t *testing.T, m Model, key tea.KeyMsg) (Model, tea.Cmd) {
	t.Helper()

	updated, cmd := m.handleKey(key)

	return updated.(Model), cmd
}

// runCmd executes cmd (when non-nil) and feeds its message back through
// Update, the way bubbletea's runtime would.
func runCmd(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()

	if cmd == nil {
		return m, nil
	}

	msg := cmd()
	if msg == nil {
		return m, nil
	}

	updated, next := m.Update(msg)

	return updated.(Model), next
}

func stateOf(t *testing.T, m Model, id string) engine.State {
	t.Helper()

	for _, s := range m.torrentStatuses {
		if s.ID == id {
			return s.State
		}
	}

	t.Fatalf("torrent %q not in snapshot", id)

	return 0
}

// --- p: pause/resume ------------------------------------------------------

// TestPauseIsOptimisticThenReconciledOnNextSnapshot: `p` moves the row to
// StatePaused before any engine call runs, and — once the call has
// returned — the next snapshot is authoritative.
func TestPauseIsOptimisticThenReconciledOnNextSnapshot(t *testing.T) {
	eng := &recordingEngine{Engine: fake.New()}
	t.Cleanup(func() { _ = eng.Close() })

	id := addFakeTorrent(t, eng.Engine, "magnet:?xt=urn:btih:p1&dn=a.iso", fake.Downloading(30*time.Second))
	eng.Advance(10 * time.Second)

	m := newDownloadsTestModel(t, eng)
	if got := stateOf(t, m, id); got != engine.StateDownloading {
		t.Fatalf("setup: state = %v, want downloading", got)
	}

	m, cmd := press(t, m, keyRune("p"))

	if got := stateOf(t, m, id); got != engine.StatePaused {
		t.Fatalf("state right after p = %v, want optimistic paused", got)
	}
	if len(eng.Calls()) != 0 {
		t.Fatalf("engine called inside Update: %v", eng.Calls())
	}
	if !strings.Contains(m.renderDownloadsScreen(), "[p] resume") {
		t.Error("row hint should offer resume right after the optimistic pause")
	}

	m, _ = runCmd(t, m, cmd)
	if got := eng.Calls(); len(got) != 1 || got[0] != "pause "+id {
		t.Fatalf("engine calls = %v, want [pause %s]", got, id)
	}

	// The next snapshot wins, whatever it says — here the engine reports
	// something different from the guess, which must replace it.
	snap := eng.List()
	snap[0].State = engine.StateQueued
	updated, _ := m.Update(engineUpdateMsg{statuses: snap})
	m = updated.(Model)

	if got := stateOf(t, m, id); got != engine.StateQueued {
		t.Fatalf("state after settled snapshot = %v, want the snapshot's queued", got)
	}
	if len(m.downloads.pending) != 0 {
		t.Fatalf("pending not cleared after reconciliation: %v", m.downloads.pending)
	}
}

// TestPauseOptimisticStateSurvivesSnapshotsWhileInFlight: a snapshot that
// arrives before the engine call has returned cannot reflect it, so the
// optimistic state is kept over it.
func TestPauseOptimisticStateSurvivesSnapshotsWhileInFlight(t *testing.T) {
	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	id := addFakeTorrent(t, eng, "magnet:?xt=urn:btih:p2&dn=b.iso", fake.Downloading(30*time.Second))
	eng.Advance(10 * time.Second)

	m := newDownloadsTestModel(t, eng)
	m, _ = press(t, m, keyRune("p")) // cmd deliberately not run: still in flight

	updated, _ := m.Update(engineUpdateMsg{statuses: eng.List()}) // stale: still downloading
	m = updated.(Model)

	if got := stateOf(t, m, id); got != engine.StatePaused {
		t.Fatalf("state after stale snapshot = %v, want optimistic paused kept", got)
	}
}

func TestResumeIsOptimisticAndCallsResume(t *testing.T) {
	eng := &recordingEngine{Engine: fake.New()}
	t.Cleanup(func() { _ = eng.Close() })

	id := addFakeTorrent(t, eng.Engine, "magnet:?xt=urn:btih:r1&dn=c.iso", fake.Downloading(30*time.Second))
	eng.Advance(10 * time.Second)
	if err := eng.Engine.Pause(id); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	m := newDownloadsTestModel(t, eng)
	m, cmd := press(t, m, keyRune("p"))

	if got := stateOf(t, m, id); got != engine.StateDownloading {
		t.Fatalf("state right after p on paused = %v, want optimistic downloading", got)
	}

	_, _ = runCmd(t, m, cmd)
	if got := eng.Calls(); len(got) != 1 || got[0] != "resume "+id {
		t.Fatalf("engine calls = %v, want [resume %s]", got, id)
	}
}

func TestResumeCompletedTorrentIsOptimisticallySeeding(t *testing.T) {
	statuses := []engine.TorrentStatus{{ID: "done", Name: "done.iso", State: engine.StatePaused, Progress: 1}}

	m := New(fake.New(), testTheme())
	m.screen = ScreenDownloads
	m.torrentStatuses = statuses

	m, _ = press(t, m, keyRune("p"))

	if got := stateOf(t, m, "done"); got != engine.StateSeeding {
		t.Fatalf("state = %v, want optimistic seeding for a completed torrent", got)
	}
	if statuses[0].State != engine.StatePaused {
		t.Fatal("optimistic update mutated the original snapshot slice in place")
	}
}

// TestPauseFailureRevertsOptimisticState: a failed engine call puts the row
// back and says why.
func TestPauseFailureRevertsOptimisticState(t *testing.T) {
	eng := &recordingEngine{Engine: fake.New(), failPause: errors.New("engine busy")}
	t.Cleanup(func() { _ = eng.Close() })

	id := addFakeTorrent(t, eng.Engine, "magnet:?xt=urn:btih:f1&dn=d.iso", fake.Downloading(30*time.Second))
	eng.Advance(10 * time.Second)

	m := newDownloadsTestModel(t, eng)
	m, cmd := press(t, m, keyRune("p"))
	m, _ = runCmd(t, m, cmd)

	if got := stateOf(t, m, id); got != engine.StateDownloading {
		t.Fatalf("state after failed pause = %v, want reverted to downloading", got)
	}
	if len(m.downloads.pending) != 0 {
		t.Fatalf("pending left behind after failure: %v", m.downloads.pending)
	}
	if !strings.Contains(m.statusBar.View(200, "downloads", m.theme), "couldn't pause d.iso: engine busy") {
		t.Errorf("status bar = %q, want the pause failure", m.statusBar.View(200, "downloads", m.theme))
	}
}

// TestSecondPauseWhileInFlightIsIgnored: two engine calls on separate
// goroutines have no ordering guarantee, so p is not re-issued until the
// first returns.
func TestSecondPauseWhileInFlightIsIgnored(t *testing.T) {
	eng := &recordingEngine{Engine: fake.New()}
	t.Cleanup(func() { _ = eng.Close() })

	id := addFakeTorrent(t, eng.Engine, "magnet:?xt=urn:btih:d1&dn=e.iso", fake.Downloading(30*time.Second))
	eng.Advance(10 * time.Second)

	m := newDownloadsTestModel(t, eng)
	m, first := press(t, m, keyRune("p"))
	m, second := press(t, m, keyRune("p"))

	if got := stateOf(t, m, id); got != engine.StatePaused {
		t.Fatalf("state = %v, want still the first press's paused", got)
	}

	// The second press yields only the status-bar notice (its cmd is that
	// message's timeout tick, not run here), never a second engine call.
	if second == nil {
		t.Fatal("second p should queue a status-bar notice")
	}
	if bar := m.statusBar.View(200, "downloads", m.theme); !strings.Contains(bar, "still applying") {
		t.Errorf("status bar = %q, want the in-flight notice", bar)
	}

	m, _ = runCmd(t, m, first)
	if !m.downloads.pending[id].settled {
		t.Fatal("first call's result should have settled the pending entry")
	}

	// Settled: p is accepted again.
	_, cmd := press(t, m, keyRune("p"))
	_, _ = runCmd(t, m, cmd)
	if got := eng.Calls(); len(got) != 2 || got[1] != "resume "+id {
		t.Fatalf("engine calls = %v, want a resume after the pause settled", got)
	}
}

func TestPauseOnErroredTorrentDoesNothing(t *testing.T) {
	eng := &recordingEngine{Engine: fake.New()}
	t.Cleanup(func() { _ = eng.Close() })

	id := addFakeTorrent(t, eng.Engine, "magnet:?xt=urn:btih:e1&dn=f.iso", fake.Errored(time.Second, errors.New("disk full")))
	eng.Advance(time.Second)

	m := newDownloadsTestModel(t, eng)
	m, _ = press(t, m, keyRune("p")) // cmd is only the notice's timeout tick

	if got := stateOf(t, m, id); got != engine.StateErrored {
		t.Fatalf("state = %v, want errored unchanged", got)
	}
	if len(m.downloads.pending) != 0 {
		t.Fatalf("p on errored left a pending toggle: %v", m.downloads.pending)
	}
	if bar := m.statusBar.View(200, "downloads", m.theme); !strings.Contains(bar, "nothing to pause or resume") {
		t.Errorf("status bar = %q, want the errored notice", bar)
	}
}

func TestActionsOnEmptyDownloadsScreenAreNoOps(t *testing.T) {
	m := New(fake.New(), testTheme())
	m.screen = ScreenDownloads

	for _, k := range []string{"p", "x", "u"} {
		updated, cmd := press(t, m, keyRune(k))
		if cmd != nil {
			t.Errorf("%s on an empty screen returned a cmd", k)
		}
		if updated.removeConfirm.IsOpen() {
			t.Errorf("%s on an empty screen opened the remove dialog", k)
		}
	}
}

// --- vanished torrents ----------------------------------------------------

// TestPauseOnVanishedTorrentFailsGracefully: the row is still on screen
// (the model's snapshot predates the removal) when p is pressed; the engine
// no longer knows the ID. No panic, the optimistic state is dropped, the
// failure is reported, and the next snapshot removes the row.
func TestPauseOnVanishedTorrentFailsGracefully(t *testing.T) {
	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	id := addFakeTorrent(t, eng, "magnet:?xt=urn:btih:v1&dn=gone.iso", fake.Downloading(30*time.Second))
	eng.Advance(10 * time.Second)

	m := newDownloadsTestModel(t, eng)

	if err := eng.Remove(id, false); err != nil { // vanishes between render and keypress
		t.Fatalf("Remove: %v", err)
	}

	m, cmd := press(t, m, keyRune("p"))
	m, _ = runCmd(t, m, cmd)

	if len(m.downloads.pending) != 0 {
		t.Fatalf("pending left behind: %v", m.downloads.pending)
	}
	if bar := m.statusBar.View(200, "downloads", m.theme); !strings.Contains(bar, "couldn't pause gone.iso") {
		t.Errorf("status bar = %q, want the failure reported", bar)
	}

	updated, _ := m.Update(engineUpdateMsg{statuses: eng.List()})
	m = updated.(Model)
	if len(m.downloadRows()) != 0 {
		t.Fatalf("rows = %d, want the vanished torrent gone after the next snapshot", len(m.downloadRows()))
	}
}

// TestPendingDroppedWhenTorrentLeavesSnapshot: an in-flight toggle whose
// torrent disappears from the snapshot leaves nothing behind.
func TestPendingDroppedWhenTorrentLeavesSnapshot(t *testing.T) {
	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	addFakeTorrent(t, eng, "magnet:?xt=urn:btih:v2&dn=gone2.iso", fake.Downloading(30*time.Second))

	m := newDownloadsTestModel(t, eng)
	m, cmd := press(t, m, keyRune("p"))

	updated, _ := m.Update(engineUpdateMsg{statuses: nil})
	m = updated.(Model)
	if len(m.downloads.pending) != 0 {
		t.Fatalf("pending = %v, want dropped once the torrent left the snapshot", m.downloads.pending)
	}

	// The engine call's late result (success here) must not panic or
	// resurrect anything.
	m, _ = runCmd(t, m, cmd)
	if len(m.downloads.pending) != 0 || len(m.torrentStatuses) != 0 {
		t.Fatalf("late result resurrected state: pending=%v statuses=%v", m.downloads.pending, m.torrentStatuses)
	}
}

// --- x: remove ------------------------------------------------------------

func TestRemoveOpensDialogDefaultingToCancel(t *testing.T) {
	eng := &recordingEngine{Engine: fake.New()}
	t.Cleanup(func() { _ = eng.Close() })

	addFakeTorrent(t, eng.Engine, "magnet:?xt=urn:btih:x1&dn=g.iso", nil)

	m := newDownloadsTestModel(t, eng)
	m, _ = press(t, m, keyRune("x"))

	if !m.removeConfirm.IsOpen() {
		t.Fatal("x did not open the remove dialog")
	}
	if m.context() != ContextRemoveConfirm {
		t.Fatalf("context = %v, want %v", m.context(), ContextRemoveConfirm)
	}

	view := m.View()
	for _, want := range []string{"Remove, keep data", "Remove and delete data", "Cancel (default)", "g.iso"} {
		if !strings.Contains(view, want) {
			t.Errorf("dialog view missing %q:\n%s", want, view)
		}
	}

	// enter with no other input takes the default: cancel.
	m, cmd := press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = runCmd(t, m, cmd)

	if m.removeConfirm.IsOpen() {
		t.Fatal("dialog still open after enter")
	}
	if len(eng.Calls()) != 0 {
		t.Fatalf("default choice reached the engine: %v", eng.Calls())
	}
}

func TestRemoveDialogChoices(t *testing.T) {
	cases := []struct {
		name  string
		moves []string // keys pressed before enter
		want  string   // engine call prefix, "" for none
	}{
		{"keep data", []string{"j"}, "remove-keep "},     // cancel -> wraps to keep
		{"delete data", []string{"k"}, "remove-delete "}, // cancel -> up to delete
		{"delete via down twice", []string{"j", "j"}, "remove-delete "},
		{"cancel after moving back", []string{"j", "k"}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng := &recordingEngine{Engine: fake.New()}
			t.Cleanup(func() { _ = eng.Close() })

			id := addFakeTorrent(t, eng.Engine, "magnet:?xt=urn:btih:x2&dn=h.iso", nil)

			m := newDownloadsTestModel(t, eng)
			m, _ = press(t, m, keyRune("x"))

			for _, k := range tc.moves {
				m, _ = press(t, m, keyRune(k))
			}

			m, cmd := press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			_, _ = runCmd(t, m, cmd)

			calls := eng.Calls()
			if tc.want == "" {
				if len(calls) != 0 {
					t.Fatalf("engine calls = %v, want none", calls)
				}
				return
			}
			if len(calls) != 1 || calls[0] != tc.want+id {
				t.Fatalf("engine calls = %v, want [%s%s]", calls, tc.want, id)
			}
		})
	}
}

func TestRemoveDialogEscCancels(t *testing.T) {
	eng := &recordingEngine{Engine: fake.New()}
	t.Cleanup(func() { _ = eng.Close() })

	addFakeTorrent(t, eng.Engine, "magnet:?xt=urn:btih:x3&dn=i.iso", nil)

	m := newDownloadsTestModel(t, eng)
	m, _ = press(t, m, keyRune("x"))
	m, _ = press(t, m, keyRune("j")) // highlight keep data
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.removeConfirm.IsOpen() || m.removeTarget != (removeTarget{}) {
		t.Fatal("esc did not close and clear the dialog")
	}
	if m.screen != ScreenDownloads {
		t.Fatalf("screen = %v, want downloads", m.screen)
	}

	// Reopening starts fresh at the default.
	m, _ = press(t, m, keyRune("x"))
	if m.removeConfirm.SelectedIndex() != removeDialogCancel {
		t.Fatalf("reopened dialog highlights %d, want cancel", m.removeConfirm.SelectedIndex())
	}
	if len(eng.Calls()) != 0 {
		t.Fatalf("engine calls = %v, want none", eng.Calls())
	}
}

// TestRemoveTargetsTorrentCapturedAtOpen: a snapshot that reorders the rows
// while the dialog is open must not redirect the removal to whatever the
// cursor now points at.
func TestRemoveTargetsTorrentCapturedAtOpen(t *testing.T) {
	eng := &recordingEngine{Engine: fake.New()}
	t.Cleanup(func() { _ = eng.Close() })

	first := addFakeTorrent(t, eng.Engine, "magnet:?xt=urn:btih:o1&dn=first.iso", nil)
	second := addFakeTorrent(t, eng.Engine, "magnet:?xt=urn:btih:o2&dn=second.iso", nil)

	m := newDownloadsTestModel(t, eng)
	m, _ = press(t, m, keyRune("x")) // cursor 0 = first

	snap := eng.List()
	snap[0], snap[1] = snap[1], snap[0]
	updated, _ := m.Update(engineUpdateMsg{statuses: snap})
	m = updated.(Model)

	m, _ = press(t, m, keyRune("j")) // keep data
	m, cmd := press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	_, _ = runCmd(t, m, cmd)

	if got := eng.Calls(); len(got) != 1 || got[0] != "remove-keep "+first {
		t.Fatalf("engine calls = %v, want [remove-keep %s] (not %s)", got, first, second)
	}
}

// TestRemoveOfTorrentThatVanishedWhileDialogOpen: the torrent leaves the
// snapshot while the dialog is open; confirming reports it instead of
// calling the engine.
func TestRemoveOfTorrentThatVanishedWhileDialogOpen(t *testing.T) {
	eng := &recordingEngine{Engine: fake.New()}
	t.Cleanup(func() { _ = eng.Close() })

	addFakeTorrent(t, eng.Engine, "magnet:?xt=urn:btih:g1&dn=vanish.iso", nil)

	m := newDownloadsTestModel(t, eng)
	m, _ = press(t, m, keyRune("x"))

	updated, _ := m.Update(engineUpdateMsg{statuses: nil})
	m = updated.(Model)

	m, _ = press(t, m, keyRune("j"))
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if len(eng.Calls()) != 0 {
		t.Fatalf("engine calls = %v, want none for a vanished torrent", eng.Calls())
	}
	if bar := m.statusBar.View(200, "downloads", m.theme); !strings.Contains(bar, "vanish.iso is no longer in the download list") {
		t.Errorf("status bar = %q, want the vanished notice", bar)
	}
}

// TestRemoveEngineFailureIsReported covers the other half of the vanish
// race: the model's snapshot still has the row, the engine does not.
func TestRemoveEngineFailureIsReported(t *testing.T) {
	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	id := addFakeTorrent(t, eng, "magnet:?xt=urn:btih:g2&dn=race.iso", nil)

	m := newDownloadsTestModel(t, eng)
	m, _ = press(t, m, keyRune("x"))

	if err := eng.Remove(id, false); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	m, _ = press(t, m, keyRune("j"))
	m, cmd := press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = runCmd(t, m, cmd)

	if bar := m.statusBar.View(200, "downloads", m.theme); !strings.Contains(bar, "couldn't remove race.iso") {
		t.Errorf("status bar = %q, want the failure reported", bar)
	}
}

func TestRemoveSuccessReportsAndClearsRowState(t *testing.T) {
	m := New(fake.New(), testTheme())
	m.downloads.expandedErr = "t1"
	m.downloads.pending = map[string]pendingToggle{"t1": {optimistic: engine.StatePaused, settled: true}}

	updated, _ := m.Update(removeResultMsg{id: "t1", name: "t1.iso", deleteData: true})
	m = updated.(Model)

	if m.downloads.expandedErr != "" || len(m.downloads.pending) != 0 {
		t.Fatal("remove success left per-row state behind")
	}
	if bar := m.statusBar.View(200, "downloads", m.theme); !strings.Contains(bar, "removed t1.iso and deleted its data") {
		t.Errorf("status bar = %q", bar)
	}
}

// --- u: open source page ----------------------------------------------------

func TestOpenSourceOnDownloadsUsesLiveOrigin(t *testing.T) {
	var got []string
	open := func(u string) error { got = append(got, u); return nil }

	m := New(fake.New(), testTheme(), WithOpenURL(open))
	m.screen = ScreenDownloads
	m.torrentStatuses = []engine.TorrentStatus{{
		ID: "t1", Name: "a.iso", State: engine.StateDownloading,
		Origin: engine.Origin{SourceURL: "https://example.org/t/1"},
	}}

	m, cmd := press(t, m, keyRune("u"))
	_, _ = runCmd(t, m, cmd)

	if len(got) != 1 || got[0] != "https://example.org/t/1" {
		t.Fatalf("openURL calls = %v, want the live origin's page", got)
	}
}

func TestOpenSourceOnDownloadsFallsBackToStoreRecord(t *testing.T) {
	var got []string
	open := func(u string) error { got = append(got, u); return nil }

	ts := &stubTorrentStore{records: []store.TorrentRecord{{ID: "t1", SourceURL: "https://example.org/t/stored"}}}

	m := New(fake.New(), testTheme(), WithOpenURL(open), WithTorrentStore(ts))
	m.screen = ScreenDownloads
	m.torrentStatuses = []engine.TorrentStatus{{ID: "t1", Name: "a.iso", State: engine.StateDownloading}}

	m, cmd := press(t, m, keyRune("u"))
	_, _ = runCmd(t, m, cmd)

	if len(got) != 1 || got[0] != "https://example.org/t/stored" {
		t.Fatalf("openURL calls = %v, want the stored record's page", got)
	}
}

func TestOpenSourceOnDownloadsWithNoPageSaysSo(t *testing.T) {
	calls := 0
	open := func(string) error { calls++; return nil }

	m := New(fake.New(), testTheme(), WithOpenURL(open), WithTorrentStore(&stubTorrentStore{}))
	m.screen = ScreenDownloads
	m.torrentStatuses = []engine.TorrentStatus{{ID: "t1", Name: "bare.iso", State: engine.StateDownloading}}

	m, _ = press(t, m, keyRune("u")) // cmd is only the notice's timeout tick

	if calls != 0 {
		t.Fatalf("openURL called %d times, want 0", calls)
	}
	if bar := m.statusBar.View(200, "downloads", m.theme); !strings.Contains(bar, "no source page for bare.iso") {
		t.Errorf("status bar = %q", bar)
	}
}

func TestRemoveConfirmKeymapHasNoConflictsAndNoDeleteShortcut(t *testing.T) {
	if c := Conflicts(AllBindings()); len(c) != 0 {
		t.Fatalf("keymap conflicts: %v", c)
	}

	km := NewKeyMap()
	if a, ok := km.Lookup(ContextRemoveConfirm, "enter"); !ok || a != ActionConfirmYes {
		t.Fatalf("enter in remove dialog = %v %v", a, ok)
	}
	// Screen keys must not leak into the modal.
	for _, k := range []string{"q", "y", "x", "p", "tab", "d"} {
		if a, ok := km.Lookup(ContextRemoveConfirm, k); ok {
			t.Errorf("key %q is bound to %v inside the remove dialog", k, a)
		}
	}
}

// --- end-to-end (teatest) ---------------------------------------------------

// TestDownloadActionsEndToEnd drives pause, resume, and remove-keeping-data
// through a real teatest program against internal/engine/fake.
func TestDownloadActionsEndToEnd(t *testing.T) {
	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	eng.ScriptFor = func(engine.AddSource) fake.Script { return fake.Downloading(time.Minute) }

	m := New(eng, testTheme())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(100, 40))
	t.Cleanup(func() { _ = tm.Quit() })

	waitForOutput(t, tm, "Query:")

	id := addFakeTorrent(t, eng, "magnet:?xt=urn:btih:e2e&dn=walk.iso", fake.Downloading(time.Minute))
	eng.Advance(10 * time.Second)

	tm.Send(keyRune("4"))
	waitForAllOutput(t, tm, "walk.iso", "[p]ause")

	tm.Send(keyRune("p"))
	waitForOutput(t, tm, "[p] resume")
	waitForPredicate(t, func() bool { return engineState(eng, id) == engine.StatePaused })

	tm.Send(keyRune("p"))
	waitForOutput(t, tm, "[p]ause")
	waitForPredicate(t, func() bool {
		s := engineState(eng, id)
		return s != engine.StatePaused && s != engine.StateErrored
	})

	tm.Send(keyRune("x"))
	waitForOutput(t, tm, "Remove and delete data")
	tm.Send(keyRune("j")) // cancel -> keep data
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "No downloads yet")

	if len(eng.List()) != 0 {
		t.Fatalf("engine still tracks %v", eng.List())
	}
}

func engineState(eng *fake.Engine, id string) engine.State {
	for _, s := range eng.List() {
		if s.ID == id {
			return s.State
		}
	}

	return -1
}

// waitForPredicate polls cond (never an exact intermediate state) until it
// holds or a generous deadline passes.
func waitForPredicate(t *testing.T, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}

	t.Fatal("condition not met before deadline")
}
