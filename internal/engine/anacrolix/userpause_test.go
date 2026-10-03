package anacrolix

import (
	"context"
	"testing"
	"time"

	"github.com/anacrolix/torrent"

	"github.com/kdta91/tortui/internal/engine"
)

// pausedOf reports what ResumeData says about id's pause.
func pausedOf(t *testing.T, e *Engine, id string) bool {
	t.Helper()

	d, err := e.ResumeData(id)
	if err != nil {
		t.Fatalf("ResumeData(%s): %v", id, err)
	}

	return d.Paused
}

// attachedTorrent waits until id is attached to the client with its info
// dictionary known, and returns the library's torrent.
func attachedTorrent(t *testing.T, e *Engine, id string) *torrent.Torrent {
	t.Helper()

	var tt *torrent.Torrent

	waitUntil(t, "torrent "+id+" attached with its info", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()

		tt = e.torrents[id].t

		return tt != nil && tt.Info() != nil
	})

	return tt
}

// TestResumeDataReportsOnlyTheUsersPause is T-952: only a pause the user
// asked for is saved. A queued torrent, a running one, and one paused by
// PauseForShutdown are not; the user's pause stays reported through the
// shutdown pause, and a Resume clears it.
func TestResumeDataReportsOnlyTheUsersPause(t *testing.T) {
	t.Parallel()

	e := queueEngine(t)
	ctx := context.Background()

	running, err := e.Add(ctx, engine.AddSource{Magnet: magnetURI("user-pause-running")})
	if err != nil {
		t.Fatalf("Add(running): %v", err)
	}

	queued, err := e.Add(ctx, engine.AddSource{Magnet: magnetURI("user-pause-queued")})
	if err != nil {
		t.Fatalf("Add(queued): %v", err)
	}

	user, err := e.Add(ctx, engine.AddSource{Magnet: magnetURI("user-pause-user")})
	if err != nil {
		t.Fatalf("Add(user): %v", err)
	}

	if err := e.Pause(user); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	if st := statusOf(t, e, queued); st.State != engine.StateQueued {
		t.Fatalf("queued torrent state = %s, want queued", st.State)
	}

	for id, want := range map[string]bool{running: false, queued: false, user: true} {
		if got := pausedOf(t, e, id); got != want {
			t.Errorf("before shutdown: ResumeData(%s).Paused = %v, want %v", id, got, want)
		}
	}

	if err := e.PauseForShutdown(); err != nil {
		t.Fatalf("PauseForShutdown: %v", err)
	}

	for _, id := range []string{running, queued, user} {
		if st := statusOf(t, e, id); st.State != engine.StatePaused {
			t.Errorf("after PauseForShutdown: %s state = %s, want paused", id, st.State)
		}
	}

	if q := e.Queue(); len(q) != 0 {
		t.Errorf("Queue() after PauseForShutdown = %v, want empty", q)
	}

	for id, want := range map[string]bool{running: false, queued: false, user: true} {
		if got := pausedOf(t, e, id); got != want {
			t.Errorf("after PauseForShutdown: ResumeData(%s).Paused = %v, want %v", id, got, want)
		}
	}

	if err := e.Resume(user); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if pausedOf(t, e, user) {
		t.Error("ResumeData.Paused still set after Resume")
	}
}

