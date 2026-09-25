package anacrolix

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
)

// manualTicker replaces the sample loop's wall-clock ticker so a test decides
// exactly when each tick happens. Its channel is unbuffered: a send completes
// only once the sample loop is back in its select, which is what lets a test
// prove the loop never blocked on a consumer.
type manualTicker struct {
	ch       chan time.Time
	interval atomic.Int64
	stopped  atomic.Bool
	now      time.Time
}

func newManualTicker() *manualTicker {
	return &manualTicker{
		ch:  make(chan time.Time),
		now: time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC),
	}
}

func (m *manualTicker) factory(d time.Duration) (<-chan time.Time, func()) {
	m.interval.Store(int64(d))
	return m.ch, func() { m.stopped.Store(true) }
}

// send delivers one tick, failing the test if the sample loop does not take
// it promptly — which is what a loop blocked on an unread Updates channel
// would look like.
func (m *manualTicker) send(t *testing.T) {
	t.Helper()

	m.now = m.now.Add(DefaultRateSampleInterval)

	select {
	case m.ch <- m.now:
	case <-time.After(5 * time.Second):
		t.Fatal("sample loop did not accept a tick: it is blocked")
	}
}

// tick delivers one tick and then a second one as a barrier: the second send
// can only complete after the loop has fully processed the first, including
// its publish. The barrier tick sees an unchanged snapshot, so it emits
// nothing of its own.
func (m *manualTicker) tick(t *testing.T) {
	t.Helper()
	m.send(t)
	m.send(t)
}

// newTickedEngine is newTestEngine driven by a manual ticker, with a metadata
// timeout long enough that no torrent changes state on its own mid-test.
func newTickedEngine(t *testing.T) (*Engine, *manualTicker) {
	t.Helper()

	mt := newManualTicker()
	e := newTestEngine(t, func(o *Options) {
		o.newTicker = mt.factory
		o.MetadataTimeout = time.Minute
	})

	return e, mt
}

// pending reports how many snapshots are sitting unread on Updates.
func pending(e *Engine) int { return len(e.Updates()) }

// recv takes one snapshot that must already be waiting.
func recv(t *testing.T, e *Engine) []engine.TorrentStatus {
	t.Helper()

	select {
	case snap, ok := <-e.Updates():
		if !ok {
			t.Fatal("Updates() closed unexpectedly")
		}

		return snap
	default:
		t.Fatal("no snapshot waiting on Updates()")
		return nil
	}
}

func addMagnet(t *testing.T, e *Engine, seed string) string {
	t.Helper()

	id, err := e.Add(context.Background(), engine.AddSource{Magnet: magnetURI(seed)})
	if err != nil {
		t.Fatalf("Add %s: %v", seed, err)
	}

	return id
}

func TestUpdatesCadenceDefaultsToTwoHertz(t *testing.T) {
	t.Parallel()

	mt := newManualTicker()
	newTestEngine(t, func(o *Options) {
		o.newTicker = mt.factory
		o.RateSampleInterval = 0
	})

	if got := time.Duration(mt.interval.Load()); got != 500*time.Millisecond {
		t.Errorf("update interval = %s, want 500ms (~2 Hz)", got)
	}

	mt2 := newManualTicker()
	newTestEngine(t, func(o *Options) {
		o.newTicker = mt2.factory
		o.RateSampleInterval = 250 * time.Millisecond
	})

	if got := time.Duration(mt2.interval.Load()); got != 250*time.Millisecond {
		t.Errorf("configured interval = %s, want 250ms passed through", got)
	}
}

func TestUpdatesEmitsOneSnapshotPerTickAndNothingBetween(t *testing.T) {
	t.Parallel()

	e, mt := newTickedEngine(t)
	id := addMagnet(t, e, "cadence")

	// No tick yet: Add alone publishes nothing — events never send.
	if n := pending(e); n != 0 {
		t.Fatalf("%d snapshots before any tick, want 0", n)
	}

	mt.tick(t)

	snap := recv(t, e)
	if len(snap) != 1 || snap[0].ID != id || snap[0].State != engine.StateChecking {
		t.Fatalf("snapshot = %+v, want the one checking torrent %s", snap, id)
	}

	// Ticks with nothing changed emit nothing: no spam from an idle engine.
	mt.tick(t)
	mt.tick(t)

	if n := pending(e); n != 0 {
		t.Fatalf("%d snapshots after unchanged ticks, want 0", n)
	}

	if err := e.Pause(id); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	if n := pending(e); n != 0 {
		t.Fatalf("%d snapshots after Pause with no tick, want 0", n)
	}

	mt.tick(t)

	snap = recv(t, e)
	if len(snap) != 1 || snap[0].State != engine.StatePaused {
		t.Fatalf("snapshot after Pause = %+v, want one paused torrent", snap)
	}

	if n := pending(e); n != 0 {
		t.Errorf("%d extra snapshots, want exactly one per changed tick", n)
	}
}

