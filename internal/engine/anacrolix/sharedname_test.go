package anacrolix

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"

	"github.com/kdta91/tortui/internal/engine"
)

// refusedWithLeftData restores, into a fresh engine whose free space the
// test sets, a torrent named name that cannot resume for lack of space, with
// partial data at <download dir>/name/a.bin: a refused entry whose left data
// is that directory (T-9128). The engine has maxActive download slots.
func refusedWithLeftData(t *testing.T, name string, maxActive int) (e *Engine, id, left string, free *atomic.Uint64) {
	t.Helper()

	free = &atomic.Uint64{}
	free.Store(1 << 40)

	e = newTestEngine(t, func(o *Options) {
		o.Config.MaxActiveDownloads = maxActive
		o.MetadataTimeout = time.Hour
		o.freeSpace = func(string) (uint64, error) { return free.Load(), nil }
	})

	left = writePartial(t, e.downloadDir, name, "a.bin")

	free.Store(0)

	id, err := e.Restore(context.Background(), engine.ResumeData{
		ID: "an-90", Name: name, SavePath: e.downloadDir,
		Metainfo: encodeTorrent(t, buildInfo(name, [][]string{{"a.bin"}})),
	})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	free.Store(1 << 40)

	e.mu.Lock()
	leftName := e.torrents[id].leftName
	e.mu.Unlock()

	if leftName != name {
		t.Fatalf("refused entry left %q, want %q", leftName, name)
	}

	return e, id, left, free
}

// removeKeeps removes id with its data and asserts the remove kept the data
// at left, saying so (maybe or not), and still removed the entry.
func removeKeeps(t *testing.T, e *Engine, id, left string, maybe bool) {
	t.Helper()

	err := e.Remove(id, true)

	var kept *engine.DataKeptError
	if !errors.As(err, &kept) || kept.Path != left || kept.Maybe != maybe {
		t.Fatalf("Remove(with data) = %#v, want a DataKeptError naming %s with Maybe %v", err, left, maybe)
	}

	if listed(e, id) {
		t.Error("the entry is still listed after a kept-data remove")
	}

	if _, err := os.Stat(filepath.Join(left, "a.bin")); err != nil {
		t.Fatalf("the data another download uses went with the remove: %v", err)
	}
}

// TestRemoveWithDataSparesAQueuedTorrentSharingItsName is PR #101 review note
// 3: a torrent waiting in the queue with its .torrent in hand will write its
// data under its name at its destination. A refused entry with data under the
// same name there does not delete it on a remove with data.
func TestRemoveWithDataSparesAQueuedTorrentSharingItsName(t *testing.T) {
	t.Parallel()

	e, id, left, _ := refusedWithLeftData(t, "queued-twin", 1)
	ctx := context.Background()

	if _, err := e.Add(ctx, engine.AddSource{Magnet: magnetURI("queued-twin-slot")}); err != nil {
		t.Fatalf("Add(slot): %v", err)
	}

	// Same name, different files: a different torrent.
	queued, err := e.Add(ctx, engine.AddSource{
		FilePath: writeTorrentFile(t, buildInfo("queued-twin", [][]string{{"b.bin"}})),
	})
	if err != nil {
		t.Fatalf("Add(queued): %v", err)
	}

	if st := statusOf(t, e, queued); st.State != engine.StateQueued {
		t.Fatalf("state = %s, want queued", st.State)
	}

	removeKeeps(t, e, id, left, false)
}

// TestRemoveWithDataSparesARestoringTorrentWithNoNameYet is PR #101 review
// note 3: a torrent restored at the same destination that has not named its
// data yet — a magnet waiting for its info dictionary, a .torrent address
// still being fetched — may write under the refused entry's name. Its data
// is kept, and the remove says another download may use it (DEC-167).
func TestRemoveWithDataSparesARestoringTorrentWithNoNameYet(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		restore func(t *testing.T, e *Engine) engine.ResumeData
	}{
		{name: "magnet waiting for its info", restore: func(_ *testing.T, e *Engine) engine.ResumeData {
			return engine.ResumeData{
				ID: "an-91", Name: "restoring-twin", SavePath: e.downloadDir,
				Magnet: magnetURI("restoring-twin"),
			}
		}},
		{name: "address being fetched", restore: func(t *testing.T, e *Engine) engine.ResumeData {
			return engine.ResumeData{
				ID: "an-91", Name: "restoring-twin", SavePath: e.downloadDir,
				TorrentURL: stalledAddress(t),
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			e, id, left, _ := refusedWithLeftData(t, "restoring-twin", 3)

			restoring, err := e.Restore(context.Background(), tc.restore(t, e))
			if err != nil {
				t.Fatalf("Restore(restoring): %v", err)
			}

			if st := statusOf(t, e, restoring); st.State != engine.StateChecking {
				t.Fatalf("restoring torrent state = %s, want checking", st.State)
			}

			removeKeeps(t, e, id, left, true)

			if st := statusOf(t, e, restoring); st.State != engine.StateChecking {
				t.Errorf("restoring torrent state = %s after the remove, want it untouched", st.State)
			}
		})
	}
}

