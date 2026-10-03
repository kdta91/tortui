package anacrolix

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

// TestReAddOfASpacePausedTorrentHandsBackTheSameEntry is the other side of
// DEC-162: a torrent the free-space re-check paused shows StateErrored but is
// still attached, with its data, so it is not refused. Adding its infohash
// again hands back the same ID, and the client still holds one torrent.
func TestReAddOfASpacePausedTorrentHandsBackTheSameEntry(t *testing.T) {
	t.Parallel()

	var free atomic.Uint64
	free.Store(1 << 40)

	e := newTestEngine(t, func(o *Options) {
		o.Config.MinFreeSpace = "1MB"
		o.SpaceCheckInterval = time.Millisecond
		o.MetadataTimeout = time.Hour
		o.freeSpace = func(string) (uint64, error) { return free.Load(), nil }
	})

	infoBytes, magnet := refusedFixture(t, "space-paused")

	id, err := e.Add(context.Background(), engine.AddSource{Magnet: magnet})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	supplyInfo(t, e, id, infoBytes)
	waitForState(t, e, id, engine.StateDownloading)

	free.Store(512 << 10) // under the 1 MiB margin alone

	if st := waitForState(t, e, id, engine.StateErrored); !errors.Is(st.Err, ErrInsufficientSpace) {
		t.Fatalf("err %v, want the space pause's ErrInsufficientSpace", st.Err)
	}

	// Not the user's pause: a restart does not bring it back paused (T-952).
	if pausedOf(t, e, id) {
		t.Error("ResumeData.Paused of a space-paused torrent = true, want false")
	}

	// A magnet carries no size, so the re-add is not refused up front.
	again, err := e.Add(context.Background(), engine.AddSource{Magnet: magnet})
	if err != nil {
		t.Fatalf("re-Add: %v", err)
	}

	if again != id {
		t.Errorf("re-Add of a space-paused torrent = %s, want the same entry %s", again, id)
	}

	if n := len(e.List()); n != 1 {
		t.Errorf("List has %d entries, want 1", n)
	}

	if n := len(e.client.Torrents()); n != 1 {
		t.Errorf("client holds %d torrents, want 1", n)
	}
}

