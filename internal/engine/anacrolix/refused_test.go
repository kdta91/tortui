package anacrolix

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"

	"github.com/kdta91/tortui/internal/engine"
)

// refusedFixture is a torrent's info dictionary and the plain magnet that
// names it, so a test can add the magnet and later hand it the dictionary a
// peer would have supplied.
func refusedFixture(t *testing.T, name string) (infoBytes []byte, magnet string) {
	t.Helper()

	infoBytes, err := bencode.Marshal(buildInfo(name, [][]string{{"a.bin"}}))
	if err != nil {
		t.Fatalf("bencode info: %v", err)
	}

	return infoBytes, "magnet:?xt=urn:btih:" + metainfo.HashBytes(infoBytes).HexString()
}

// supplyInfo hands a magnet torrent its info dictionary, as a peer would.
func supplyInfo(t *testing.T, e *Engine, id string, infoBytes []byte) {
	t.Helper()

	e.mu.Lock()
	tt := e.torrents[id].t
	e.mu.Unlock()

	if tt == nil {
		t.Fatalf("torrent %s is not attached", id)
	}

	if err := tt.SetInfoBytes(infoBytes); err != nil {
		t.Fatalf("SetInfoBytes: %v", err)
	}
}

// listed reports whether List has id.
func listed(e *Engine, id string) bool {
	for _, st := range e.List() {
		if st.ID == id {
			return true
		}
	}

	return false
}

// TestReAddOfARefusedMagnetIsEvaluatedAgain is T-948's awaitInfo half. A
// magnet refused when its info dictionary arrives (here for free space) is
// errored and dropped from the client. Adding it again must not hand back
// that stale entry with a nil error: the stale entry is untracked and the
// magnet starts over under a new ID, and is refused again on the same
// grounds when its dictionary arrives.
func TestReAddOfARefusedMagnetIsEvaluatedAgain(t *testing.T) {
	t.Parallel()

	var free atomic.Uint64

	e := newTestEngine(t, func(o *Options) {
		o.MetadataTimeout = time.Hour
		o.freeSpace = func(string) (uint64, error) { return free.Load(), nil }
	})

	infoBytes, magnet := refusedFixture(t, "refused-magnet")

	first, err := e.Add(context.Background(), engine.AddSource{Magnet: magnet})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	supplyInfo(t, e, first, infoBytes)

	if st := waitForState(t, e, first, engine.StateErrored); !errors.Is(st.Err, ErrInsufficientSpace) {
		t.Fatalf("first add: err %v, want ErrInsufficientSpace", st.Err)
	}

	again, err := e.Add(context.Background(), engine.AddSource{Magnet: magnet})
	if err != nil {
		t.Fatalf("re-Add: %v", err)
	}

	if again == first {
		t.Fatalf("re-Add returned the refused entry %s", first)
	}

	if listed(e, first) {
		t.Errorf("the refused entry %s is still listed after the re-Add", first)
	}

	if st := statusOf(t, e, again); st.State != engine.StateChecking {
		t.Fatalf("re-added state = %s (err %v), want %s", st.State, st.Err, engine.StateChecking)
	}

	supplyInfo(t, e, again, infoBytes)

	if st := waitForState(t, e, again, engine.StateErrored); !errors.Is(st.Err, ErrInsufficientSpace) {
		t.Fatalf("re-add: err %v, want ErrInsufficientSpace again", st.Err)
	}

	// With room on the disk, a third add is accepted and runs.
	free.Store(1 << 40)

	third, err := e.Add(context.Background(), engine.AddSource{Magnet: magnet})
	if err != nil || third == again {
		t.Fatalf("third Add = %s, %v; want a fresh entry", third, err)
	}

	supplyInfo(t, e, third, infoBytes)
	waitForState(t, e, third, engine.StateDownloading)
}

// TestReAddByAddressOfARefusedTorrentIsNotADuplicate is T-948 for a .torrent
// address: a torrent fetched by address whose infohash matches a refused
// magnet is added, not failed as "already added as" the dead entry.
func TestReAddByAddressOfARefusedTorrentIsNotADuplicate(t *testing.T) {
	t.Parallel()

	var free atomic.Uint64

	e := newTestEngine(t, func(o *Options) {
		o.MetadataTimeout = time.Hour
		o.freeSpace = func(string) (uint64, error) { return free.Load(), nil }
	})

	info := buildInfo("refused-address", [][]string{{"a.bin"}})

	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("bencode info: %v", err)
	}

	first, err := e.Add(context.Background(), engine.AddSource{
		Magnet: "magnet:?xt=urn:btih:" + metainfo.HashBytes(infoBytes).HexString(),
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	supplyInfo(t, e, first, infoBytes)
	waitForState(t, e, first, engine.StateErrored)

	free.Store(1 << 40)

	byURL, err := e.Add(context.Background(), engine.AddSource{TorrentURL: serveTorrent(t, encodeTorrent(t, info))})
	if err != nil {
		t.Fatalf("Add by address: %v", err)
	}

	// Before T-948 this fails with "already added as" the refused entry.
	waitForState(t, e, byURL, engine.StateDownloading)

	if listed(e, first) {
		t.Errorf("the refused entry %s is still listed after the address add", first)
	}
}

// TestReAddOfATorrentWhoseQueuedStartFailedIsEvaluatedAgain is T-948's queue
// half: a queued .torrent whose start fails when the queue promotes it (the
// disk filled while it waited) is errored, and adding it again starts it
// over under a new ID instead of handing back the errored entry.
func TestReAddOfATorrentWhoseQueuedStartFailedIsEvaluatedAgain(t *testing.T) {
	t.Parallel()

	var free atomic.Uint64
	free.Store(1 << 40)

	e := newTestEngine(t, func(o *Options) {
		o.Config.MaxActiveDownloads = 1
		o.MetadataTimeout = time.Hour
		o.freeSpace = func(string) (uint64, error) { return free.Load(), nil }
	})

	ctx := context.Background()

	slot, err := e.Add(ctx, engine.AddSource{Magnet: magnetURI("refused-queue-slot")})
	if err != nil {
		t.Fatalf("Add(slot): %v", err)
	}

	path := writeTorrentFile(t, buildInfo("refused-queued", [][]string{{"q.bin"}}))

	queued, err := e.Add(ctx, engine.AddSource{FilePath: path})
	if err != nil {
		t.Fatalf("Add(queued): %v", err)
	}

	if st := statusOf(t, e, queued); st.State != engine.StateQueued {
		t.Fatalf("second torrent state = %s, want queued", st.State)
	}

	free.Store(0)

	if err := e.Remove(slot, false); err != nil {
		t.Fatalf("Remove(slot): %v", err)
	}

	if st := waitForState(t, e, queued, engine.StateErrored); !errors.Is(st.Err, ErrInsufficientSpace) {
		t.Fatalf("promoted start: err %v, want ErrInsufficientSpace", st.Err)
	}

	free.Store(1 << 40)

	again, err := e.Add(ctx, engine.AddSource{FilePath: path})
	if err != nil {
		t.Fatalf("re-Add: %v", err)
	}

	if again == queued {
		t.Fatalf("re-Add returned the errored entry %s", queued)
	}

	if listed(e, queued) {
		t.Errorf("the errored entry %s is still listed after the re-Add", queued)
	}

	waitForState(t, e, again, engine.StateDownloading)
}