// stalledAddress is a loopback .torrent address whose fetch stays in flight
// until the test ends.
func stalledAddress(t *testing.T) string {
	t.Helper()

	address, _ := serveGatedTorrent(t, encodeTorrent(t, buildInfo("restoring-twin", [][]string{{"c.bin"}})))

	return address
}

// TestRemoveWithDataOfAnEntryWhoseNameWasAnothersKeepsIt is PR #101 review
// note 4: a refused entry whose data name was another download's when it was
// refused (sharedName) never deletes that data, even once the other download
// is removed with its data kept; the remove says the data was kept, not that
// it was deleted. When the other download took its data with it, nothing is
// left to keep and the remove reports plain success.
func TestRemoveWithDataOfAnEntryWhoseNameWasAnothersKeepsIt(t *testing.T) {
	t.Parallel()

	for _, otherDeletes := range []bool{false, true} {
		t.Run(map[bool]string{false: "other kept its data", true: "other deleted its data"}[otherDeletes], func(t *testing.T) {
			t.Parallel()

			var free atomic.Uint64
			free.Store(1 << 40)

			e := newTestEngine(t, func(o *Options) {
				o.MetadataTimeout = time.Hour
				o.freeSpace = func(string) (uint64, error) { return free.Load(), nil }
			})

			ctx := context.Background()
			shared := writePartial(t, e.downloadDir, "was-twin", "a.bin")

			other, err := e.Add(ctx, engine.AddSource{
				FilePath: writeTorrentFile(t, buildInfo("was-twin", [][]string{{"a.bin"}})),
			})
			if err != nil {
				t.Fatalf("Add(other): %v", err)
			}

			infoChecked(t, e, other)

			free.Store(0)

			id, err := e.Restore(ctx, engine.ResumeData{
				ID: "an-92", Name: "was-twin", SavePath: e.downloadDir,
				Metainfo: encodeTorrent(t, buildInfo("was-twin", [][]string{{"b.bin"}})),
			})
			if err != nil {
				t.Fatalf("Restore: %v", err)
			}

			e.mu.Lock()
			sharedName := e.torrents[id].sharedName
			e.mu.Unlock()

			if sharedName != "was-twin" {
				t.Fatalf("sharedName = %q, want %q", sharedName, "was-twin")
			}

			free.Store(1 << 40)

			if err := e.Remove(other, otherDeletes); err != nil {
				t.Fatalf("Remove(other, %v): %v", otherDeletes, err)
			}

			if otherDeletes {
				if err := e.Remove(id, true); err != nil {
					t.Fatalf("Remove(with data) with nothing left = %v, want nil", err)
				}

				return
			}

			removeKeeps(t, e, id, shared, true)
		})
	}
}

// twinInfo is the info dictionary of one of two different torrents that
// share the name name: the file it declares tells them apart.
func twinInfo(name, file string) metainfo.Info {
	return buildInfo(name, [][]string{{file}})
}

// spaceTestEngine is an engine with maxActive download slots whose free space
// the test sets, writing under dir (a fresh directory when dir is "").
func spaceTestEngine(t *testing.T, dir string, maxActive int) (*Engine, *atomic.Uint64) {
	t.Helper()

	free := &atomic.Uint64{}
	free.Store(1 << 40)

	e := newTestEngine(t, func(o *Options) {
		if dir != "" {
			o.Config.DownloadDir = dir
		}

		o.Config.MaxActiveDownloads = maxActive
		o.MetadataTimeout = time.Hour
		o.freeSpace = func(string) (uint64, error) { return free.Load(), nil }
	})

	return e, free
}

