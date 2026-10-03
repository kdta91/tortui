package anacrolix

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// closeOutlivesWindow is how long a test gives Close to return early — a
// Close that does not wait for work in flight returns within microseconds.
// A correct Close never returns in it, so the window only costs time.
const closeOutlivesWindow = 200 * time.Millisecond

// startClose runs ft.Close on its own goroutine once ft is closing, and
// fails the test if Close returns within closeOutlivesWindow: it must wait
// for what is in flight, which the caller is still holding.
func startClose(t *testing.T, ft *fileTorrent, holding string) <-chan error {
	t.Helper()

	done := make(chan error, 1)

	go func() { done <- ft.Close() }()

	waitUntil(t, "storage Close to begin", ft.closed.Load)

	select {
	case err := <-done:
		t.Errorf("Close returned while %s", holding)
		done <- err // for the caller's wait
	case <-time.After(closeOutlivesWindow):
	}

	return done
}

// wait returns what ch delivers, failing the test after five seconds.
func wait[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()

	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)

		var zero T

		return zero
	}
}

// requireGone fails the test unless nothing exists at path.
func requireGone(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s exists after storage Close (stat err %v)", path, err)
	}
}

// requireClosed fails the test unless f was already closed.
func requireClosed(t *testing.T, f *os.File, what string) {
	t.Helper()

	if f == nil {
		t.Fatalf("%s: no file was captured", what)
	}

	if err := f.Close(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("%s was still open (a second Close returned %v)", what, err)
	}
}

// TestFileStoreNothingIsCreatedOnceCloseBegins is the T-9003 review's
// finding: the library writes with its own lock released and closes a
// dropped torrent's storage while those writes run, and Remove deletes the
// data right after. A write whose open is in flight as Close begins must
// neither recreate the deleted file nor leave a handle open past Close.
func TestFileStoreNothingIsCreatedOnceCloseBegins(t *testing.T) {
	t.Parallel()

	// The open is held before anything is created; meanwhile Close begins
	// and the data is deleted. Nothing may be created afterwards.
	t.Run("open held before create", func(t *testing.T) {
		t.Parallel()

		info := storeFixture("probe", 1000, 1000)
		s, _, dir := openStore(t, info)
		ft := torrentOf(t, s)
		second := filepath.Join(dir, "probe", "f01.bin")

		var (
			once                  sync.Once
			mu                    sync.Mutex
			createdAfterRelease   bool
			entered, release      = make(chan struct{}), make(chan struct{})
			released              = false
			stuckDone             = make(chan error, 1)
			markReleased          = func() { mu.Lock(); released = true; mu.Unlock() }
			createdWhileReleasing = func(flag int) {
				mu.Lock()
				if released && flag&os.O_CREATE != 0 {
					createdAfterRelease = true
				}
				mu.Unlock()
			}
		)

		s.openFile = func(name string, flag int, perm os.FileMode) (*os.File, error) {
			if name == second {
				once.Do(func() {
					close(entered)
					<-release
				})
				createdWhileReleasing(flag)
			}

			return os.OpenFile(name, flag, perm)
		}

		go func() { stuckDone <- ft.withFile(1, true, func(*os.File) error { return nil }) }()

		<-entered

		closeDone := startClose(t, ft, "an open was in flight")

		if err := os.RemoveAll(filepath.Join(dir, "probe")); err != nil {
			t.Fatalf("delete the data: %v", err)
		}

		markReleased()
		close(release)

		if err := wait(t, closeDone, "Close"); err != nil {
			t.Errorf("Close: %v", err)
		}

		if err := wait(t, stuckDone, "the held write"); !errors.Is(err, errStorageClosed) {
			t.Errorf("held write = %v, want errStorageClosed", err)
		}

		requireGone(t, second)
		requireGone(t, filepath.Join(dir, "probe"))

		if createdAfterRelease {
			t.Error("a file was opened for creation after Close began")
		}

		if n := s.openHandles(); n != 0 {
			t.Errorf("open handles after Close = %d, want 0", n)
		}
	})

	// The file is created just as Close begins. Close waits for the open,
	// which closes its handle and removes the file and the directory it
	// made, so nothing is left even without a delete.
	t.Run("file created as Close begins", func(t *testing.T) {
		t.Parallel()

		info := storeFixture("made", 1000, 1000)
		s, _, dir := openStore(t, info)
		ft := torrentOf(t, s)
		second := filepath.Join(dir, "made", "f01.bin")

		var (
			once             sync.Once
			created          *os.File
			entered, release = make(chan struct{}), make(chan struct{})
			stuckDone        = make(chan error, 1)
		)

		s.openFile = func(name string, flag int, perm os.FileMode) (*os.File, error) {
			f, err := os.OpenFile(name, flag, perm)
			if name == second && flag&os.O_CREATE != 0 && err == nil {
				once.Do(func() {
					created = f
					close(entered)
					<-release
				})
			}

			return f, err
		}

		go func() { stuckDone <- ft.withFile(1, true, func(*os.File) error { return nil }) }()

		<-entered

		closeDone := startClose(t, ft, "a created file's open was in flight")

		close(release)

		if err := wait(t, closeDone, "Close"); err != nil {
			t.Errorf("Close: %v", err)
		}

		if err := wait(t, stuckDone, "the held write"); !errors.Is(err, errStorageClosed) {
			t.Errorf("held write = %v, want errStorageClosed", err)
		}

		requireGone(t, second)
		requireGone(t, filepath.Join(dir, "made"))
		requireClosed(t, created, "the handle opened as Close began")
	})
}

