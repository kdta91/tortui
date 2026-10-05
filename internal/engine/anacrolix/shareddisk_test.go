package anacrolix

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/engine"
)

// sharedText is what a free-space refusal says when other downloads on the
// same disk need some of its free space: one other download with 3 KiB left.
const sharedText = "sharing the disk with 1 other download that still needs 3.0 KiB"

// threeKiB is a .torrent of name whose two files are 3 KiB together.
func threeKiB(t *testing.T, name string) string {
	t.Helper()

	return writeTorrentFile(t, buildInfo(name, [][]string{{"a.bin"}, {"b.bin"}}))
}

// TestAddRefusesDownloadsThatTogetherOvercommitADisk is Backlog T-947: two
// downloads of 3 KiB each fit a disk with 5 KiB free and a 1 KiB margin
// alone, but not together. The second add is refused, saying the need is
// shared with the other download, when both write to one filesystem — one
// destination, or two on one filesystem. On two filesystems, or with the
// first download paused, the second fits. Where the filesystem identity
// cannot be read, the need is summed per destination.
func TestAddRefusesDownloadsThatTogetherOvercommitADisk(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		second   string // the second download's destination under the download dir; "" for the dir itself
		twoDisks bool   // each destination reports a filesystem of its own
		noID     bool   // no filesystem identity can be read
		pause    bool   // the first download is paused first
		refused  bool
	}{
		{name: "one destination", refused: true},
		{name: "two destinations on one filesystem", second: "other", refused: true},
		{name: "two filesystems", second: "other", twoDisks: true},
		{name: "first download paused", pause: true},
		{name: "no filesystem identity, one destination", noID: true, refused: true},
		{name: "no filesystem identity, two destinations", second: "other", noID: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			e := newTestEngine(t, func(o *Options) {
				o.Config.MinFreeSpace = "1KB"
				o.MetadataTimeout = time.Hour
				o.SpaceCheckInterval = time.Hour
				o.freeSpace = func(string) (uint64, error) { return 5 << 10, nil }

				switch {
				case tc.twoDisks:
					o.filesystemID = func(dest string) (string, error) { return "fs:" + dest, nil }
				case tc.noID:
					o.filesystemID = func(string) (string, error) { return "", errors.New("no identity here") }
				}
			})

			ctx := context.Background()

			first, err := e.Add(ctx, engine.AddSource{FilePath: threeKiB(t, "first")})
			if err != nil {
				t.Fatalf("Add(first) = %v, want it to fit alone", err)
			}

			waitForState(t, e, first, engine.StateDownloading)

			if tc.pause {
				if err := e.Pause(first); err != nil {
					t.Fatalf("Pause: %v", err)
				}
			}

			dest := e.downloadDir
			if tc.second != "" {
				dest = filepath.Join(dest, tc.second)
			}

			_, err = e.Add(ctx, engine.AddSource{FilePath: threeKiB(t, "second"), SavePath: dest})

			if !tc.refused {
				if err != nil {
					t.Fatalf("Add(second) = %v, want it to fit", err)
				}

				return
			}

			if !errors.Is(err, ErrInsufficientSpace) {
				t.Fatalf("Add(second) = %v, want ErrInsufficientSpace", err)
			}

			// 3 KiB + 3 KiB + 1 KiB margin, 5 KiB free: 2 KiB short.
			for _, want := range []string{"needs 3.0 KiB", sharedText, "1.0 KiB min_free_space margin", "5.0 KiB free", "2.0 KiB short"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Add(second) error %q does not say %q", err, want)
				}
			}

			if n := len(e.List()); n != 1 {
				t.Errorf("a refused add left %d tracked torrents, want 1", n)
			}
		})
	}
}

// TestRecheckPausesTheNewestDownloadsADiskCannotHoldTogether is Backlog T-947
// for the periodic re-check: two downloads of 3 KiB each on one disk, whose
// free space drops to 5 KiB with a 1 KiB margin, fit alone but not together.
// The newer one is paused, saying the need is shared; the older one keeps
// going. When the disk drops to 3 KiB the older one is paused too, and as the
// paused one needs nothing now, its reason names no other download.
func TestRecheckPausesTheNewestDownloadsADiskCannotHoldTogether(t *testing.T) {
	t.Parallel()

	var free atomic.Uint64
	free.Store(1 << 40)

	e := newTestEngine(t, func(o *Options) {
		o.Config.MinFreeSpace = "1KB"
		o.MetadataTimeout = time.Hour
		o.SpaceCheckInterval = time.Millisecond
		o.freeSpace = func(string) (uint64, error) { return free.Load(), nil }
	})

	ctx := context.Background()

	older, err := e.Add(ctx, engine.AddSource{FilePath: threeKiB(t, "older")})
	if err != nil {
		t.Fatalf("Add(older): %v", err)
	}

	newer, err := e.Add(ctx, engine.AddSource{
		FilePath: threeKiB(t, "newer"), SavePath: filepath.Join(e.downloadDir, "other"),
	})
	if err != nil {
		t.Fatalf("Add(newer): %v", err)
	}

	waitForState(t, e, older, engine.StateDownloading)
	waitForState(t, e, newer, engine.StateDownloading)

	free.Store(5 << 10)

	st := waitForState(t, e, newer, engine.StateErrored)
	if !errors.Is(st.Err, ErrInsufficientSpace) || !strings.Contains(st.Err.Error(), sharedText) {
		t.Fatalf("newer download's reason = %v, want an ErrInsufficientSpace saying %q", st.Err, sharedText)
	}

	if st := statusOf(t, e, older); st.State != engine.StateDownloading || st.Err != nil {
		t.Fatalf("older download: state %s err %v, want still downloading", st.State, st.Err)
	}

	free.Store(3 << 10)

	st = waitForState(t, e, older, engine.StateErrored)
	if !errors.Is(st.Err, ErrInsufficientSpace) || strings.Contains(st.Err.Error(), "sharing the disk") {
		t.Fatalf("older download's reason = %v, want an ErrInsufficientSpace naming no other download", st.Err)
	}
}

// TestInsufficientSpaceErrorSaysTheNeedIsShared pins the refusal's wording:
// alone it reads as before; with other downloads on the same disk it says how
// many and what they still need, in the singular and the plural.
func TestInsufficientSpaceErrorSaysTheNeedIsShared(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		others sharedNeed
		want   string
	}{
		{want: "not enough free space at /d: needs 1.0 KiB plus the 1.0 KiB min_free_space margin, 1.0 KiB free — 1.0 KiB short"},
		{
			others: sharedNeed{downloads: 1, bytes: 2048},
			want: "not enough free space at /d: needs 1.0 KiB, sharing the disk with 1 other download that still needs " +
				"2.0 KiB, plus the 1.0 KiB min_free_space margin, 1.0 KiB free — 1.0 KiB short",
		},
		{
			others: sharedNeed{downloads: 2, bytes: 4096},
			want: "not enough free space at /d: needs 1.0 KiB, sharing the disk with 2 other downloads that still need " +
				"4.0 KiB, plus the 1.0 KiB min_free_space margin, 1.0 KiB free — 1.0 KiB short",
		},
	} {
		if got := insufficientSpaceError("/d", 1024, 1024, tc.others, 1024, 1024).Error(); got != tc.want {
			t.Errorf("insufficientSpaceError =\n  %q\nwant\n  %q", got, tc.want)
		}
	}
}
