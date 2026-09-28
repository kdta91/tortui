package lifecycle

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/engine/fake"
	"github.com/kdta91/tortui/internal/store"
)

// gatedEngine is an engine.Engine and engine.Resumer whose tracked torrents
// the test sets directly, which can park ResumeData for one id until the
// test releases it, and which counts every List or ResumeData call that
// starts or finishes after Close — the calls T-994 forbids. engine/fake
// supplies the rest of the interface. Every field is guarded by mu, so a
// Session saving on several goroutines is race-clean against it.
type gatedEngine struct {
	*fake.Engine

	mu         sync.Mutex
	ids        []string
	closed     bool
	lists      int
	afterClose int
	parkID     string
	parked     chan struct{}
	release    chan struct{}
}

func newGatedEngine(t *testing.T) *gatedEngine {
	t.Helper()

	g := &gatedEngine{Engine: fake.New()}
	t.Cleanup(func() { _ = g.Close() })

	return g
}

// track makes the engine report id, as if an add had just been accepted.
func (g *gatedEngine) track(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.ids = append(g.ids, id)
}

// park makes the next ResumeData(id) block until release is closed; parked
// is closed once it is blocked.
func (g *gatedEngine) park(id string) (parked <-chan struct{}, release chan<- struct{}) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.parkID = id
	g.parked = make(chan struct{})
	g.release = make(chan struct{})

	return g.parked, g.release
}

func (g *gatedEngine) counts() (lists, afterClose int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.lists, g.afterClose
}

func (g *gatedEngine) isClosed() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	return g.closed
}

func (g *gatedEngine) List() []engine.TorrentStatus {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.lists++
	if g.closed {
		g.afterClose++
	}

	out := make([]engine.TorrentStatus, 0, len(g.ids))
	for _, id := range g.ids {
		out = append(out, engine.TorrentStatus{ID: id, Name: id, State: engine.StateDownloading})
	}

	return out
}

func (g *gatedEngine) ResumeData(id string) (engine.ResumeData, error) {
	g.mu.Lock()
	if g.closed {
		g.afterClose++
	}

	var parked, release chan struct{}
	if id == g.parkID {
		parked, release = g.parked, g.release
		g.parkID = ""
	}
	g.mu.Unlock()

	if parked != nil {
		close(parked)
		<-release

		g.mu.Lock()
		if g.closed {
			g.afterClose++
		}
		g.mu.Unlock()
	}

	return engine.ResumeData{ID: id, Name: id, Magnet: "magnet:?xt=urn:btih:" + id}, nil
}

func (g *gatedEngine) Restore(_ context.Context, d engine.ResumeData) (string, error) {
	g.track(d.ID)

	return d.ID, nil
}

func (g *gatedEngine) Close() error {
	g.mu.Lock()
	g.closed = true
	g.mu.Unlock()

	return g.Engine.Close()
}

// resumedSession returns a Session over a fresh store and eng, resumed so
// Save does real work.
func resumedSession(t *testing.T, eng engine.Engine) (*Session, *store.Store) {
	t.Helper()

	st := openStore(t, filepath.Join(t.TempDir(), "tortui.db"))
	s := NewSession(eng, st, quietLogger())

	if _, err := s.Resume(context.Background()); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	return s, st
}

// TestSessionSavesRacingShutdownNeverOutliveIt runs saves on several
// goroutines — as saveSessionCmd does — before, during, and after Shutdown,
// and checks no save reaches the engine after it closed and each save after
// Shutdown is a quiet no-op (T-994).
func TestSessionSavesRacingShutdownNeverOutliveIt(t *testing.T) {
	eng := newGatedEngine(t)
	s, st := resumedSession(t, eng)
	eng.track("a")

	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	stop := make(chan struct{})
	errs := make(chan error, 64)

	var wg sync.WaitGroup

	for range 8 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for {
				select {
				case <-stop:
					// One more, strictly after Shutdown returned.
					errs <- s.Save()
					return
				default:
					if err := s.Save(); err != nil {
						errs <- err
						return
					}
				}
			}
		}()
	}

	shutdownErrs := Shutdown(ShutdownOptions{Engine: eng, Store: st, Session: s, Logger: quietLogger()})
	close(stop)
	wg.Wait()
	close(errs)

	if len(shutdownErrs) != 0 {
		t.Fatalf("Shutdown: %v", shutdownErrs)
	}

	for err := range errs {
		if err != nil {
			t.Fatalf("a Save racing Shutdown failed: %v", err)
		}
	}

	if _, after := eng.counts(); after != 0 {
		t.Fatalf("%d engine calls from a Save after the engine closed, want 0", after)
	}

	if err := s.SetTorrent(store.TorrentRecord{ID: "late"}); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("SetTorrent after Shutdown = %v, want ErrSessionClosed", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("second Close = %v, want nil", err)
	}
}