// TestFileStoreCloseWaitsForAnEvictedHandle: a handle evicted to make room
// is closed after the table's lock is released, and Close must not return
// while that close is still pending — once Close returns, every data file
// the torrent opened is closed (T-097's Windows remove-with-data).
func TestFileStoreCloseWaitsForAnEvictedHandle(t *testing.T) {
	t.Parallel()

	lengths := make([]int64, maxOpenFilesPerTorrent+1)
	for i := range lengths {
		lengths[i] = 100
	}

	info := storeFixture("evict", lengths...)
	s, _, dir := openStore(t, info)
	ft := torrentOf(t, s)

	noop := func(*os.File) error { return nil }

	for i := range maxOpenFilesPerTorrent {
		if err := ft.withFile(i, true, noop); err != nil {
			t.Fatalf("open file %d: %v", i, err)
		}
	}

	ft.mu.RLock()
	victim := ft.handles[0].f
	ft.mu.RUnlock()

	var (
		once             sync.Once
		entered, release = make(chan struct{}), make(chan struct{})
		openDone         = make(chan error, 1)
	)

	s.closeFile = func(f *os.File) error {
		if f == victim {
			once.Do(func() {
				close(entered)
				<-release
			})
		}

		return f.Close()
	}

	go func() { openDone <- ft.withFile(maxOpenFilesPerTorrent, true, noop) }()

	<-entered

	closeDone := startClose(t, ft, "an evicted handle was still open")

	close(release)

	if err := wait(t, closeDone, "Close"); err != nil {
		t.Errorf("Close: %v", err)
	}

	// Close began before the write got to its I/O, so the write loses.
	if err := wait(t, openDone, "the evicting write"); !errors.Is(err, errStorageClosed) {
		t.Errorf("evicting write = %v, want errStorageClosed", err)
	}

	requireClosed(t, victim, "the evicted handle")

	if n := s.openHandles(); n != 0 {
		t.Errorf("open handles after Close = %d, want 0", n)
	}

	if _, err := os.Stat(filepath.Join(dir, "evict", "f00.bin")); err != nil {
		t.Errorf("stat the evicted file: %v", err)
	}
}

// TestFileStoreKeepsTheFirstOfTwoConcurrentOpens: two calls open the same
// file at once; the one that installs its handle second keeps the first's
// and closes its own.
func TestFileStoreKeepsTheFirstOfTwoConcurrentOpens(t *testing.T) {
	t.Parallel()

	info := storeFixture("twice", 1000, 1000)
	s, _, dir := openStore(t, info)
	ft := torrentOf(t, s)
	second := filepath.Join(dir, "twice", "f01.bin")

	if err := os.MkdirAll(filepath.Dir(second), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(second, make([]byte, 1000), 0o600); err != nil {
		t.Fatalf("write file 1: %v", err)
	}

	var (
		first            atomic.Bool
		loser            *os.File
		entered, release = make(chan struct{}), make(chan struct{})
		loserDone        = make(chan error, 1)
		loserUsed        = make(chan *os.File, 1)
	)

	s.openFile = func(name string, flag int, perm os.FileMode) (*os.File, error) {
		f, err := os.OpenFile(name, flag, perm)
		// Only the first open is held; the second must not wait on it.
		if name == second && err == nil && first.CompareAndSwap(false, true) {
			loser = f
			close(entered)
			<-release
		}

		return f, err
	}

	go func() {
		loserDone <- ft.withFile(1, true, func(f *os.File) error {
			loserUsed <- f
			return nil
		})
	}()

	<-entered

	var winner *os.File

	if err := ft.withFile(1, true, func(f *os.File) error {
		winner = f
		return nil
	}); err != nil {
		t.Fatalf("winning open: %v", err)
	}

	close(release)

	if err := wait(t, loserDone, "the losing open"); err != nil {
		t.Fatalf("losing open: %v", err)
	}

	if got := wait(t, loserUsed, "the losing open's I/O"); got != winner {
		t.Error("the losing open did its I/O on its own handle, not the one installed first")
	}

	requireClosed(t, loser, "the losing open's handle")

	if n := s.openHandles(); n != 1 {
		t.Errorf("open handles = %d, want 1", n)
	}
}