// refusedInQueueOrder sets up T-9177's state: with data under name already on
// disk at the download directory (shared, returned), two different torrents
// named name queue behind a one-slot magnet, and removing the slot with no
// free space refuses them in queue order. The queue orders the refusals with
// no timing involved: the second cannot start until the first is refused and
// frees the only slot. It returns the engine and both IDs, first refused
// first, with the free space restored.
func refusedInQueueOrder(t *testing.T, name string) (e *Engine, first, second, shared string) {
	t.Helper()

	e, free := spaceTestEngine(t, "", 1)
	ctx := context.Background()
	shared = writePartial(t, e.downloadDir, name, "a.bin")

	slot, err := e.Add(ctx, engine.AddSource{Magnet: magnetURI(name + "-slot")})
	if err != nil {
		t.Fatalf("Add(slot): %v", err)
	}

	var ids [2]string

	for i, file := range []string{"a.bin", "b.bin"} {
		ids[i], err = e.Add(ctx, engine.AddSource{FilePath: writeTorrentFile(t, twinInfo(name, file))})
		if err != nil {
			t.Fatalf("Add(%s): %v", file, err)
		}

		if st := statusOf(t, e, ids[i]); st.State != engine.StateQueued {
			t.Fatalf("torrent with %s: state = %s, want queued", file, st.State)
		}
	}

	free.Store(0)

	if err := e.Remove(slot, false); err != nil {
		t.Fatalf("Remove(slot): %v", err)
	}

	for _, id := range ids {
		if st := waitForState(t, e, id, engine.StateErrored); !errors.Is(st.Err, ErrInsufficientSpace) {
			t.Fatalf("torrent %s: err %v, want ErrInsufficientSpace", id, st.Err)
		}
	}

	free.Store(1 << 40)

	return e, ids[0], ids[1], shared
}

// keptNames returns the leftName and sharedName of tracked entry id.
func keptNames(e *Engine, id string) (left, shared string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.torrents[id].leftName, e.torrents[id].sharedName
}

// restoreTwin restores, as record id, the torrent twinInfo(name, file) at
// e's download directory, returning its tracked ID.
func restoreTwin(t *testing.T, e *Engine, id, name, file string) string {
	t.Helper()

	got, err := e.Restore(context.Background(), engine.ResumeData{
		ID: id, Name: name, SavePath: e.downloadDir,
		Metainfo: encodeTorrent(t, twinInfo(name, file)),
	})
	if err != nil {
		t.Fatalf("Restore(%s): %v", id, err)
	}

	return got
}

// TestARefusedEntryNeverClaimsDataAnEarlierRefusalLeftAsShared is T-9177:
// two torrents with one name queue at one destination where data under that
// name is already on disk. The first is refused when it starts and, since
// the second is queued under the same name, keeps that data as shared, not
// its own. The second, refused next, must not then claim the data as its own
// left data: the first deferred to it only while it was queued, and the data
// may be the first's. Neither remove with data deletes it.
//
// This is the interleaving the T-9177 flake hit at random, where the
// free-space check after a torrent's info arrived refused the other torrent
// before the queued one was refused.
func TestARefusedEntryNeverClaimsDataAnEarlierRefusalLeftAsShared(t *testing.T) {
	t.Parallel()

	e, first, second, shared := refusedInQueueOrder(t, "queue-twin")

	if _, firstShared := keptNames(e, first); firstShared != "queue-twin" {
		t.Fatalf("first refusal: sharedName = %q, want %q (the queued torrent had the name)", firstShared, "queue-twin")
	}

	if left, sh := keptNames(e, second); left != "" || sh != "queue-twin" {
		t.Errorf("second refusal: leftName %q, sharedName %q; want none and %q", left, sh, "queue-twin")
	}

	removeKeeps(t, e, second, shared, true)
	removeKeeps(t, e, first, shared, true)
}

