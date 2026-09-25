package anacrolix

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/kdta91/tortui/internal/engine"
)

// TestAddRootAdmitsAUserChosenDestination is T-074's "every destination the
// user has used or saved joins the known-roots set": a destination outside
// the configured roots is refused until AddRoot admits it, and then Add,
// Restore, and Remove's delete check all accept it (AGENT.md §6.12).
func TestAddRootAdmitsAUserChosenDestination(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)
	chosen := filepath.Join(t.TempDir(), "chosen")

	src := engine.AddSource{Magnet: magnetURI("add-root"), SavePath: chosen}
	if _, err := e.Add(context.Background(), src); !errors.Is(err, ErrOutsideRoots) {
		t.Fatalf("Add before AddRoot = %v, want ErrOutsideRoots", err)
	}

	if err := e.AddRoot(chosen); err != nil {
		t.Fatalf("AddRoot: %v", err)
	}

	id, err := e.Add(context.Background(), src)
	if err != nil {
		t.Fatalf("Add after AddRoot: %v", err)
	}

	if got := statusOf(t, e, id).SavePath; got != filepath.Clean(chosen) {
		t.Errorf("SavePath = %q, want %q", got, filepath.Clean(chosen))
	}

	restored, err := e.Restore(context.Background(), engine.ResumeData{
		ID: "an-restore", Magnet: magnetURI("add-root-restore"), SavePath: chosen,
	})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if st := statusOf(t, e, restored); errors.Is(st.Err, ErrOutsideRoots) {
		t.Fatalf("Restore into an added root: %v", st.Err)
	}

	if err := e.Remove(id, false); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Data written under the added root is deletable with the data: the
	// delete-time re-check (AGENT.md §6.11) sees the grown set.
	file := writeTorrentFile(t, buildInfo("added-root-fixture", [][]string{{"a.bin"}}))

	fid, err := e.Add(context.Background(), engine.AddSource{FilePath: file, SavePath: chosen})
	if err != nil {
		t.Fatalf("Add .torrent under the added root: %v", err)
	}

	target := filepath.Join(chosen, "added-root-fixture")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatalf("seed target dir: %v", err)
	}

	if err := e.Remove(fid, true); err != nil {
		t.Fatalf("Remove with data under an added root: %v", err)
	}

	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("data under the added root survived Remove(deleteData): %v", err)
	}
}

// TestAddRootRefusesUnsafeRoots confirms AddRoot never admits a relative
// path, a NUL-bearing one, or a volume root that would make containment
// vacuous — and that a refused root stays refused for Add.
func TestAddRootRefusesUnsafeRoots(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)
	volumeRoot := filepath.VolumeName(t.TempDir()) + string(filepath.Separator)

	for _, dir := range []string{"", "relative/dir", volumeRoot, filepath.Join(t.TempDir(), "nul\x00")} {
		if err := e.AddRoot(dir); !errors.Is(err, engine.ErrUnsafePath) {
			t.Errorf("AddRoot(%q) = %v, want ErrUnsafePath", dir, err)
		}
	}

	outside := filepath.Join(t.TempDir(), "x")
	if _, err := e.Add(context.Background(), engine.AddSource{Magnet: magnetURI("refused"), SavePath: outside}); !errors.Is(err, ErrOutsideRoots) {
		t.Fatalf("Add = %v, want ErrOutsideRoots", err)
	}
}

// TestAddRootIsIdempotentAndConcurrencySafe runs AddRoot alongside Add under
// -race: roots are read and grown under the engine lock, and a repeated root
// is stored once.
func TestAddRootIsIdempotentAndConcurrencySafe(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)
	dir := t.TempDir()

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)

		go func() {
			defer wg.Done()

			if err := e.AddRoot(dir); err != nil {
				t.Errorf("AddRoot: %v", err)
			}
		}()

		go func() {
			defer wg.Done()

			_, _ = e.Add(context.Background(), engine.AddSource{Magnet: magnetURI(filepath.Join("race", string(rune('a'+i))))})
		}()
	}

	wg.Wait()

	n := 0
	for _, r := range e.knownRoots() {
		if r == filepath.Clean(dir) {
			n++
		}
	}

	if n != 1 {
		t.Errorf("root %s stored %d times, want 1", dir, n)
	}
}

// TestAddRootAfterCloseFails confirms a closed engine refuses to grow.
func TestAddRootAfterCloseFails(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := e.AddRoot(t.TempDir()); !errors.Is(err, ErrClosed) {
		t.Fatalf("AddRoot after Close = %v, want ErrClosed", err)
	}
}
