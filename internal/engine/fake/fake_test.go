package fake

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/engine"
)

// compile-time assertion that Engine satisfies the frozen interface.
var _ engine.Engine = (*Engine)(nil)

func TestAddRejectsEmptySource(t *testing.T) {
	e := New()
	t.Cleanup(func() { _ = e.Close() })

	if _, err := e.Add(context.Background(), engine.AddSource{}); !errors.Is(err, ErrNoSource) {
		t.Fatalf("Add(empty source) error = %v, want %v", err, ErrNoSource)
	}
}

func TestAddRejectsCancelledContext(t *testing.T) {
	e := New()
	t.Cleanup(func() { _ = e.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := e.Add(ctx, engine.AddSource{Magnet: "magnet:?xt=urn:btih:deadbeef"}); err == nil {
		t.Fatal("Add with a cancelled context returned a nil error")
	}
}

func TestAddAfterCloseFails(t *testing.T) {
	e := New()
	if err := e.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}

	if _, err := e.Add(context.Background(), engine.AddSource{Magnet: "magnet:?xt=urn:btih:deadbeef"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Add after Close error = %v, want %v", err, ErrClosed)
	}
}

func TestAddNamesFromMagnetDisplayName(t *testing.T) {
	e := New()
	t.Cleanup(func() { _ = e.Close() })

	id, err := e.Add(context.Background(), engine.AddSource{
		Magnet: "magnet:?xt=urn:btih:deadbeef&dn=debian-12.5.0-amd64-netinst.iso",
	})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	statuses := e.List()
	got := findStatus(t, statuses, id)
	if got.Name != "debian-12.5.0-amd64-netinst.iso" {
		t.Errorf("Name = %q, want the magnet's dn parameter", got.Name)
	}
	if got.State != engine.StateChecking && got.State != engine.StateQueued {
		t.Errorf("State immediately after Add = %v, want Queued or Checking", got.State)
	}
}

func TestAddNamesFromFilePathAndTorrentURL(t *testing.T) {
	e := New()
	t.Cleanup(func() { _ = e.Close() })

	idFile, err := e.Add(context.Background(), engine.AddSource{FilePath: "/home/user/Downloads/example.torrent"})
	if err != nil {
		t.Fatalf("Add(FilePath) error = %v", err)
	}
	idURL, err := e.Add(context.Background(), engine.AddSource{TorrentURL: "https://example.org/files/example2.torrent"})
	if err != nil {
		t.Fatalf("Add(TorrentURL) error = %v", err)
	}

	statuses := e.List()
	if got := findStatus(t, statuses, idFile).Name; got != "example.torrent" {
		t.Errorf("Name from FilePath = %q, want %q", got, "example.torrent")
	}
	if got := findStatus(t, statuses, idURL).Name; got != "example2.torrent" {
		t.Errorf("Name from TorrentURL = %q, want %q", got, "example2.torrent")
	}
}

func TestDownloadLifecycleViaAdvance(t *testing.T) {
	e := New()
	t.Cleanup(func() { _ = e.Close() })
	e.ScriptFor = func(engine.AddSource) Script { return Downloading(10 * time.Second) }

	id, err := e.Add(context.Background(), engine.AddSource{Magnet: "magnet:?xt=urn:btih:aaaa"})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if got := findStatus(t, e.List(), id).State; got != engine.StateChecking {
		t.Fatalf("State immediately after Add = %v, want %v", got, engine.StateChecking)
	}

	e.Advance(5 * time.Second)
	mid := findStatus(t, e.List(), id)
	if mid.State != engine.StateDownloading {
		t.Errorf("State at t=5s = %v, want %v", mid.State, engine.StateDownloading)
	}
	if mid.Progress <= 0 || mid.Progress >= 1 {
		t.Errorf("Progress at t=5s = %v, want strictly between 0 and 1", mid.Progress)
	}

	e.Advance(5 * time.Second)
	final := findStatus(t, e.List(), id)
	if final.State != engine.StateSeeding {
		t.Errorf("State at t=10s = %v, want %v", final.State, engine.StateSeeding)
	}
	if final.Progress != 1 {
		t.Errorf("Progress at t=10s = %v, want 1", final.Progress)
	}
}

func TestStallNeverCompletes(t *testing.T) {
	e := New()
	t.Cleanup(func() { _ = e.Close() })
	e.ScriptFor = func(engine.AddSource) Script { return Stalled(2*time.Second, 0.3) }

	id, err := e.Add(context.Background(), engine.AddSource{Magnet: "magnet:?xt=urn:btih:bbbb"})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	e.Advance(2 * time.Second)
	e.Advance(24 * time.Hour)

	got := findStatus(t, e.List(), id)
	if got.State != engine.StateDownloading {
		t.Errorf("stalled State = %v, want %v", got.State, engine.StateDownloading)
	}
	if got.Progress != 0.3 {
		t.Errorf("stalled Progress = %v, want 0.3", got.Progress)
	}
	if got.DownRate != 0 {
		t.Errorf("stalled DownRate = %d, want 0", got.DownRate)
	}
}

func TestErrorTransitionSurfacesErr(t *testing.T) {
	e := New()
	t.Cleanup(func() { _ = e.Close() })
	wantErr := errors.New("metadata fetch timed out")
	e.ScriptFor = func(engine.AddSource) Script { return Errored(time.Second, wantErr) }

	id, err := e.Add(context.Background(), engine.AddSource{Magnet: "magnet:?xt=urn:btih:cccc"})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	e.Advance(time.Second)

	got := findStatus(t, e.List(), id)
	if got.State != engine.StateErrored {
		t.Fatalf("State = %v, want %v", got.State, engine.StateErrored)
	}
	if !errors.Is(got.Err, wantErr) {
		t.Errorf("Err = %v, want %v", got.Err, wantErr)
	}
}

func TestPauseStopsProgressAndResumeContinues(t *testing.T) {
	e := New()
	t.Cleanup(func() { _ = e.Close() })
	e.ScriptFor = func(engine.AddSource) Script { return Downloading(10 * time.Second) }

	id, err := e.Add(context.Background(), engine.AddSource{Magnet: "magnet:?xt=urn:btih:dddd"})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	e.Advance(4 * time.Second)
	beforePause := findStatus(t, e.List(), id)

	if err := e.Pause(id); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}
	paused := findStatus(t, e.List(), id)
	if paused.State != engine.StatePaused {
		t.Errorf("State after Pause = %v, want %v", paused.State, engine.StatePaused)
	}
	if paused.DownRate != 0 || paused.UpRate != 0 {
		t.Errorf("rates after Pause = down %d up %d, want both 0", paused.DownRate, paused.UpRate)
	}

	// Advancing while paused must not move progress.
	e.Advance(3 * time.Second)
	stillPaused := findStatus(t, e.List(), id)
	if stillPaused.Progress != beforePause.Progress {
		t.Errorf("Progress moved while paused: before %v, after advancing while paused %v",
			beforePause.Progress, stillPaused.Progress)
	}

	if err := e.Resume(id); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	resumed := findStatus(t, e.List(), id)
	if resumed.State != engine.StateDownloading {
		t.Errorf("State after Resume = %v, want %v", resumed.State, engine.StateDownloading)
	}

	// The remaining 6 simulated seconds (10s total minus the 4s that ran
	// before pause) must still complete the download; the paused interval
	// must not count against it.
	e.Advance(6 * time.Second)
	final := findStatus(t, e.List(), id)
	if final.State != engine.StateSeeding || final.Progress != 1 {
		t.Errorf("final status = %+v, want StateSeeding at Progress 1", final)
	}
}

func TestPauseResumeUnknownIDFails(t *testing.T) {
	e := New()
	t.Cleanup(func() { _ = e.Close() })

	if err := e.Pause("does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Pause(unknown) error = %v, want %v", err, ErrNotFound)
	}
	if err := e.Resume("does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Resume(unknown) error = %v, want %v", err, ErrNotFound)
	}
}