func TestUpdatesCoalescesEveryChangeBetweenTicksIntoOneFullSnapshot(t *testing.T) {
	t.Parallel()

	e, mt := newTickedEngine(t)

	ids := make([]string, 5)
	for i := range ids {
		ids[i] = addMagnet(t, e, fmt.Sprintf("coalesce-%d", i))
	}

	for _, id := range ids[:3] {
		if err := e.Pause(id); err != nil {
			t.Fatalf("Pause: %v", err)
		}
	}

	if err := e.Resume(ids[0]); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if err := e.Remove(ids[4], false); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if n := pending(e); n != 0 {
		t.Fatalf("%d snapshots before the tick, want 0 — changes must not send per event", n)
	}

	mt.tick(t)

	snap := recv(t, e)
	if n := pending(e); n != 0 {
		t.Fatalf("%d further snapshots, want one coalesced snapshot", n)
	}

	want := map[string]engine.State{
		ids[0]: engine.StateChecking,
		ids[1]: engine.StatePaused,
		ids[2]: engine.StatePaused,
		ids[3]: engine.StateChecking,
	}

	if len(snap) != len(want) {
		t.Fatalf("snapshot has %d torrents, want the full set of %d", len(snap), len(want))
	}

	for _, st := range snap {
		if w, ok := want[st.ID]; !ok || st.State != w {
			t.Errorf("torrent %s state = %s (tracked %v), want %s", st.ID, st.State, ok, w)
		}
	}

	if !snapshotsEqual(snap, e.List()) {
		t.Errorf("snapshot %+v differs from List() %+v", snap, e.List())
	}
}

func TestUpdatesDropsStaleSnapshotsForASlowConsumer(t *testing.T) {
	t.Parallel()

	e, mt := newTickedEngine(t)

	if c := cap(e.Updates()); c != 1 {
		t.Fatalf("Updates() capacity = %d, want a one-snapshot buffer", c)
	}

	// Nobody reads. Every tick carries a changed snapshot, and every tick
	// send must still complete: mt.send fails the test if the loop blocks.
	const rounds = 20
	for i := range rounds {
		addMagnet(t, e, fmt.Sprintf("slow-%d", i))
		mt.tick(t)

		if n := pending(e); n != 1 {
			t.Fatalf("round %d: %d snapshots buffered, want exactly 1", i, n)
		}
	}

	snap := recv(t, e)
	if len(snap) != rounds {
		t.Errorf("slow consumer read a snapshot of %d torrents, want the newest (%d); stale ones must be dropped",
			len(snap), rounds)
	}

	mt.tick(t)

	if n := pending(e); n != 0 {
		t.Errorf("%d snapshots after draining, want 0", n)
	}
}

func TestUpdatesConsumerMayMutateWhatItReceives(t *testing.T) {
	t.Parallel()

	e, mt := newTickedEngine(t)
	addMagnet(t, e, "mutating-consumer")
	addMagnet(t, e, "mutating-consumer-2")

	for round := range 5 {
		mt.tick(t)

		snap := recv(t, e)

		// A consumer is free to edit or reorder its own snapshot. This
		// must neither race the engine (run under -race) nor make the
		// next, unchanged tick look like a change.
		for i := range snap {
			snap[i].Name = "edited by consumer"
			snap[i].State = engine.StateErrored
		}

		snap[0], snap[1] = snap[1], snap[0]

		mt.tick(t)

		if n := pending(e); n != 0 {
			t.Fatalf("round %d: %d snapshots after an unchanged tick, want 0 — consumer edits leaked into change detection",
				round, n)
		}

		addMagnet(t, e, fmt.Sprintf("mutating-consumer-round-%d", round))
	}
}

func TestUpdatesRealTickerDeliversSnapshots(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, func(o *Options) { o.MetadataTimeout = time.Minute })
	id := addMagnet(t, e, "real-ticker")

	deadline := time.After(5 * time.Second)

	for {
		select {
		case snap := <-e.Updates():
			for _, st := range snap {
				if st.ID == id {
					return
				}
			}
		case <-deadline:
			t.Fatal("no snapshot containing the added torrent arrived on Updates()")
		}
	}
}

