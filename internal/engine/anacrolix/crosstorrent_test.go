package anacrolix

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"github.com/kdta91/tortui/internal/engine"
)

// openTwin opens info in s as the torrent hashed from seed and returns its
// fileTorrent: two seeds give two torrents with one name and one file list.
func openTwin(t *testing.T, s *fileStore, info *metainfo.Info, seed string) *fileTorrent {
	t.Helper()

	s.mu.Lock()
	before := make(map[*fileTorrent]bool, len(s.torrents))
	for ft := range s.torrents {
		before[ft] = true
	}
	s.mu.Unlock()

	if _, err := s.OpenTorrent(context.Background(), info, metainfo.HashBytes([]byte(seed))); err != nil {
		t.Fatalf("OpenTorrent(%s): %v", seed, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for ft := range s.torrents {
		if !before[ft] {
			return ft
		}
	}

	t.Fatalf("OpenTorrent(%s) added no torrent", seed)

	return nil
}

// newGroupStore is a file store at dir in group, closed at cleanup.
func newGroupStore(t *testing.T, dir string, group *storeGroup) *fileStore {
	t.Helper()

	s := newFileStore(dir, storage.NewMapPieceCompletion(), discardLogger())
	s.group = group
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("store Close: %v", err)
		}
	})

	return s
}

// TestFileStoreDiscardKeepsAFileAnotherTorrentOpened is Backlog T-9131: torrent
// A creates a file, and is held before it installs its handle; torrent B, with
// the same name at the same destination, opens that file and installs its own
// handle, writing nothing yet. When A finds its storage closed it must not
// remove the empty file B holds open, whose writes would land in a deleted
// file — whether B opened it through the same destination or through another
// path to the same directory (a symlinked alias, or a case variant on a
// case-insensitive file system).
func TestFileStoreDiscardKeepsAFileAnotherTorrentOpened(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		alias func(t *testing.T, dir string) string // B's destination; "" for dir itself
	}{
		{name: "same destination"},
		{name: "symlinked destination", alias: symlinkedDir},
		{name: "case-variant destination", alias: caseVariantDir},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "Dest")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}

			otherDir := dir
			if tc.alias != nil {
				otherDir = tc.alias(t, dir)
			}

			group := newStoreGroup()
			s := newGroupStore(t, dir, group)
			sb := s
			if otherDir != dir {
				sb = newGroupStore(t, otherDir, group)
			}

			info := storeFixture("twin", 1000, 1000)
			a := openTwin(t, s, info, "twin-a")
			b := openTwin(t, sb, info, "twin-b")
			created := filepath.Join(dir, "twin", "f01.bin")

			entered, release := holdCreate(s, created)
			stuckDone := make(chan error, 1)

			go func() { stuckDone <- a.withFile(1, true, func(*os.File) error { return nil }) }()

			<-entered

			var held *os.File
			if err := b.withFile(1, true, func(f *os.File) error {
				held = f
				return nil
			}); err != nil {
				t.Fatalf("B's open of the file A created: %v", err)
			}

			closeDone := startClose(t, a, "a created file's open was in flight")

			close(release)

			if err := wait(t, closeDone, "Close"); err != nil {
				t.Errorf("Close: %v", err)
			}

			if err := wait(t, stuckDone, "the held create"); !errors.Is(err, errStorageClosed) {
				t.Errorf("held create = %v, want errStorageClosed", err)
			}

			onDisk, err := os.Stat(created)
			if err != nil {
				t.Fatalf("A's discard removed the file B holds open: %v", err)
			}

			heldInfo, err := held.Stat()
			if err != nil {
				t.Fatalf("stat B's handle: %v", err)
			}

			if !os.SameFile(onDisk, heldInfo) {
				t.Error("B's handle is not the file on disk: its writes would be lost")
			}
		})
	}
}

// TestFileStoreDiscardStillRemovesAFileNoOtherTorrentDeclares: the group check
// keeps only what another torrent declares. A torrent with another name in
// the same group does not stop a discard removing the empty file it made.
func TestFileStoreDiscardStillRemovesAFileNoOtherTorrentDeclares(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s := newGroupStore(t, dir, newStoreGroup())
	a := openTwin(t, s, storeFixture("alone", 1000, 1000), "alone-a")
	openTwin(t, s, storeFixture("other", 1000, 1000), "other-b")
	created := filepath.Join(dir, "alone", "f01.bin")

	entered, release := holdCreate(s, created)
	stuckDone := make(chan error, 1)

	go func() { stuckDone <- a.withFile(1, true, func(*os.File) error { return nil }) }()

	<-entered

	closeDone := startClose(t, a, "a created file's open was in flight")

	close(release)

	if err := wait(t, closeDone, "Close"); err != nil {
		t.Errorf("Close: %v", err)
	}

	if err := wait(t, stuckDone, "the held create"); !errors.Is(err, errStorageClosed) {
		t.Errorf("held create = %v, want errStorageClosed", err)
	}

	requireGone(t, created)
	requireGone(t, filepath.Dir(created))
}

