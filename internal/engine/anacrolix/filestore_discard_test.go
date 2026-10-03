package anacrolix

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// discardWarning is the text discard logs when it could not remove a path.
const discardWarning = "created as storage closed"

// holdCreate makes s hold the first open of path that creates it, once the
// file exists, until release is closed; entered is closed when it is held.
// Every other open runs straight through.
func holdCreate(s *fileStore, path string) (entered, release chan struct{}) {
	var once sync.Once

	entered, release = make(chan struct{}), make(chan struct{})

	s.openFile = func(name string, flag int, perm os.FileMode) (*os.File, error) {
		f, err := os.OpenFile(name, flag, perm)
		if name == path && flag&os.O_CREATE != 0 && err == nil {
			once.Do(func() {
				close(entered)
				<-release
			})
		}

		return f, err
	}

	return entered, release
}

// logTo points s's logger at a buffer the test can read.
func logTo(s *fileStore) *lockedBuffer {
	log := &lockedBuffer{}
	s.logger = slog.New(slog.NewTextHandler(log, nil))

	return log
}

// requireBytes fails the test unless the file at path holds exactly want.
func requireBytes(t *testing.T, path string, want []byte) {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("%s holds %d bytes not matching the %d written", path, len(got), len(want))
	}
}

// TestFileStoreDiscardLeavesADirectorySomethingElseFilled is review finding A
// of PR #99: a create held as Close begins makes a directory, and while it is
// held something else puts a file in that directory. Discarding the create
// removes the file it made, but must leave the directory and the other file.
func TestFileStoreDiscardLeavesADirectorySomethingElseFilled(t *testing.T) {
	t.Parallel()

	info := storeFixture("filled", 1000, 1000)
	s, _, dir := openStore(t, info)
	ft := torrentOf(t, s)
	second := filepath.Join(dir, "filled", "f01.bin")
	sibling := filepath.Join(dir, "filled", "sibling.txt")

	entered, release := holdCreate(s, second)
	stuckDone := make(chan error, 1)

	go func() { stuckDone <- ft.withFile(1, true, func(*os.File) error { return nil }) }()

	<-entered

	if err := os.WriteFile(sibling, []byte("not the torrent's"), 0o600); err != nil {
		t.Fatalf("write the sibling: %v", err)
	}

	closeDone := startClose(t, ft, "a created file's open was in flight")

	close(release)

	if err := wait(t, closeDone, "Close"); err != nil {
		t.Errorf("Close: %v", err)
	}

	if err := wait(t, stuckDone, "the held write"); !errors.Is(err, errStorageClosed) {
		t.Errorf("held write = %v, want errStorageClosed", err)
	}

	requireGone(t, second)
	requireBytes(t, sibling, []byte("not the torrent's"))
}

// TestFileStoreTwoCreatesOfOneMissingFileBothWrite is review finding B of PR
// #99: two writers find the same file missing and both try to create it. The
// one whose exclusive create loses reopens the file the other made, and both
// writes land in that one file.
func TestFileStoreTwoCreatesOfOneMissingFileBothWrite(t *testing.T) {
	t.Parallel()

	info := storeFixture("both", 1000, 1000)
	s, _, dir := openStore(t, info)
	ft := torrentOf(t, s)
	second := filepath.Join(dir, "both", "f01.bin")

	var (
		held             atomic.Bool
		lostCreate       atomic.Int32
		entered, release = make(chan struct{}), make(chan struct{})
		heldDone         = make(chan error, 1)
	)

	// The first exclusive create is held before it runs, so the other
	// writer's create goes first and this one finds the file there.
	s.openFile = func(name string, flag int, perm os.FileMode) (*os.File, error) {
		if name == second && flag&os.O_EXCL != 0 && held.CompareAndSwap(false, true) {
			close(entered)
			<-release
		}

		f, err := os.OpenFile(name, flag, perm)
		if name == second && flag&os.O_EXCL != 0 && errors.Is(err, fs.ErrExist) {
			lostCreate.Add(1)
		}

		return f, err
	}

	head, tail := pattern(500, 1), pattern(500, 2)

	write := func(b []byte, off int64) func(*os.File) error {
		return func(f *os.File) error {
			_, err := f.WriteAt(b, off)
			return err
		}
	}

	go func() { heldDone <- ft.withFile(1, true, write(head, 0)) }()

	<-entered

	if err := ft.withFile(1, true, write(tail, 500)); err != nil {
		t.Fatalf("the winning create's write: %v", err)
	}

	close(release)

	if err := wait(t, heldDone, "the losing create's write"); err != nil {
		t.Fatalf("the losing create's write: %v", err)
	}

	if n := lostCreate.Load(); n != 1 {
		t.Fatalf("exclusive creates that found the file there = %d, want 1", n)
	}

	if n := s.openHandles(); n != 1 {
		t.Errorf("open handles = %d, want 1", n)
	}

	if err := ft.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	requireBytes(t, second, append(append([]byte(nil), head...), tail...))
}