func TestCloseClosesUpdatesOnceWithASnapshotPending(t *testing.T) {
	t.Parallel()

	mt := newManualTicker()

	e, err := New(Options{
		Config:          config.Config{DownloadDir: t.TempDir()},
		Logger:          discardLogger(),
		MetadataTimeout: time.Minute,
		Offline:         true,
		newTicker:       mt.factory,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	addMagnet(t, e, "close-pending")
	mt.tick(t)

	ch := e.Updates()

	done := make(chan error, 4)
	for range cap(done) {
		go func() { done <- e.Close() }()
	}

	for range cap(done) {
		if err := <-done; err != nil {
			t.Errorf("Close: %v", err)
		}
	}

	if !mt.stopped.Load() {
		t.Error("Close did not stop the sample ticker")
	}

	if snap, ok := <-ch; !ok || len(snap) != 1 {
		t.Errorf("pending snapshot = %v (open %v), want the buffered snapshot still readable", snap, ok)
	}

	if _, ok := <-ch; ok {
		t.Error("Updates() still open after Close")
	}
}

func TestETAUsesTheRollingAverageRate(t *testing.T) {
	t.Parallel()

	e, _ := newTickedEngine(t)
	path := writeTorrentFile(t, buildInfo("eta-fixture", [][]string{{"a.bin"}}))

	id, err := e.Add(context.Background(), engine.AddSource{FilePath: path})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	waitForState(t, e, id, engine.StateDownloading)

	// The manual ticker never fires, so nothing overwrites these. The
	// instantaneous rate and the rolling average disagree on purpose.
	set := func(rate, avg int64) {
		e.mu.Lock()
		e.torrents[id].down.rate = rate
		e.torrents[id].down.avg = avg
		e.mu.Unlock()
	}

	set(1, 512)

	st := statusOf(t, e, id)
	if st.ETA != 2*time.Second {
		t.Errorf("ETA = %s, want 2s (1024 bytes left at the 512 B/s average, not the 1 B/s instantaneous rate)", st.ETA)
	}

	if st.DownRate != 1 {
		t.Errorf("DownRate = %d, want the instantaneous 1 B/s", st.DownRate)
	}

	set(4096, 0)

	if st := statusOf(t, e, id); st.ETA != -1 {
		t.Errorf("ETA with a zero average = %s, want -1 (indeterminate)", st.ETA)
	}

	set(512, 512)

	if err := e.Pause(id); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	if st := statusOf(t, e, id); st.ETA != -1 {
		t.Errorf("ETA while paused = %s, want -1", st.ETA)
	}
}

func TestRateMeterAveragesAcrossTheWindow(t *testing.T) {
	t.Parallel()

	var m rateMeter
	base := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	// 0, then a 1000-byte burst, then idle: instantaneous rate swings,
	// the average does not.
	m.observe(base, 0)
	m.observe(base.Add(time.Second), 1000)
	m.observe(base.Add(2*time.Second), 1000)

	if m.rate != 0 {
		t.Errorf("instantaneous rate = %d, want 0 on an idle sample", m.rate)
	}

	if m.avg != 500 {
		t.Errorf("avg = %d, want 500 (1000 bytes over 2s)", m.avg)
	}

	// Fill well past the window at a steady 100 B/s: the early burst ages
	// out and the average converges on the steady rate.
	counter := int64(1000)
	for i := 3; i < 3+2*rateWindow; i++ {
		counter += 100
		m.observe(base.Add(time.Duration(i)*time.Second), counter)
	}

	if m.avg != 100 {
		t.Errorf("avg after the burst aged out = %d, want 100", m.avg)
	}

	if len(m.window) != rateWindow+1 {
		t.Errorf("window holds %d samples, want %d", len(m.window), rateWindow+1)
	}
}

func TestRateMeterAverageRestartsOnACounterResetAndOnReset(t *testing.T) {
	t.Parallel()

	var m rateMeter
	base := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	m.observe(base, 0)
	m.observe(base.Add(time.Second), 5000)
	m.observe(base.Add(2*time.Second), 10)

	if m.avg != 0 {
		t.Errorf("avg after a counter reset = %d, want 0", m.avg)
	}

	m.observe(base.Add(3*time.Second), 210)
	if m.avg != 200 {
		t.Errorf("avg after the restart = %d, want 200, not polluted by the old counter", m.avg)
	}

	m.reset()

	if m.avg != 0 || len(m.window) != 0 {
		t.Errorf("reset left avg %d, window %d", m.avg, len(m.window))
	}
}

// uncomparableErr is an error whose dynamic type cannot be compared with ==.
type uncomparableErr struct{ parts []string }

func (u uncomparableErr) Error() string { return fmt.Sprint(u.parts) }

func TestSnapshotsEqualComparesErrorsByMessage(t *testing.T) {
	t.Parallel()

	a := []engine.TorrentStatus{{ID: "x", Err: uncomparableErr{[]string{"boom"}}}}
	b := []engine.TorrentStatus{{ID: "x", Err: uncomparableErr{[]string{"boom"}}}}

	if !snapshotsEqual(a, b) {
		t.Error("identical snapshots with an uncomparable error type reported different")
	}

	b[0].Err = errors.New("other")
	if snapshotsEqual(a, b) {
		t.Error("snapshots with different errors reported equal")
	}

	if snapshotsEqual(a, nil) {
		t.Error("snapshots of different length reported equal")
	}

	c := []engine.TorrentStatus{{ID: "x", Err: a[0].Err, Progress: 0.5}}
	if snapshotsEqual(a, c) {
		t.Error("snapshots differing in Progress reported equal")
	}
}