func TestPauseIsIdempotent(t *testing.T) {
	e := New()
	t.Cleanup(func() { _ = e.Close() })

	id, err := e.Add(context.Background(), engine.AddSource{Magnet: "magnet:?xt=urn:btih:eeee"})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if err := e.Pause(id); err != nil {
		t.Fatalf("first Pause() error = %v", err)
	}
	if err := e.Pause(id); err != nil {
		t.Fatalf("second Pause() error = %v, want nil (no-op)", err)
	}
}

func TestRemoveDropsFromListAndFiles(t *testing.T) {
	e := New()
	t.Cleanup(func() { _ = e.Close() })

	id, err := e.Add(context.Background(), engine.AddSource{Magnet: "magnet:?xt=urn:btih:ffff"})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if err := e.Remove(id, false); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	for _, s := range e.List() {
		if s.ID == id {
			t.Fatalf("List() still contains removed torrent %q", id)
		}
	}
	if _, err := e.Files(id); !errors.Is(err, ErrNotFound) {
		t.Errorf("Files(removed) error = %v, want %v", err, ErrNotFound)
	}
	if err := e.Remove(id, false); !errors.Is(err, ErrNotFound) {
		t.Errorf("Remove(already removed) error = %v, want %v", err, ErrNotFound)
	}
}