// TestReAddElsewhereOfARefusedTorrentWithDataIsRefused is T-9127 (Backlog
// T-9126, DEC-163). A restored torrent whose queued start fails is refused
// with its partial data still at its destination. Adding it again to another
// destination would leave that data with nothing tracking it, so that add is
// refused, naming the data, and the errored entry stays for the user to
// remove, with or without its data. An add to the same destination, or one
// whose refused entry left nothing on disk, starts over as before (DEC-162).
func TestReAddElsewhereOfARefusedTorrentWithDataIsRefused(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		data      bool   // partial data from an earlier session is on disk
		elsewhere bool   // the re-add picks another destination
		via       string // how the re-add names the torrent: "" a file, or an address
		// that serves it ("address") or redirects to its magnet ("redirect"),
		// which fail after Add returns
		deleted bool // the user deletes the left data by hand before the re-add (T-9129)
	}{
		{name: "data, another destination", data: true, elsewhere: true},
		{name: "data deleted by hand, another destination", data: true, elsewhere: true, deleted: true},
		{name: "data deleted by hand, another destination, by address", data: true, elsewhere: true, via: "address", deleted: true},
		{name: "data deleted by hand, another destination, by redirect", data: true, elsewhere: true, via: "redirect", deleted: true},
		{name: "data, another destination, by address", data: true, elsewhere: true, via: "address"},
		{name: "data, another destination, by redirect", data: true, elsewhere: true, via: "redirect"},
		{name: "data, same destination", data: true},
		{name: "no data, another destination", elsewhere: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var free atomic.Uint64
			free.Store(1 << 40)

			e := newTestEngine(t, func(o *Options) {
				o.Config.MaxActiveDownloads = 1
				o.MetadataTimeout = time.Hour
				o.freeSpace = func(string) (uint64, error) { return free.Load(), nil }
			})

			ctx := context.Background()
			first := filepath.Join(e.downloadDir, "first")
			second := filepath.Join(e.downloadDir, "second")
			info := buildInfo("left-behind", [][]string{{"q.bin"}})
			left := filepath.Join(first, "left-behind")

			if tc.data {
				if err := os.MkdirAll(left, 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}

				if err := os.WriteFile(filepath.Join(left, "q.bin"), []byte("partial"), 0o600); err != nil {
					t.Fatalf("write partial data: %v", err)
				}
			}

			slot, err := e.Add(ctx, engine.AddSource{Magnet: magnetURI("left-behind-slot")})
			if err != nil {
				t.Fatalf("Add(slot): %v", err)
			}

			restored, err := e.Restore(ctx, engine.ResumeData{
				ID: "an-9", Name: "left-behind", Metainfo: encodeTorrent(t, info), SavePath: first,
			})
			if err != nil {
				t.Fatalf("Restore: %v", err)
			}

			if st := statusOf(t, e, restored); st.State != engine.StateQueued {
				t.Fatalf("restored state = %s (err %v), want queued", st.State, st.Err)
			}

			free.Store(0)

			if err := e.Remove(slot, false); err != nil {
				t.Fatalf("Remove(slot): %v", err)
			}

			if st := waitForState(t, e, restored, engine.StateErrored); !errors.Is(st.Err, ErrInsufficientSpace) {
				t.Fatalf("promoted start: err %v, want ErrInsufficientSpace", st.Err)
			}

			free.Store(1 << 40)

			if tc.deleted {
				if err := os.RemoveAll(left); err != nil {
					t.Fatalf("delete the left data by hand: %v", err)
				}
			}

			dest := first
			if tc.elsewhere {
				dest = second
			}

			path := writeTorrentFile(t, info)

			infoBytes, err := bencode.Marshal(info)
			if err != nil {
				t.Fatalf("bencode info: %v", err)
			}

			hash := metainfo.HashBytes(infoBytes).HexString()

			src := engine.AddSource{FilePath: path, SavePath: dest}

			switch tc.via {
			case "address":
				src = engine.AddSource{TorrentURL: serveTorrent(t, encodeTorrent(t, info)), SavePath: dest}
			case "redirect":
				address, _ := serveMagnetRedirect(t, http.StatusFound, "magnet:?xt=urn:btih:"+hash)
				src = engine.AddSource{TorrentURL: address, SavePath: dest}
			}

			again, err := e.Add(ctx, src)
			if tc.via != "" && err == nil && !tc.deleted {
				st := waitForState(t, e, again, engine.StateErrored)
				err = st.Err

				if rmErr := e.Remove(again, false); rmErr != nil {
					t.Fatalf("Remove(the failed address add): %v", rmErr)
				}
			}

			if !tc.data || !tc.elsewhere || tc.deleted {
				if err != nil {
					t.Fatalf("re-Add: %v", err)
				}

				// An address add claims the infohash once its fetch is done:
				// wait for that before looking for the refused entry.
				if tc.via == "redirect" {
					// A magnet stays checking offline.
					waitUntil(t, "the redirect's magnet to be added", func() bool {
						return statusOf(t, e, again).InfoHash == hash
					})
				} else {
					waitForState(t, e, again, engine.StateDownloading)
				}

				if listed(e, restored) {
					t.Errorf("the refused entry %s is still listed after the re-Add", restored)
				}

				if tc.data && !tc.deleted {
					if _, err := os.Stat(filepath.Join(left, "q.bin")); err != nil {
						t.Errorf("the partial data was touched: %v", err)
					}
				}

				return
			}

			if !errors.Is(err, ErrLeftData) || !strings.Contains(err.Error(), left) {
				t.Fatalf("re-Add elsewhere = %q, %v; want ErrLeftData naming %s", again, err, left)
			}

			if st := statusOf(t, e, restored); st.State != engine.StateErrored {
				t.Fatalf("refused entry state = %s, want it still listed, errored", st.State)
			}

			if _, err := os.Stat(filepath.Join(left, "q.bin")); err != nil {
				t.Fatalf("the refused add touched the partial data: %v", err)
			}

			// The user removes the errored entry with its data, then adds
			// the torrent where they want it.
			if err := e.Remove(restored, true); err != nil {
				t.Fatalf("Remove(refused, with data): %v", err)
			}

			if _, err := os.Lstat(left); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("%s still exists after remove with data (stat err %v)", left, err)
			}

			again, err = e.Add(ctx, src)
			if err != nil {
				t.Fatalf("Add after the remove: %v", err)
			}

			if tc.via != "redirect" {
				waitForState(t, e, again, engine.StateDownloading)
				return
			}

			// A magnet stays checking offline: wait for the switch to it.
			waitUntil(t, "the redirect's magnet to be added", func() bool {
				st := statusOf(t, e, again)
				return st.InfoHash == hash || st.State == engine.StateErrored
			})

			if st := statusOf(t, e, again); st.State == engine.StateErrored {
				t.Fatalf("add after the remove errored: %v", st.Err)
			}
		})
	}
}
