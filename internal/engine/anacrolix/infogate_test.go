package anacrolix

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent"

	"github.com/kdta91/tortui/internal/engine"
)

// downloadGateOpen reports whether tt's download gate is open, through the
// library's own reader: a read of a piece nobody has blocks while downloads
// are allowed, and fails at once — or as soon as the gate closes — while
// they are disallowed. It waits up to wait for the gate to close.
func downloadGateOpen(t *testing.T, tt *torrent.Torrent, wait time.Duration) bool {
	t.Helper()

	r := tt.NewReader()
	defer func() { _ = r.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()

	r.SetContext(ctx)

	_, err := r.Read(make([]byte, 1))

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return true
	case err != nil && strings.Contains(err.Error(), "data downloading disabled"):
		return false
	default:
		t.Fatalf("probe read = %v, want the deadline or the library's downloads-disabled error", err)
		return false
	}
}

// TestResumeAsMetadataArrivesLeavesTransfersRunning is T-946. A torrent
// paused before its info dictionary arrived is resumed in the window right
// after awaitInfo has decided to hold it and released Engine.mu. The gate
// must already be in place by then, so the Resume lifts it: the torrent
// shows StateDownloading and its downloads are allowed. Applying the gate
// after unlocking would close it behind the Resume and leave the torrent
// showing StateDownloading with its transfers held.
func TestResumeAsMetadataArrivesLeavesTransfersRunning(t *testing.T) {
	t.Parallel()

	url := serveTorrent(t, encodeTorrent(t, buildInfo("info-gate-fixture", [][]string{{"a.bin"}})))

	var (
		e       *Engine
		id      string
		reached = make(chan struct{})
		proceed = make(chan struct{})
		resumed = make(chan error, 1)
	)

	e = newTestEngine(t, func(o *Options) {
		o.MetadataTimeout = time.Hour
		o.beforeAttach = func() {
			close(reached)
			<-proceed
		}
		o.afterInfo = func(got string) {
			if got == id {
				resumed <- e.Resume(id)
			}
		}
	})

	var err error

	id, err = e.Add(context.Background(), engine.AddSource{TorrentURL: url})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	select {
	case <-reached:
	case <-time.After(5 * time.Second):
		t.Fatal("the fetch never reached attach")
	}

	if err := e.Pause(id); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	close(proceed)

	select {
	case err := <-resumed:
		if err != nil {
			t.Fatalf("Resume in the window: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("awaitInfo never reached the window after its gate")
	}

	st := statusOf(t, e, id)
	if st.State != engine.StateDownloading {
		t.Fatalf("State after Resume = %s, want %s", st.State, engine.StateDownloading)
	}

	e.mu.Lock()
	tt := e.torrents[id].t
	e.mu.Unlock()

	// awaitInfo has nothing left to do but register the data as wanted
	// (and, before T-946, apply the gate); the probe watches for the gate
	// to close for a whole second after the Resume.
	if !downloadGateOpen(t, tt, time.Second) {
		t.Fatal("downloads are disallowed on a torrent resumed as its metadata arrived")
	}
}