// TestShutdownWaitsForASaveInFlight parks a save inside the engine, starts
// Shutdown, and checks the engine is closed only after that save finished,
// and that the final save still ran (T-994).
func TestShutdownWaitsForASaveInFlight(t *testing.T) {
	eng := newGatedEngine(t)
	s, st := resumedSession(t, eng)
	eng.track("a")

	parked, release := eng.park("a")
	saved := make(chan error, 1)

	go func() { saved <- s.Save() }()

	<-parked

	done := make(chan []error, 1)

	go func() {
		done <- Shutdown(ShutdownOptions{Engine: eng, Store: st, Session: s, Logger: quietLogger()})
	}()

	// Shutdown's pause step lists the engine (the third List, after
	// Resume's and the parked save's); its Close step comes next and must
	// wait behind the parked save. A Close that does not take the lock
	// lists the engine again at once, and Shutdown then closes the engine
	// and returns. The window only gives such a Close time to show itself;
	// a correct one never gets past the lock before release.
	waitUntil(t, "Shutdown's pause step", func() bool { lists, _ := eng.counts(); return lists >= 3 })

	window := time.After(100 * time.Millisecond)

	for blocked := true; blocked; {
		select {
		case errs := <-done:
			t.Fatalf("Shutdown returned (%v) while a save was in flight", errs)
		case <-window:
			blocked = false
		case <-time.After(time.Millisecond):
		}

		if lists, _ := eng.counts(); lists != 3 {
			t.Fatalf("engine List calls = %d while a save was in flight, want 3: Close saved without waiting for it", lists)
		}

		if eng.isClosed() {
			t.Fatal("engine closed while a save was in flight")
		}
	}

	close(release)

	if err := <-saved; err != nil {
		t.Fatalf("Save in flight: %v", err)
	}

	if errs := <-done; len(errs) != 0 {
		t.Fatalf("Shutdown: %v", errs)
	}

	lists, after := eng.counts()
	if after != 0 {
		t.Fatalf("%d engine calls from a Save after the engine closed, want 0", after)
	}

	// resumedSession's Resume, the parked save, Shutdown's pause, and
	// Close's final save: the final save ran, behind the parked one.
	if lists != 4 {
		t.Fatalf("engine List calls = %d, want 4 (the final save must still run)", lists)
	}
}

// TestShutdownSealsTheSessionWhenItsSaveTimesOut parks a save past
// Shutdown's step timeout: Shutdown moves on and closes the store and the
// engine, and once the hung save returns, nothing left queued on the session
// — Shutdown's own Close included — runs against them (T-994).
func TestShutdownSealsTheSessionWhenItsSaveTimesOut(t *testing.T) {
	eng := newGatedEngine(t)
	s, st := resumedSession(t, eng)
	eng.track("a")

	parked, release := eng.park("a")
	saved := make(chan error, 1)

	go func() { saved <- s.Save() }()

	<-parked

	errs := Shutdown(ShutdownOptions{
		Engine: eng, Store: st, Session: s, Logger: quietLogger(),
		StepTimeout: 50 * time.Millisecond,
	})

	if len(errs) != 1 || !errors.Is(errs[0], ErrStepTimedOut) {
		t.Fatalf("Shutdown errors = %v, want the save step timed out", errs)
	}

	close(release)

	// The hung save itself finishes against the closed store: the
	// documented cost of abandoning a step (ErrStepTimedOut).
	if err := <-saved; !errors.Is(err, store.ErrClosed) {
		t.Fatalf("hung Save = %v, want store.ErrClosed", err)
	}

	// Whichever of this Save and Shutdown's abandoned Close takes the lock
	// first, an unsealed session lets one of them reach the closed engine
	// before this Save returns.
	if err := s.Save(); err != nil {
		t.Fatalf("Save after a sealed Shutdown = %v, want nil", err)
	}

	// The one call after close is the hung save's own ResumeData return.
	if _, after := eng.counts(); after != 1 {
		t.Fatalf("%d engine calls after the engine closed, want 1 (the hung save's own)", after)
	}
}

// TestSessionCloseMakesASaveQueuedBehindItANoOp parks Close's final save,
// queues a Save behind it, and checks that Save does nothing once Close
// finishes (T-994). Which goroutine takes the lock when Close lets go of it
// is up to the scheduler, so this does not pin that Close marks the session
// closed before it unlocks; TestSessionCloseLeavesNoGapBeforeItIsClosed
// does (T-9008).
func TestSessionCloseMakesASaveQueuedBehindItANoOp(t *testing.T) {
	eng := newGatedEngine(t)
	s, _ := resumedSession(t, eng)
	eng.track("a")

	parked, release := eng.park("a")
	closed := make(chan error, 1)

	go func() { closed <- s.Close() }()

	<-parked

	queued := make(chan error, 1)

	go func() { queued <- s.Save() }()

	close(release)

	if err := <-closed; err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := <-queued; err != nil {
		t.Fatalf("queued Save: %v", err)
	}

	// Resume's List and Close's save; the queued Save made none.
	if lists, _ := eng.counts(); lists != 2 {
		t.Fatalf("engine List calls = %d, want 2 (a Save queued behind Close must not run)", lists)
	}
}

