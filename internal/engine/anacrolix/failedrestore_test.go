package anacrolix

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
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

// infoHashOf is the hex infohash of info.
func infoHashOf(t *testing.T, info metainfo.Info) string {
	t.Helper()

	b, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("bencode info: %v", err)
	}

	return metainfo.HashBytes(b).HexString()
}

// writePartial puts partial data for a one-file torrent at dir/name/file.
func writePartial(t *testing.T, dir, name, file string) string {
	t.Helper()

	root := filepath.Join(dir, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, file), []byte("partial"), 0o600); err != nil {
		t.Fatalf("write partial data: %v", err)
	}

	return root
}

// spaceEngine is a test engine whose free-space reading the test sets.
func spaceEngine(t *testing.T) (*Engine, *atomic.Uint64) {
	t.Helper()

	var free atomic.Uint64
	free.Store(1 << 40)

	e := newTestEngine(t, func(o *Options) {
		o.MetadataTimeout = time.Hour
		o.freeSpace = func(string) (uint64, error) { return free.Load(), nil }
	})

	return e, &free
}

// TestFailedRestoreKeepsItsInfohashAndDataName is T-9128. A torrent that
// cannot be restored for lack of free space, with partial data on disk,
// carries its infohash and its data's name like a refused entry (T-9127):
// remove with data deletes that data, an add to another destination is
// refused while the data is there, and an add to the same destination starts
// over.
func TestFailedRestoreKeepsItsInfohashAndDataName(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		then string // "remove", "elsewhere", or "same"
	}{
		{name: "remove with data", then: "remove"},
		{name: "re-add elsewhere", then: "elsewhere"},
		{name: "re-add to the same destination", then: "same"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			e, free := spaceEngine(t)
			ctx := context.Background()
			first := filepath.Join(e.downloadDir, "first")
			info := buildInfo("restore-left", [][]string{{"q.bin"}})
			left := writePartial(t, first, "restore-left", "q.bin")

			free.Store(0)

			id, err := e.Restore(ctx, engine.ResumeData{
				ID: "an-7", Name: "restore-left", Metainfo: encodeTorrent(t, info), SavePath: first,
			})
			if err != nil {
				t.Fatalf("Restore: %v", err)
			}

			st := statusOf(t, e, id)
			if st.State != engine.StateErrored || !errors.Is(st.Err, ErrInsufficientSpace) {
				t.Fatalf("restored state %s err %v, want errored for lack of space", st.State, st.Err)
			}

			if want := infoHashOf(t, info); st.InfoHash != want {
				t.Fatalf("failed restore InfoHash = %q, want %q", st.InfoHash, want)
			}

			free.Store(1 << 40)

			switch tc.then {
			case "remove":
				if err := e.Remove(id, true); err != nil {
					t.Fatalf("Remove(with data): %v", err)
				}

				if _, err := os.Lstat(left); !errors.Is(err, fs.ErrNotExist) {
					t.Fatalf("%s still exists after remove with data (stat err %v)", left, err)
				}

			case "elsewhere":
				src := engine.AddSource{FilePath: writeTorrentFile(t, info), SavePath: filepath.Join(e.downloadDir, "second")}

				if _, err := e.Add(ctx, src); !errors.Is(err, ErrLeftData) || !strings.Contains(err.Error(), left) {
					t.Fatalf("re-Add elsewhere = %v, want ErrLeftData naming %s", err, left)
				}

				if st := statusOf(t, e, id); st.State != engine.StateErrored {
					t.Fatalf("failed entry state = %s, want it still listed", st.State)
				}

			case "same":
				again, err := e.Add(ctx, engine.AddSource{FilePath: writeTorrentFile(t, info), SavePath: first})
				if err != nil {
					t.Fatalf("re-Add to the same destination: %v", err)
				}

				if again == id || listed(e, id) {
					t.Fatalf("re-Add = %s with the failed entry %s listed %v; want a fresh entry, the old one gone",
						again, id, listed(e, id))
				}

				if _, err := os.Stat(filepath.Join(left, "q.bin")); err != nil {
					t.Errorf("the partial data was touched: %v", err)
				}
			}
		})
	}
}