// TestRestoredUserPauseComesBackPausedAndHeld is T-952's restart criterion:
// a torrent the user paused, restored into a fresh engine, comes back paused
// with its download gate closed from the start, takes no queue slot, and
// stays reported as paused for the next save. Resume starts it. A record
// saved without the field restores running, as before.
func TestRestoredUserPauseComesBackPausedAndHeld(t *testing.T) {
	t.Parallel()

	info := buildInfo("paused-restore", [][]string{{"a.bin"}})
	plain := buildInfo("plain-restore", [][]string{{"b.bin"}})

	first := newTestEngine(t, nil)
	dir := first.downloadDir

	id, err := first.Add(context.Background(), engine.AddSource{FilePath: writeTorrentFile(t, info)})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := first.Pause(id); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	d, err := first.ResumeData(id)
	if err != nil {
		t.Fatalf("ResumeData: %v", err)
	}

	if !d.Paused || len(d.Metainfo) == 0 {
		t.Fatalf("ResumeData = paused %v, metainfo %d bytes; want paused with metainfo", d.Paused, len(d.Metainfo))
	}

	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second := newTestEngine(t, func(o *Options) {
		o.Config.DownloadDir = dir
		o.Config.MaxActiveDownloads = 1
	})

	// Every slot is taken: a paused torrent must not queue behind it.
	if _, err := second.Add(context.Background(), engine.AddSource{Magnet: magnetURI("paused-restore-slot")}); err != nil {
		t.Fatalf("Add(slot): %v", err)
	}

	restored, err := second.Restore(context.Background(), d)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if st := statusOf(t, second, restored); st.State != engine.StatePaused {
		t.Fatalf("restored state = %s (err %v), want paused", st.State, st.Err)
	}

	if q := second.Queue(); len(q) != 0 {
		t.Errorf("Queue() = %v, want a paused torrent out of the queue", q)
	}

	tt := attachedTorrent(t, second, restored)

	if downloadGateOpen(t, tt, 200*time.Millisecond) {
		t.Fatal("a torrent restored paused has its downloads allowed")
	}

	if st := statusOf(t, second, restored); st.State != engine.StatePaused || st.DownloadedBytes != 0 {
		t.Fatalf("restored status = state %s, %d bytes downloaded; want paused, nothing", st.State, st.DownloadedBytes)
	}

	if !pausedOf(t, second, restored) {
		t.Error("ResumeData.Paused of the restored torrent = false, want the pause kept for the next save")
	}

	// The record without the field: as before T-952, it runs.
	running, err := second.Restore(context.Background(), engine.ResumeData{
		ID: "an-40", Name: "plain-restore", Metainfo: encodeTorrent(t, plain), SavePath: dir,
	})
	if err != nil {
		t.Fatalf("Restore(plain): %v", err)
	}

	if st := statusOf(t, second, running); st.State == engine.StatePaused {
		t.Fatalf("a record saved without a pause restored paused")
	}

	if pausedOf(t, second, running) {
		t.Error("ResumeData.Paused of a record saved without a pause = true")
	}

	if err := second.Resume(restored); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	// The slot torrent and the plain one hold the only slot, so the
	// resumed one waits in the queue; it no longer shows paused.
	if st := statusOf(t, second, restored); st.State == engine.StatePaused {
		t.Fatalf("state after Resume = %s, want it out of the pause", st.State)
	}
}

// TestRestoredPausedMagnetAndAddressComeBackPaused covers the two restore
// paths with no saved metainfo: a magnet and a .torrent address each come
// back paused, and the address's torrent is attached with its download gate
// closed once its .torrent is fetched.
func TestRestoredPausedMagnetAndAddressComeBackPaused(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, func(o *Options) { o.MetadataTimeout = time.Hour })
	ctx := context.Background()

	magnet, err := e.Restore(ctx, engine.ResumeData{
		ID: "an-50", Name: "paused-magnet", Magnet: magnetURI("paused-magnet"), SavePath: e.downloadDir, Paused: true,
	})
	if err != nil {
		t.Fatalf("Restore(magnet): %v", err)
	}

	url := serveTorrent(t, encodeTorrent(t, buildInfo("paused-address", [][]string{{"c.bin"}})))

	address, err := e.Restore(ctx, engine.ResumeData{
		ID: "an-51", Name: "paused-address", TorrentURL: url, SavePath: e.downloadDir, Paused: true,
	})
	if err != nil {
		t.Fatalf("Restore(address): %v", err)
	}

	for _, id := range []string{magnet, address} {
		if st := statusOf(t, e, id); st.State != engine.StatePaused {
			t.Errorf("restored %s state = %s (err %v), want paused", id, st.State, st.Err)
		}
	}

	if downloadGateOpen(t, attachedTorrent(t, e, address), 200*time.Millisecond) {
		t.Fatal("a torrent restored paused by address has its downloads allowed")
	}

	if err := e.Resume(magnet); err != nil {
		t.Fatalf("Resume(magnet): %v", err)
	}

	if st := statusOf(t, e, magnet); st.State != engine.StateChecking {
		t.Errorf("magnet state after Resume = %s, want checking (no info yet)", st.State)
	}
}