// TestSessionSetTorrentWaitsForASaveInFlight is the T-994 origin race: a
// save lists the engine, the next add lands in the engine and writes its
// record — origin included — while that save is still running, and the save
// then prunes what it did not list. SetTorrent must wait for the save, so
// the record survives with its origin, and the add's own save keeps it.
func TestSessionSetTorrentWaitsForASaveInFlight(t *testing.T) {
	eng := newGatedEngine(t)
	s, st := resumedSession(t, eng)
	eng.track("a")

	parked, release := eng.park("a")
	saved := make(chan error, 1)

	go func() { saved <- s.Save() }()

	<-parked

	// The downloads screen's read must not wait for the save.
	if _, ok := s.GetTorrent("a"); ok {
		t.Fatal("GetTorrent found a record the parked save has not written yet")
	}

	// The next add: accepted by the engine after the save listed it.
	eng.track("b")

	added := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	want := store.TorrentRecord{
		ID: "b", IndexerID: "example-src", SourceURL: "https://example.org/t/b",
		AddedAt: added, Magnet: "magnet:?xt=urn:btih:b",
	}

	set := make(chan error, 1)

	go func() { set <- s.SetTorrent(want) }()

	// A SetTorrent that does not wait for the save lands here, and the
	// save's prune then deletes it. The wait only gives such a SetTorrent
	// time to show itself; a correct one never returns before release.
	select {
	case err := <-set:
		t.Fatalf("SetTorrent returned (%v) while a Save was in flight", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(release)

	if err := <-saved; err != nil {
		t.Fatalf("Save in flight: %v", err)
	}

	if err := <-set; err != nil {
		t.Fatalf("SetTorrent: %v", err)
	}

	// The add's own save (saveSessionCmd after handleAddResult).
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	rec, ok := st.GetTorrent("b")
	if !ok || rec.IndexerID != want.IndexerID || rec.SourceURL != want.SourceURL || !rec.AddedAt.Equal(added) {
		t.Fatalf("record after racing saves = %+v (found %v), want origin %q %q added %v", rec, ok, want.IndexerID, want.SourceURL, added)
	}
}

// waitUntil polls cond until it holds, failing the test after 5s.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}

		time.Sleep(time.Millisecond)
	}
}

// gapProbe runs a SetTorrent and a Save the moment the session's lock is
// first released after arm — exactly what a goroutine queued on the lock
// would do if it won it then — and records what each did. A session that
// is closed before it unlocks leaves no gap: the SetTorrent is refused and
// the Save touches nothing.
type gapProbe struct {
	fired     bool
	setErr    error
	saveErr   error
	saveLists int
}

func (p *gapProbe) arm(s *Session, eng *gatedEngine) {
	s.afterUnlock = func() {
		if p.fired {
			return // the probe's own calls unlock too
		}

		p.fired = true
		p.setErr = s.SetTorrent(store.TorrentRecord{ID: "gap", Magnet: "magnet:?xt=urn:btih:gap"})

		before, _ := eng.counts()
		p.saveErr = s.Save()
		after, _ := eng.counts()
		p.saveLists = after - before
	}
}

// check fails t unless the probe ran and found the session already closed.
// Call it only once whatever released the lock has returned.
func (p *gapProbe) check(t *testing.T) {
	t.Helper()

	if !p.fired {
		t.Fatal("the lock was never released through Session.unlock")
	}

	if !errors.Is(p.setErr, ErrSessionClosed) {
		t.Errorf("SetTorrent the moment the lock was released = %v, want ErrSessionClosed", p.setErr)
	}

	if p.saveErr != nil || p.saveLists != 0 {
		t.Errorf("Save the moment the lock was released = %v with %d engine List calls, want a no-op", p.saveErr, p.saveLists)
	}
}

// TestSessionCloseLeavesNoGapBeforeItIsClosed takes the lock the moment
// Close releases it: the session must already be closed then, or a save or
// record write queued behind Close runs after it (T-994, T-9008).
func TestSessionCloseLeavesNoGapBeforeItIsClosed(t *testing.T) {
	eng := newGatedEngine(t)
	s, _ := resumedSession(t, eng)
	eng.track("a")

	var p gapProbe
	p.arm(s, eng)

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	p.check(t)
}

// TestShutdownClosesTheSessionBeforeReleasingIt takes the lock the moment
// Shutdown's session step releases it, before that step returns: the
// session must already be closed, so Shutdown has to close it (Close), not
// save it and seal it afterwards, which leaves a queued save or record
// write free to run against the store and engine Shutdown then closes
// (T-994, T-9008).
func TestShutdownClosesTheSessionBeforeReleasingIt(t *testing.T) {
	eng := newGatedEngine(t)
	s, st := resumedSession(t, eng)
	eng.track("a")

	var p gapProbe
	p.arm(s, eng)

	if errs := Shutdown(ShutdownOptions{Engine: eng, Store: st, Session: s, Logger: quietLogger()}); len(errs) != 0 {
		t.Fatalf("Shutdown: %v", errs)
	}

	p.check(t)
}