// TestFailedRestoreNamesNoDataOutsideAKnownRoot: a restore whose destination
// is no longer a known root is tracked with its infohash, but nothing there
// is named as its data, so a re-add elsewhere starts over rather than being
// refused over data tortui could not delete.
func TestFailedRestoreNamesNoDataOutsideAKnownRoot(t *testing.T) {
	t.Parallel()

	e, _ := spaceEngine(t)
	ctx := context.Background()
	outside := t.TempDir()
	info := buildInfo("outside-left", [][]string{{"q.bin"}})
	left := writePartial(t, outside, "outside-left", "q.bin")

	id, err := e.Restore(ctx, engine.ResumeData{
		ID: "an-8", Name: "outside-left", Metainfo: encodeTorrent(t, info), SavePath: outside,
	})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if st := statusOf(t, e, id); st.State != engine.StateErrored || st.InfoHash != infoHashOf(t, info) {
		t.Fatalf("restored state %s infohash %q, want errored with the infohash", st.State, st.InfoHash)
	}

	again, err := e.Add(ctx, engine.AddSource{FilePath: writeTorrentFile(t, info)})
	if err != nil {
		t.Fatalf("re-Add: %v", err)
	}

	if listed(e, id) {
		t.Errorf("the failed entry %s is still listed after the re-Add %s", id, again)
	}

	if _, err := os.Stat(filepath.Join(left, "q.bin")); err != nil {
		t.Errorf("data outside every root was touched: %v", err)
	}
}

// TestFailedRestoreOfAMagnetIsMatchedOnReAdd: a magnet record that cannot be
// restored still carries the infohash its magnet names, so a re-add of that
// magnet replaces it instead of listing the torrent twice.
func TestFailedRestoreOfAMagnetIsMatchedOnReAdd(t *testing.T) {
	t.Parallel()

	e, _ := spaceEngine(t)
	ctx := context.Background()
	outside := t.TempDir()
	magnet := magnetURI("failed-magnet")

	id, err := e.Restore(ctx, engine.ResumeData{ID: "an-9", Name: "failed-magnet", Magnet: magnet, SavePath: outside})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if st := statusOf(t, e, id); st.State != engine.StateErrored || st.InfoHash == "" {
		t.Fatalf("restored state %s infohash %q, want errored with the infohash", st.State, st.InfoHash)
	}

	if _, err := e.Add(ctx, engine.AddSource{Magnet: magnet}); err != nil {
		t.Fatalf("re-Add: %v", err)
	}

	if n := len(e.List()); n != 1 || listed(e, id) {
		t.Errorf("List has %d entries with the failed one listed %v; want just the re-add", n, listed(e, id))
	}
}

// TestRemoveWithDataSaysWhenItKeptAnotherDownloadsData is PR #100 review note
// b: a failed restore whose name another tracked torrent's data uses at the
// same destination never deletes that data, and a remove with data says it
// kept it, and where, in an error and a warning, instead of nothing.
func TestRemoveWithDataSaysWhenItKeptAnotherDownloadsData(t *testing.T) {
	t.Parallel()

	log := &lockedBuffer{}

	var free atomic.Uint64
	free.Store(1 << 40)

	e := newTestEngine(t, func(o *Options) {
		o.Logger = slog.New(slog.NewTextHandler(log, nil))
		o.MetadataTimeout = time.Hour
		o.freeSpace = func(string) (uint64, error) { return free.Load(), nil }
	})

	ctx := context.Background()
	dest := e.downloadDir
	shared := writePartial(t, dest, "twin-restore", "a.bin")

	other, err := e.Add(ctx, engine.AddSource{FilePath: writeTorrentFile(t, buildInfo("twin-restore", [][]string{{"a.bin"}}))})
	if err != nil {
		t.Fatalf("Add(other): %v", err)
	}

	infoChecked(t, e, other)

	free.Store(0)

	// Same name, different files: a different torrent.
	id, err := e.Restore(ctx, engine.ResumeData{
		ID: "an-20", Name: "twin-restore", SavePath: dest,
		Metainfo: encodeTorrent(t, buildInfo("twin-restore", [][]string{{"b.bin"}})),
	})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if st := statusOf(t, e, id); st.State != engine.StateErrored {
		t.Fatalf("restored state = %s, want errored", st.State)
	}

	err = e.Remove(id, true)

	var kept *engine.DataKeptError
	if !errors.As(err, &kept) || kept.Path != shared {
		t.Fatalf("Remove(with data) = %v, want a DataKeptError naming %s", err, shared)
	}

	if listed(e, id) {
		t.Error("the entry is still listed: a kept-data remove still removes it")
	}

	if _, err := os.Stat(filepath.Join(shared, "a.bin")); err != nil {
		t.Errorf("the other download's data went with the remove: %v", err)
	}

	if out := log.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "kept its data") {
		t.Errorf("no warning logged for the kept data; log:\n%s", out)
	}
}
