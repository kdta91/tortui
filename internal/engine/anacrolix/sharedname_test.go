package anacrolix

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

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

// TestARefusedEntryNeverClaimsDataAnEarlierRefusalLeftAsShared is T-9177:
// two torrents with one name queue at one destination where data under that
// name is already on disk. The first is refused when it starts and, since
// the second is queued under the same name, keeps that data as shared, not
// its own. The second, refused next, must not then claim the data as its own
// left data: the first deferred to it only while it was queued, and the data
// may be the first's. Neither remove with data deletes it.
//
// The queue orders the two refusals with no timing involved: the second
// cannot start until the first is refused and frees the only slot. This is
// the interleaving the T-9177 flake hit at random, where the free-space
// check after a torrent's info arrived refused the other torrent before the
// queued one was refused.
func TestARefusedEntryNeverClaimsDataAnEarlierRefusalLeftAsShared(t *testing.T) {
	t.Parallel()

	var free atomic.Uint64
	free.Store(1 << 40)

	e := newTestEngine(t, func(o *Options) {
		o.Config.MaxActiveDownloads = 1
		o.MetadataTimeout = time.Hour
		o.freeSpace = func(string) (uint64, error) { return free.Load(), nil }
	})

	ctx := context.Background()
	shared := writePartial(t, e.downloadDir, "queue-twin", "a.bin")

	slot, err := e.Add(ctx, engine.AddSource{Magnet: magnetURI("queue-twin-slot")})
	if err != nil {
		t.Fatalf("Add(slot): %v", err)
	}

	var ids [2]string

	// Same name, different files: two different torrents.
	for i, file := range []string{"a.bin", "b.bin"} {
		ids[i], err = e.Add(ctx, engine.AddSource{
			FilePath: writeTorrentFile(t, buildInfo("queue-twin", [][]string{{file}})),
		})
		if err != nil {
			t.Fatalf("Add(%s): %v", file, err)
		}

		if st := statusOf(t, e, ids[i]); st.State != engine.StateQueued {
			t.Fatalf("torrent with %s: state = %s, want queued", file, st.State)
		}
	}

	first, second := ids[0], ids[1]

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

	e.mu.Lock()
	firstShared := e.torrents[first].sharedName
	secondLeft, secondShared := e.torrents[second].leftName, e.torrents[second].sharedName
	e.mu.Unlock()

	if firstShared != "queue-twin" {
		t.Fatalf("first refusal: sharedName = %q, want %q (the queued torrent had the name)", firstShared, "queue-twin")
	}

	if secondLeft != "" || secondShared != "queue-twin" {
		t.Errorf("second refusal: leftName %q, sharedName %q; want none and %q",
			secondLeft, secondShared, "queue-twin")
	}

	removeKeeps(t, e, second, shared, true)
	removeKeeps(t, e, first, shared, true)
}
