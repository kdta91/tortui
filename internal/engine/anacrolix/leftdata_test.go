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

// TestLeftDataRefusesAnUnsafeName: a refused entry keeps the name of data on
// disk only when that name is safe at its destination. ".." joined to a
// destination is the destination's parent, which always exists, so a check
// that only looked for something on disk would keep it (T-9127 review).
func TestLeftDataRefusesAnUnsafeName(t *testing.T) {
	t.Parallel()

	dest := filepath.Join(t.TempDir(), "a", "b")
	if err := os.MkdirAll(filepath.Join(dest, "safe"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	unsafe := buildInfo("..", [][]string{{"x.bin"}})
	if got := leftData(&unsafe, dest); got != "" {
		t.Errorf("leftData for a torrent named %q = %q, want none", "..", got)
	}

	safe := buildInfo("safe", [][]string{{"x.bin"}})
	if got := leftData(&safe, dest); got != "safe" {
		t.Errorf("leftData for a safe name with data = %q, want %q", got, "safe")
	}
}

// TestDeleteTorrentDataRefusesAnUnsafeName: the delete itself re-checks the
// name it is handed (AGENT.md §6.11). With ".." the target would be the
// destination's parent, inside the root, and everything in it would go.
func TestDeleteTorrentDataRefusesAnUnsafeName(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)
	root := e.downloadDir
	savePath := filepath.Join(root, "a", "b")
	canary := filepath.Join(root, "a", "canary.txt")

	if err := os.MkdirAll(savePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(canary, []byte("not the torrent's"), 0o600); err != nil {
		t.Fatalf("write canary: %v", err)
	}

	if err := e.deleteTorrentData("an-1", savePath, "..", []string{root}); !errors.Is(err, ErrUnsafePath) {
		t.Errorf("deleteTorrentData with name %q = %v, want ErrUnsafePath", "..", err)
	}

	if _, err := os.Stat(canary); err != nil {
		t.Errorf("the destination's parent lost its contents: %v", err)
	}
}

// TestRemoveWithDataOfARefusedEntrySparesATorrentSharingItsName is review
// note N1 of T-9127: two different torrents with the same name at the same
// destination keep their data in one place. Removing the refused one with
// its data must not delete what the other keeps there, whether the other
// was tracked when the entry was refused or added after.
func TestRemoveWithDataOfARefusedEntrySparesATorrentSharingItsName(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		otherFirst bool // the other torrent is tracked before the refusal
	}{
		{name: "other tracked when refused", otherFirst: true},
		{name: "other added after the refusal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var free atomic.Uint64
			free.Store(1 << 40)

			e := newTestEngine(t, func(o *Options) {
				o.Config.MaxActiveDownloads = 2
				o.MetadataTimeout = time.Hour
				o.freeSpace = func(string) (uint64, error) { return free.Load(), nil }
			})

			ctx := context.Background()
			shared := filepath.Join(e.downloadDir, "twin")
			kept := filepath.Join(shared, "a.bin")

			if err := os.MkdirAll(shared, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}

			if err := os.WriteFile(kept, []byte("the other torrent's data"), 0o600); err != nil {
				t.Fatalf("write the other's data: %v", err)
			}

			addOther := func() string {
				t.Helper()

				path := writeTorrentFile(t, buildInfo("twin", [][]string{{"a.bin"}}))

				id, err := e.Add(ctx, engine.AddSource{FilePath: path})
				if err != nil {
					t.Fatalf("Add(other): %v", err)
				}

				return id
			}

			slot, err := e.Add(ctx, engine.AddSource{Magnet: magnetURI("twin-slot")})
			if err != nil {
				t.Fatalf("Add(slot): %v", err)
			}

			if tc.otherFirst {
				// Live, not refused, when this test's entry is refused: its
				// own free-space check runs after Add returns (T-9177).
				infoChecked(t, e, addOther())
			} else {
				// Hold the second slot so the refused torrent queues.
				if _, err := e.Add(ctx, engine.AddSource{Magnet: magnetURI("twin-slot-2")}); err != nil {
					t.Fatalf("Add(second slot): %v", err)
				}
			}

			// Same name, different files, so a different torrent.
			path := writeTorrentFile(t, buildInfo("twin", [][]string{{"b.bin"}}))

			refused, err := e.Add(ctx, engine.AddSource{FilePath: path})
			if err != nil {
				t.Fatalf("Add(refused): %v", err)
			}

			if st := statusOf(t, e, refused); st.State != engine.StateQueued {
				t.Fatalf("state = %s, want queued", st.State)
			}

			free.Store(0)

			if err := e.Remove(slot, false); err != nil {
				t.Fatalf("Remove(slot): %v", err)
			}

			if st := waitForState(t, e, refused, engine.StateErrored); !errors.Is(st.Err, ErrInsufficientSpace) {
				t.Fatalf("promoted start: err %v, want ErrInsufficientSpace", st.Err)
			}

			free.Store(1 << 40)

			e.mu.Lock()
			leftName := e.torrents[refused].leftName
			e.mu.Unlock()

			if tc.otherFirst && leftName != "" {
				t.Errorf("refused entry kept %q, the other torrent's data, as its own", leftName)
			}

			if !tc.otherFirst {
				if leftName != "twin" {
					t.Fatalf("refused entry kept %q, want %q (nothing else was there yet)", leftName, "twin")
				}

				addOther()
			}

			// It says it kept the data, and where (T-9133).
			err = e.Remove(refused, true)

			var keptErr *engine.DataKeptError
			if !errors.As(err, &keptErr) || keptErr.Path != shared {
				t.Fatalf("Remove(refused, with data) = %v, want a DataKeptError naming %s", err, shared)
			}

			if listed(e, refused) {
				t.Error("the refused entry is still listed after a kept-data remove")
			}

			if _, err := os.Stat(kept); err != nil {
				t.Errorf("the other torrent's data went with the refused entry: %v", err)
			}
		})
	}
}
