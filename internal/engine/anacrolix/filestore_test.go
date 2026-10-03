package anacrolix

import (
	"bytes"
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"github.com/kdta91/tortui/internal/engine"
)

// storeFixture is a multi-file info whose files are small enough that one
// piece spans several of them.
func storeFixture(name string, lengths ...int64) *metainfo.Info {
	files := make([]metainfo.FileInfo, len(lengths))

	var total int64
	for i, l := range lengths {
		files[i] = metainfo.FileInfo{Length: l, Path: []string{fmt.Sprintf("f%02d.bin", i)}}
		total += l
	}

	pieces := max(1, int((total+testPieceLength-1)/testPieceLength))

	return &metainfo.Info{
		Name:        name,
		PieceLength: testPieceLength,
		Pieces:      make([]byte, sha1.Size*pieces),
		Files:       files,
	}
}

// openStore opens info in a fresh fileStore under a temp directory. The
// store is closed at cleanup.
func openStore(t *testing.T, info *metainfo.Info) (*fileStore, storage.TorrentImpl, string) {
	t.Helper()

	dir := t.TempDir()
	s := newFileStore(dir, storage.NewMapPieceCompletion(), discardLogger())
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("store Close: %v", err)
		}
	})

	ti, err := s.OpenTorrent(context.Background(), info, metainfo.HashBytes([]byte(info.Name)))
	if err != nil {
		t.Fatalf("OpenTorrent: %v", err)
	}

	return s, ti, dir
}

// pattern is deterministic data of length n.
func pattern(n int64, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*13) ^ seed
	}

	return b
}

func TestFileStoreWritesAndReadsAcrossFileBoundaries(t *testing.T) {
	t.Parallel()

	info := storeFixture("span", 1000, 3000, 500)
	_, ti, dir := openStore(t, info)

	p := ti.Piece(info.Piece(0))
	data := pattern(4500, 1)

	if n, err := p.WriteAt(data, 0); err != nil || n != len(data) {
		t.Fatalf("WriteAt = %d, %v; want %d, nil", n, err, len(data))
	}

	got := make([]byte, len(data))
	if n, err := p.ReadAt(got, 0); n != len(data) || (err != nil && !errors.Is(err, io.EOF)) {
		t.Fatalf("ReadAt = %d, %v", n, err)
	}

	if !bytes.Equal(got, data) {
		t.Fatal("data read back differs from data written")
	}

	// Each file holds exactly its own slice of the stream.
	for i, span := range [][2]int{{0, 1000}, {1000, 4000}, {4000, 4500}} {
		onDisk, err := os.ReadFile(filepath.Join(dir, "span", fmt.Sprintf("f%02d.bin", i)))
		if err != nil {
			t.Fatalf("read file %d: %v", i, err)
		}

		if !bytes.Equal(onDisk, data[span[0]:span[1]]) {
			t.Fatalf("file %d holds the wrong bytes", i)
		}
	}
}

func TestFileStoreCloseReleasesEveryHandleSoDataCanBeRewrittenAndDeleted(t *testing.T) {
	t.Parallel()

	info := storeFixture("release", 1000, 3000)
	s, ti, dir := openStore(t, info)

	p := ti.Piece(info.Piece(0))
	if _, err := p.WriteAt(pattern(4000, 2), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}

	if _, err := p.ReadAt(make([]byte, 4000), 0); err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("ReadAt: %v", err)
	}

	if n := s.openHandles(); n != 2 {
		t.Fatalf("open handles before Close = %d, want 2", n)
	}

	if err := ti.Close(); err != nil {
		t.Fatalf("torrent Close: %v", err)
	}

	if n := s.openHandles(); n != 0 {
		t.Fatalf("open handles after Close = %d, want 0", n)
	}

	// On Windows each of these fails while a handle or mapping is open.
	first := filepath.Join(dir, "release", "f00.bin")
	if err := os.WriteFile(first, []byte("rewritten"), 0o600); err != nil {
		t.Fatalf("rewrite after Close: %v", err)
	}

	if err := os.RemoveAll(filepath.Join(dir, "release")); err != nil {
		t.Fatalf("delete after Close: %v", err)
	}

	if _, err := p.ReadAt(make([]byte, 10), 0); !errors.Is(err, errStorageClosed) {
		t.Fatalf("ReadAt after Close = %v, want errStorageClosed", err)
	}

	if _, err := p.WriteAt([]byte("x"), 0); !errors.Is(err, errStorageClosed) {
		t.Fatalf("WriteAt after Close = %v, want errStorageClosed", err)
	}

	if _, err := os.Stat(first); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a write after Close recreated data: stat = %v", err)
	}
}