// TestRemoveWithDataOfALeftNameKeepsDataAnotherRefusalKeptAsShared is the
// T-9177 review finding: one refused entry holding the data as its own left
// data (leftName) and another refused entry at the same destination holding
// it as shared (sharedName) is reachable without T-9177's queue order, and
// which of the two holds which follows only the order they were refused or
// restored in. The leftName holder's remove with data keeps the data while
// the other is tracked, and says another download may use it.
func TestRemoveWithDataOfALeftNameKeepsDataAnotherRefusalKeptAsShared(t *testing.T) {
	t.Parallel()

	const name = "boot-twin"

	// Two different same-name torrents restored, one after the other, onto
	// a full disk at startup: the first restored takes the data as its own.
	t.Run("restored onto a full disk", func(t *testing.T) {
		t.Parallel()

		e, free := spaceTestEngine(t, "", 2)
		shared := writePartial(t, e.downloadDir, name, "a.bin")

		free.Store(0)

		owner := restoreTwin(t, e, "an-93", name, "a.bin")
		other := restoreTwin(t, e, "an-94", name, "b.bin")

		free.Store(1 << 40)

		if left, _ := keptNames(e, owner); left != name {
			t.Fatalf("first restored: leftName = %q, want %q", left, name)
		}

		if left, sh := keptNames(e, other); left != "" || sh != name {
			t.Fatalf("second restored: leftName %q, sharedName %q; want none and %q", left, sh, name)
		}

		removeKeeps(t, e, owner, shared, true)
		removeKeeps(t, e, other, shared, true)
	})

	// T-9177's state (both shared) does not survive a restart: leftName and
	// sharedName are worked out again in restore order, so the entry refused
	// first, restored first, now takes the data as its own.
	t.Run("T-9177 state after a restart", func(t *testing.T) {
		t.Parallel()

		before, first, second, _ := refusedInQueueOrder(t, name)
		dir := before.downloadDir

		if err := before.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		e, free := spaceTestEngine(t, dir, 1)
		shared := filepath.Join(dir, name)

		free.Store(0)

		// The records as the session saves them, in add order (T-9180: a
		// queue-refused .torrent's own record carries no metainfo today).
		owner := restoreTwin(t, e, first, name, "a.bin")
		other := restoreTwin(t, e, second, name, "b.bin")

		free.Store(1 << 40)

		if left, _ := keptNames(e, owner); left != name {
			t.Fatalf("first restored: leftName = %q, want %q", left, name)
		}

		if _, sh := keptNames(e, other); sh != name {
			t.Fatalf("second restored: sharedName = %q, want %q", sh, name)
		}

		removeKeeps(t, e, owner, shared, true)
		removeKeeps(t, e, other, shared, true)
	})

	// Both restored at once: one takes the data as its own, whichever wins
	// the lock, and the other keeps it as shared. Each is removed first in
	// turn, picked by what it holds, not by the order it was restored in.
	for _, ownerFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("restored at once, owner removed first %v", ownerFirst), func(t *testing.T) {
			t.Parallel()

			e, free := spaceTestEngine(t, "", 2)
			shared := writePartial(t, e.downloadDir, name, "a.bin")

			free.Store(0)

			var (
				ids [2]string
				wg  sync.WaitGroup
			)

			for i, file := range []string{"a.bin", "b.bin"} {
				wg.Go(func() {
					got, err := e.Restore(context.Background(), engine.ResumeData{
						ID: fmt.Sprintf("an-%d", 95+i), Name: name, SavePath: e.downloadDir,
						Metainfo: encodeTorrent(t, twinInfo(name, file)),
					})
					if err != nil {
						t.Errorf("Restore(%s): %v", file, err)
					}

					ids[i] = got
				})
			}

			wg.Wait()

			if t.Failed() {
				return
			}

			free.Store(1 << 40)

			var owner, sharer string

			for _, id := range ids {
				switch left, sh := keptNames(e, id); {
				case left == name && sh == "" && owner == "":
					owner = id
				case left == "" && sh == name && sharer == "":
					sharer = id
				default:
					t.Fatalf("entry %s: leftName %q, sharedName %q; want one owner and one sharer", id, left, sh)
				}
			}

			if ownerFirst {
				// The sharer's claim keeps it; the sharer then keeps it
				// too, as data that was never its own.
				removeKeeps(t, e, owner, shared, true)
				removeKeeps(t, e, sharer, shared, true)

				return
			}

			// The owner's left data is taken: kept, not "may use".
			removeKeeps(t, e, sharer, shared, false)

			// Nothing else is tracked now, so the owner deletes its left
			// data (DEC-167).
			if err := e.Remove(owner, true); err != nil {
				t.Fatalf("Remove(owner, with data) alone = %v, want nil", err)
			}

			if _, err := os.Stat(shared); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the owner's left data is still there after its remove with data: %v", err)
			}
		})
	}
}