// TestRemoveWithDataSparesALiveTorrentSharingItsName is Backlog T-9131: two
// live torrents with one name at one destination keep their data in one
// place. A remove with data of one keeps it, says another download uses it,
// and still removes the entry; once the other is gone, its own remove with
// data deletes it. Through a symlinked alias of the destination, or a case
// variant on a case-insensitive file system, the data is kept just the same.
func TestRemoveWithDataSparesALiveTorrentSharingItsName(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		alias func(t *testing.T, dir string) string
	}{
		{name: "same destination"},
		{name: "symlinked destination", alias: symlinkedDir},
		{name: "case-variant destination", alias: caseVariantDir},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			e := newTestEngine(t, func(o *Options) { o.Config.MaxActiveDownloads = 4 })
			ctx := context.Background()

			dest := filepath.Join(e.downloadDir, "Dest")
			if err := os.Mkdir(dest, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}

			other := dest
			if tc.alias != nil {
				other = tc.alias(t, dest)
			}

			first, err := e.Add(ctx, engine.AddSource{
				FilePath: writeTorrentFile(t, buildInfo("twin", [][]string{{"a.bin"}})), SavePath: dest,
			})
			if err != nil {
				t.Fatalf("Add(first): %v", err)
			}

			// Same name, different files: a different torrent.
			second, err := e.Add(ctx, engine.AddSource{
				FilePath: writeTorrentFile(t, buildInfo("twin", [][]string{{"b.bin"}})), SavePath: other,
			})
			if err != nil {
				t.Fatalf("Add(second): %v", err)
			}

			waitForStarted(t, e, first)
			waitForStarted(t, e, second)

			shared := filepath.Join(dest, "twin")
			data := filepath.Join(shared, "a.bin")

			if err := os.MkdirAll(shared, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}

			if err := os.WriteFile(data, []byte("both torrents' data"), 0o600); err != nil {
				t.Fatalf("write: %v", err)
			}

			err = e.Remove(second, true)

			var kept *engine.DataKeptError
			if !errors.As(err, &kept) || kept.Maybe || kept.Path != filepath.Join(other, "twin") {
				t.Fatalf("Remove(second, with data) = %#v, want a DataKeptError naming %s, not Maybe",
					err, filepath.Join(other, "twin"))
			}

			if listed(e, second) {
				t.Error("the entry is still listed after a kept-data remove")
			}

			if _, err := os.Stat(data); err != nil {
				t.Fatalf("the other torrent's data went with the remove: %v", err)
			}

			if err := e.Remove(first, true); err != nil {
				t.Fatalf("Remove(first, with data) once alone: %v", err)
			}

			requireGone(t, shared)
		})
	}
}

// TestATorrentNamingDataBeingDeletedWaitsForTheDelete is the PR #105 review's
// finding 1: a torrent with no name yet does not stop a live torrent's remove
// with data (DEC-177), so its info dictionary can arrive, naming the same
// data, after the remove decided to delete it and before the delete runs. It
// must download nothing until the delete has finished, or its writes land in
// files the delete then takes away.
func TestATorrentNamingDataBeingDeletedWaitsForTheDelete(t *testing.T) {
	t.Parallel()

	var (
		e          *Engine
		pendingID  atomic.Value // string
		pendingT   atomic.Pointer[torrent.Torrent]
		waited     = make(chan struct{})
		afterOrder = make(chan error, 1)
	)

	infoB := buildInfo("twin", [][]string{{"b.bin"}})

	infoBytes, err := bencode.Marshal(infoB)
	if err != nil {
		t.Fatalf("bencode: %v", err)
	}

	e = newTestEngine(t, func(o *Options) {
		o.Config.MaxActiveDownloads = 4
		o.MetadataTimeout = time.Hour

		o.beforeDelete = func(string) {
			// The remove has decided and recorded its delete: the
			// unnamed torrent's info dictionary arrives now.
			if err := pendingT.Load().SetInfoBytes(infoBytes); err != nil {
				t.Errorf("SetInfoBytes: %v", err)
				return
			}

			select {
			case <-waited:
			case <-time.After(5 * time.Second):
				t.Error("the torrent whose info named the data being deleted did not wait for the delete")
			}
		}

		o.onDeleteWait = func(id string) {
			if id == pendingID.Load() {
				select {
				case <-waited:
				default:
					close(waited)
				}
			}
		}

		o.afterInfo = func(id string) {
			if id != pendingID.Load() {
				return
			}

			// Past the wait: the delete has finished, and nothing of
			// the data it deleted is back.
			_, err := os.Lstat(filepath.Join(e.downloadDir, "twin"))
			if !errors.Is(err, fs.ErrNotExist) {
				afterOrder <- fmt.Errorf("the data is still there as the torrent starts (stat err %w)", err)
				return
			}

			afterOrder <- nil
		}
	})

	ctx := context.Background()

	live, err := e.Add(ctx, engine.AddSource{FilePath: writeTorrentFile(t, buildInfo("twin", [][]string{{"a.bin"}}))})
	if err != nil {
		t.Fatalf("Add(live): %v", err)
	}

	waitForStarted(t, e, live)

	if err := os.MkdirAll(filepath.Join(e.downloadDir, "twin"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(e.downloadDir, "twin", "a.bin"), []byte("the live torrent's data"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	magnet := fmt.Sprintf("magnet:?xt=urn:btih:%s", metainfo.HashBytes(infoBytes).HexString())

	pending, err := e.Add(ctx, engine.AddSource{Magnet: magnet})
	if err != nil {
		t.Fatalf("Add(pending): %v", err)
	}

	pendingID.Store(pending)

	waitUntil(t, "the magnet attached", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()

		if tt := e.torrents[pending].t; tt != nil {
			pendingT.Store(tt)
			return true
		}

		return false
	})

	if err := e.Remove(live, true); err != nil {
		t.Fatalf("Remove(live, with data) = %v, want the data deleted", err)
	}

	if err := wait(t, afterOrder, "the waiting torrent to start"); err != nil {
		t.Fatal(err)
	}

	e.mu.Lock()
	n := len(e.deleting)
	e.mu.Unlock()

	if n != 0 {
		t.Errorf("%d deletes still recorded in flight after the remove returned", n)
	}
}