func TestFileStoreCloseReleasesTorrentsTheClientLeftOpen(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s := newFileStore(dir, storage.NewMapPieceCompletion(), discardLogger())
	info := storeFixture("left-open", 2000)

	ti, err := s.OpenTorrent(context.Background(), info, metainfo.HashBytes([]byte(info.Name)))
	if err != nil {
		t.Fatalf("OpenTorrent: %v", err)
	}

	if _, err := ti.Piece(info.Piece(0)).WriteAt(pattern(2000, 3), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("store Close: %v", err)
	}

	if n := s.openHandles(); n != 0 {
		t.Fatalf("open handles after store Close = %d, want 0", n)
	}

	if err := os.RemoveAll(filepath.Join(dir, "left-open")); err != nil {
		t.Fatalf("delete after store Close: %v", err)
	}
}

func TestFileStoreReadsAMissingFileAsEOFWithoutCreatingIt(t *testing.T) {
	t.Parallel()

	info := storeFixture("missing", 2000)
	_, ti, dir := openStore(t, info)

	n, err := ti.Piece(info.Piece(0)).ReadAt(make([]byte, 100), 0)
	if n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("ReadAt of a missing file = %d, %v; want 0, io.EOF", n, err)
	}

	if _, err := os.Stat(filepath.Join(dir, "missing", "f00.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a read created the missing file: stat = %v", err)
	}
}

func TestFileStoreCompletionDropsAPieceWhoseDataIsGone(t *testing.T) {
	t.Parallel()

	info := storeFixture("gone", 2000)
	_, ti, dir := openStore(t, info)
	p := ti.Piece(info.Piece(0))

	if _, err := p.WriteAt(pattern(2000, 4), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}

	if err := p.MarkComplete(); err != nil {
		t.Fatalf("MarkComplete: %v", err)
	}

	if c := p.Completion(); !c.Ok || !c.Complete || c.Err != nil {
		t.Fatalf("Completion after MarkComplete = %+v, want complete", c)
	}

	// Truncated below the piece's extent: no longer complete.
	if err := ti.Close(); err != nil {
		t.Fatalf("torrent Close: %v", err)
	}

	if err := os.Truncate(filepath.Join(dir, "gone", "f00.bin"), 1000); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	if c := p.Completion(); !c.Ok || c.Complete || c.Err != nil {
		t.Fatalf("Completion of truncated data = %+v, want known incomplete", c)
	}

	// And the record itself was corrected, not just the answer.
	if err := os.Truncate(filepath.Join(dir, "gone", "f00.bin"), 2000); err != nil {
		t.Fatalf("re-extend: %v", err)
	}

	if c := p.Completion(); c.Complete {
		t.Fatal("a piece re-marked incomplete came back complete once the file was long enough again")
	}
}

func TestFileStoreCapsOpenHandlesPerTorrent(t *testing.T) {
	t.Parallel()

	lengths := make([]int64, maxOpenFilesPerTorrent+8)
	for i := range lengths {
		lengths[i] = 100
	}

	info := storeFixture("many", lengths...)
	s, ti, dir := openStore(t, info)

	data := pattern(int64(len(lengths))*100, 5)
	if _, err := ti.Piece(info.Piece(0)).WriteAt(data, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}

	if n := s.openHandles(); n != maxOpenFilesPerTorrent {
		t.Fatalf("open handles = %d, want the cap %d", n, maxOpenFilesPerTorrent)
	}

	// An evicted file reopens on demand: every file still holds its data.
	got := make([]byte, len(data))
	if _, err := ti.Piece(info.Piece(0)).ReadAt(got, 0); err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("ReadAt: %v", err)
	}

	if !bytes.Equal(got, data) {
		t.Fatal("data read back through evicted handles differs")
	}

	if _, err := os.Stat(filepath.Join(dir, "many", "f00.bin")); err != nil {
		t.Fatalf("stat first file: %v", err)
	}
}