// TestFileStoreDiscardKeepsAFileAnotherCallerOpened is review finding C of PR
// #99: one call creates a file and is held before it installs its handle;
// another opens a file there, installs its own handle, and maybe writes, all
// before Close begins. When the held create finds the storage closed it must
// not remove a file another caller opened, or a directory holding one.
func TestFileStoreDiscardKeepsAFileAnotherCallerOpened(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		other int  // the file the other caller opens: 1, the created one, or 2 beside it
		write bool // the other caller writes it
		evict bool // and then loses its handle to an eviction before Close
	}{
		{name: "installed and written", other: 1, write: true},
		{name: "installed, nothing written", other: 1},
		{name: "written, handle since evicted", other: 1, write: true, evict: true},
		{name: "another file in its directory", other: 2, write: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Files 1 and 2 share a directory of their own; files 3 on
			// are enough to evict a handle.
			lengths := make([]int64, maxOpenFilesPerTorrent+3)
			for i := range lengths {
				lengths[i] = 100
			}

			info := storeFixture("kept", lengths...)
			info.Files[1].Path = []string{"sub", "f01.bin"}
			info.Files[2].Path = []string{"sub", "f02.bin"}
			s, _, dir := openStore(t, info)
			ft := torrentOf(t, s)
			log := logTo(s)
			created := filepath.Join(dir, "kept", "sub", "f01.bin")
			other := filepath.Join(dir, "kept", "sub", fmt.Sprintf("f%02d.bin", tc.other))

			entered, release := holdCreate(s, created)
			stuckDone := make(chan error, 1)

			go func() { stuckDone <- ft.withFile(1, true, func(*os.File) error { return nil }) }()

			<-entered

			data := pattern(100, 7)
			if !tc.write {
				data = []byte{}
			}

			if err := ft.withFile(tc.other, true, func(f *os.File) error {
				_, err := f.WriteAt(data, 0)
				return err
			}); err != nil {
				t.Fatalf("the other caller's write: %v", err)
			}

			if tc.evict {
				for i := 3; i < len(lengths); i++ {
					if err := ft.withFile(i, true, func(*os.File) error { return nil }); err != nil {
						t.Fatalf("open file %d: %v", i, err)
					}
				}

				ft.mu.RLock()
				_, open := ft.handles[tc.other]
				ft.mu.RUnlock()

				if open {
					t.Fatal("the other caller's handle was not evicted")
				}
			}

			closeDone := startClose(t, ft, "a created file's open was in flight")

			close(release)

			if err := wait(t, closeDone, "Close"); err != nil {
				t.Errorf("Close: %v", err)
			}

			if err := wait(t, stuckDone, "the held create"); !errors.Is(err, errStorageClosed) {
				t.Errorf("held create = %v, want errStorageClosed", err)
			}

			requireBytes(t, other, data)

			if other != created {
				// Nobody else opened the created file, and it is empty.
				requireGone(t, created)
			}

			if strings.Contains(log.String(), discardWarning) {
				t.Errorf("discard tried to remove a kept path:\n%s", log)
			}

			if n := s.openHandles(); n != 0 {
				t.Errorf("open handles after Close = %d, want 0", n)
			}
		})
	}
}