func TestFilesEmptyBeforeMetadataKnown(t *testing.T) {
	e := New()
	t.Cleanup(func() { _ = e.Close() })
	e.ScriptFor = func(engine.AddSource) Script { return nil } // stays at Add-time zero status

	id, err := e.Add(context.Background(), engine.AddSource{Magnet: "magnet:?xt=urn:btih:0000"})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	files, err := e.Files(id)
	if err != nil {
		t.Fatalf("Files() error = %v", err)
	}
	if len(files) != 0 {
		t.Errorf("Files() = %v, want empty before TotalBytes is known", files)
	}
}

func TestFilesReflectsProgressOnceKnown(t *testing.T) {
	e := New()
	t.Cleanup(func() { _ = e.Close() })
	e.ScriptFor = func(engine.AddSource) Script { return Completed() }

	id, err := e.Add(context.Background(), engine.AddSource{Magnet: "magnet:?xt=urn:btih:1111"})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	files, err := e.Files(id)
	if err != nil {
		t.Fatalf("Files() error = %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("Files() = %v, want exactly one synthetic file", files)
	}
	if files[0].Progress != 1 {
		t.Errorf("Files()[0].Progress = %v, want 1", files[0].Progress)
	}
	if files[0].DownloadedBytes != files[0].SizeBytes {
		t.Errorf("Files()[0] downloaded %d != size %d", files[0].DownloadedBytes, files[0].SizeBytes)
	}
}

func TestFilesUnknownID(t *testing.T) {
	e := New()
	t.Cleanup(func() { _ = e.Close() })

	if _, err := e.Files("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Files(unknown) error = %v, want %v", err, ErrNotFound)
	}
}

func TestUpdatesDeliversCoalescedSnapshot(t *testing.T) {
	e := New()
	t.Cleanup(func() { _ = e.Close() })
	e.ScriptFor = func(engine.AddSource) Script { return Downloading(10 * time.Second) }

	id, err := e.Add(context.Background(), engine.AddSource{Magnet: "magnet:?xt=urn:btih:2222"})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	// Drain the snapshot Add already published.
	select {
	case <-e.Updates():
	case <-time.After(time.Second):
		t.Fatal("no snapshot published by Add")
	}

	// Multiple Advance calls without reading in between must still leave
	// exactly one coalesced snapshot waiting, reflecting the latest state.
	e.Advance(3 * time.Second)
	e.Advance(3 * time.Second)
	e.Advance(3 * time.Second)

	var snap []engine.TorrentStatus
	select {
	case snap = <-e.Updates():
	case <-time.After(time.Second):
		t.Fatal("no snapshot published after Advance")
	}

	got := findStatus(t, snap, id)
	if got.State != engine.StateDownloading {
		t.Errorf("coalesced snapshot State = %v, want %v", got.State, engine.StateDownloading)
	}

	select {
	case extra := <-e.Updates():
		t.Fatalf("unexpected second snapshot waiting: %v", extra)
	default:
	}
}

func TestCloseIsIdempotentAndClosesChannel(t *testing.T) {
	e := New()

	if err := e.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("second Close() error = %v, want nil", err)
	}

	_, open := <-e.Updates()
	if open {
		t.Error("Updates() channel still open after Close")
	}
}

func TestAdvanceAfterCloseDoesNotPanic(t *testing.T) {
	e := New()
	if _, err := e.Add(context.Background(), engine.AddSource{Magnet: "magnet:?xt=urn:btih:3333"}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	e.Advance(time.Second) // must not panic or send on the closed channel
}

func TestConcurrentUse(t *testing.T) {
	e := New()
	t.Cleanup(func() { _ = e.Close() })
	e.ScriptFor = func(engine.AddSource) Script { return Downloading(2 * time.Second) }

	const n = 20
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		id, err := e.Add(context.Background(), engine.AddSource{Magnet: "magnet:?xt=urn:btih:concurrent"})
		if err != nil {
			t.Fatalf("Add() error = %v", err)
		}
		ids[i] = id
	}

	var wg sync.WaitGroup
	for _, id := range ids {
		id := id
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = e.Pause(id)
			_ = e.Resume(id)
			e.List()
			_, _ = e.Files(id)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			e.Advance(100 * time.Millisecond)
		}
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent use did not complete in time")
	}
}

// findStatus locates the status for id in statuses, failing the test if it
// is missing.
func findStatus(t *testing.T, statuses []engine.TorrentStatus, id string) engine.TorrentStatus {
	t.Helper()
	for _, s := range statuses {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no status found for id %q in %+v", id, statuses)
	return engine.TorrentStatus{}
}