func TestFileStoreCreatesZeroLengthFilesAndRefusesEscapes(t *testing.T) {
	t.Parallel()

	info := storeFixture("empty", 0, 100)
	_, _, dir := openStore(t, info)

	if fi, err := os.Stat(filepath.Join(dir, "empty", "f00.bin")); err != nil || fi.Size() != 0 {
		t.Fatalf("zero-length file: %v, %v; want an empty file", fi, err)
	}

	s := newFileStore(dir, storage.NewMapPieceCompletion(), discardLogger())
	defer func() { _ = s.Close() }()

	escape := storeFixture("escape", 100)
	escape.Files[0].Path = []string{"..", "..", "outside.bin"}

	if _, err := s.OpenTorrent(context.Background(), escape, metainfo.HashBytes([]byte("escape"))); err == nil {
		t.Fatal("OpenTorrent accepted a file path climbing out of the store's directory")
	}
}

// TestCompletedFileIsRemovableWithDataWhileRunningAndAfterClose is T-097's
// every-OS proof. Open handle counts are asserted, not just the deletes,
// because only Windows refuses to delete an open file: elsewhere the delete
// alone would pass with every handle leaked.
func TestCompletedFileIsRemovableWithDataWhileRunningAndAfterClose(t *testing.T) {
	t.Parallel()

	storeOf := func(e *Engine) *fileStore {
		e.mu.Lock()
		defer e.mu.Unlock()

		return e.storages[e.downloadDir].inner
	}

	complete := func(e *Engine, name string) string {
		id, err := e.Add(context.Background(), engine.AddSource{FilePath: completeTorrent(t, e.downloadDir, name, 3*testPieceLength)})
		if err != nil {
			t.Fatalf("Add: %v", err)
		}

		waitForStarted(t, e, id)
		verify(t, e, id)
		waitUntil(t, name+" complete", func() bool { return statusOf(t, e, id).Progress == 1 })

		return id
	}

	// While running: remove with data releases the handle, then deletes.
	e := newTestEngine(t, nil)
	id := complete(e, "running.bin")

	s := storeOf(e)
	if s.openHandles() == 0 {
		t.Fatal("verifying the data opened no handle; the test would prove nothing")
	}

	if err := e.Remove(id, true); err != nil {
		t.Fatalf("Remove with data: %v", err)
	}

	if n := s.openHandles(); n != 0 {
		t.Fatalf("open handles after Remove = %d, want 0", n)
	}

	if _, err := os.Stat(filepath.Join(e.downloadDir, "running.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed file survived Remove with data: stat = %v", err)
	}

	// After Close: nothing is held, so the file can be rewritten and
	// deleted — what the next session's remove-with-data does.
	e2 := newTestEngine(t, nil)
	complete(e2, "closed.bin")
	s2 := storeOf(e2)

	if err := e2.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if n := s2.openHandles(); n != 0 {
		t.Fatalf("open handles after Close = %d, want 0", n)
	}

	path := filepath.Join(e2.downloadDir, "closed.bin")
	if err := os.WriteFile(path, []byte("rewritten"), 0o600); err != nil {
		t.Fatalf("rewrite completed file after Close: %v", err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("delete completed file after Close: %v", err)
	}
}

// torrentOf returns the one fileTorrent s has open.
func torrentOf(t *testing.T, s *fileStore) *fileTorrent {
	t.Helper()

	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.torrents) != 1 {
		t.Fatalf("store has %d torrents open, want 1", len(s.torrents))
	}

	for ft := range s.torrents {
		return ft
	}

	return nil
}

// TestFileStoreOpeningOneFileNeverStallsAnother is T-9003: the first use of
// a torrent's file — its open and the I/O straight after it — runs without
// the torrent's exclusive lock, so I/O on another file of the same torrent,
// whose handle is already open, goes ahead while it is stuck. Both a stuck
// open and stuck I/O are held until the other file's write has finished;
// before T-9003 that write waited for them.
func TestFileStoreOpeningOneFileNeverStallsAnother(t *testing.T) {
	t.Parallel()

	for _, stuck := range []string{"open", "io"} {
		t.Run(stuck, func(t *testing.T) {
			t.Parallel()

			info := storeFixture("stall-"+stuck, 1000, 1000)
			s, _, dir := openStore(t, info)
			ft := torrentOf(t, s)

			// File 0's handle is open before anything is stuck.
			if err := ft.withFile(0, true, func(*os.File) error { return nil }); err != nil {
				t.Fatalf("open file 0: %v", err)
			}

			second := filepath.Join(dir, "stall-"+stuck, "f01.bin")
			entered, release := make(chan struct{}), make(chan struct{})

			if stuck == "open" {
				var once sync.Once

				s.openFile = func(name string, flag int, perm os.FileMode) (*os.File, error) {
					if name == second {
						once.Do(func() {
							close(entered)
							<-release
						})
					}

					return os.OpenFile(name, flag, perm)
				}
			}

			stuckDone := make(chan error, 1)

			go func() {
				stuckDone <- ft.withFile(1, true, func(*os.File) error {
					if stuck == "io" {
						close(entered)
						<-release
					}

					return nil
				})
			}()

			<-entered

			otherDone := make(chan error, 1)

			go func() {
				otherDone <- ft.withFile(0, true, func(f *os.File) error {
					_, err := f.WriteAt([]byte("x"), 0)
					return err
				})
			}()

			stalled := false

			select {
			case err := <-otherDone:
				if err != nil {
					t.Errorf("write to file 0: %v", err)
				}
			case <-time.After(5 * time.Second):
				stalled = true
				t.Errorf("a write to file 0 waited on file 1's %s", stuck)
			}

			close(release)

			if err := <-stuckDone; err != nil {
				t.Errorf("file 1: %v", err)
			}

			if stalled {
				if err := <-otherDone; err != nil {
					t.Errorf("write to file 0: %v", err)
				}
			}
		})
	}
}

// TestFileStoreConcurrentIOAcrossEvictionsKeepsEveryByte drives many
// goroutines writing and reading more files than the handle cap, so opens,
// evictions and reuse interleave; run under -race it checks the handle table
// is only touched under its lock, and every file still holds its bytes.
func TestFileStoreConcurrentIOAcrossEvictionsKeepsEveryByte(t *testing.T) {
	t.Parallel()

	const size = 512

	lengths := make([]int64, maxOpenFilesPerTorrent*2)
	for i := range lengths {
		lengths[i] = size
	}

	info := storeFixture("churn", lengths...)
	s, _, _ := openStore(t, info)
	ft := torrentOf(t, s)

	var wg sync.WaitGroup

	errs := make(chan error, len(lengths))

	for i := range lengths {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			want := pattern(size, byte(i))

			for round := range 20 {
				if err := ft.withFile(i, true, func(f *os.File) error {
					_, err := f.WriteAt(want, 0)
					return err
				}); err != nil {
					errs <- fmt.Errorf("file %d round %d write: %w", i, round, err)
					return
				}

				got := make([]byte, size)
				if err := ft.withFile(i, false, func(f *os.File) error {
					_, err := f.ReadAt(got, 0)
					return err
				}); err != nil {
					errs <- fmt.Errorf("file %d round %d read: %w", i, round, err)
					return
				}

				if !bytes.Equal(got, want) {
					errs <- fmt.Errorf("file %d round %d read back different bytes", i, round)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}

	if n := s.openHandles(); n > maxOpenFilesPerTorrent {
		t.Errorf("open handles = %d, over the cap %d", n, maxOpenFilesPerTorrent)
	}
}
