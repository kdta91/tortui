package anacrolix

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anacrolix/torrent/metainfo"

	"github.com/kdta91/tortui/internal/engine"
)

// updateFixtures regenerates testdata/malicious/*.torrent from
// maliciousFixtures: go test ./internal/engine/anacrolix -run
// TestMaliciousFixturesAreCurrent -update-fixtures.
var updateFixtures = flag.Bool("update-fixtures", false, "rewrite testdata/malicious/*.torrent")

// maliciousFixture is one crafted hostile .torrent: a single, invented
// torrent (it names no real source, AGENT.md §2) whose declared paths
// exercise exactly one category of AGENT.md §6.11's hostile input.
type maliciousFixture struct {
	file  string
	name  string
	paths [][]string
}

// maliciousFixtures covers every category T-034 names: traversal, absolute
// paths (POSIX and Windows forms), NUL bytes, Windows reserved names, trailing
// dots and spaces, and paths exceeding the platform limit (a single component
// over NAME_MAX, and a whole path over every tier-1 platform's PATH_MAX).
var maliciousFixtures = []maliciousFixture{
	{"traversal.torrent", "example-fixture", [][]string{{"..", "..", ".ssh", "authorized_keys"}}},
	{"traversal-name.torrent", "..", [][]string{{"escaped.txt"}}},
	{"absolute-posix.torrent", "example-fixture", [][]string{{"/etc", "passwd"}}},
	{"absolute-windows.torrent", "example-fixture", [][]string{{`C:\Windows`, "System32", "evil.dll"}}},
	{"nul-byte.torrent", "example-fixture", [][]string{{"innocent.txt\x00.exe"}}},
	{"reserved-name.torrent", "example-fixture", [][]string{{"CON"}}},
	{"reserved-name-extension.torrent", "example-fixture", [][]string{{"docs", "com1.txt"}}},
	{"trailing-dot.torrent", "example-fixture", [][]string{{"notes."}}},
	{"trailing-space.torrent", "example-fixture", [][]string{{"notes "}}},
	{"long-component.torrent", "example-fixture", [][]string{{strings.Repeat("a", 256)}}},
	{"long-path.torrent", "example-fixture", [][]string{longPath()}},
}

// longPath is 20 components of 250 bytes: 5000+ bytes in total, past every
// tier-1 platform's limit (PATH_MAX 4096 on Linux, 1024 on macOS, MAX_PATH
// on Windows) while no single component breaks NAME_MAX.
func longPath() []string {
	parts := make([]string, 20)
	for i := range parts {
		parts[i] = strings.Repeat(string(rune('a'+i)), 250)
	}

	return parts
}

func fixturePath(name string) string {
	return filepath.Join("testdata", "malicious", name)
}

// TestMaliciousFixturesAreCurrent pins the committed fixtures to their
// definitions above, so a fixture can never silently drift into something
// harmless.
func TestMaliciousFixturesAreCurrent(t *testing.T) {
	for _, f := range maliciousFixtures {
		want := encodeTorrent(t, buildInfo(f.name, f.paths))

		if *updateFixtures {
			if err := os.MkdirAll(filepath.Dir(fixturePath(f.file)), 0o755); err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(fixturePath(f.file), want, 0o600); err != nil {
				t.Fatal(err)
			}

			continue
		}

		got, err := os.ReadFile(fixturePath(f.file))
		if err != nil {
			t.Fatalf("read fixture %s: %v (regenerate with -update-fixtures)", f.file, err)
		}

		if !bytes.Equal(got, want) {
			t.Errorf("fixture %s is stale (regenerate with -update-fixtures)", f.file)
		}
	}
}

// TestMaliciousTorrentFixturesAreRefused adds every committed hostile
// .torrent, from disk, through the public Add path and asserts it is refused
// with ErrUnsafePath and a reason, that nothing is tracked, and that nothing
// was written anywhere under the destination.
func TestMaliciousTorrentFixturesAreRefused(t *testing.T) {
	e := newTestEngine(t, nil)
	before := dirNames(t, e.downloadDir)

	for _, f := range maliciousFixtures {
		t.Run(f.file, func(t *testing.T) {
			_, err := e.Add(context.Background(), engine.AddSource{FilePath: fixturePath(f.file)})
			if !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("Add(%s) = %v, want ErrUnsafePath", f.file, err)
			}

			if !strings.Contains(err.Error(), "unsafe path:") {
				t.Errorf("Add(%s) error %q carries no reason", f.file, err)
			}

			if n := len(e.List()); n != 0 {
				t.Fatalf("Add(%s) left %d tracked torrents, want 0", f.file, n)
			}
		})
	}

	// The client keeps its own bookkeeping files in the download dir from
	// New onwards; a refused add must not add anything next to them.
	if after := dirNames(t, e.downloadDir); after != before {
		t.Fatalf("download dir changed after only refused adds: before %q, after %q", before, after)
	}
}

// dirNames lists dir's entry names, joined, for a before/after comparison.
func dirNames(t *testing.T, dir string) string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, en := range entries {
		names = append(names, en.Name())
	}

	return strings.Join(names, ",")
}

// TestMaliciousFixturesAreRefusedByTheStorageGate proves the second, hard
// gate independently: safeStorage.OpenTorrent — what the library calls when a
// magnet's info dictionary arrives from a peer, long after Add — refuses
// every fixture before touching the filesystem.
func TestMaliciousFixturesAreRefusedByTheStorageGate(t *testing.T) {
	dest := t.TempDir()
	store := newSafeStorage(dest, discardLogger())

	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close storage: %v", err)
		}
	})

	for _, f := range maliciousFixtures {
		mi, err := metainfo.LoadFromFile(fixturePath(f.file))
		if err != nil {
			t.Fatalf("load %s: %v", f.file, err)
		}

		info, err := mi.UnmarshalInfo()
		if err != nil {
			t.Fatalf("unmarshal %s: %v", f.file, err)
		}

		if _, err := store.OpenTorrent(context.Background(), &info, mi.HashInfoBytes()); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("OpenTorrent(%s) = %v, want ErrUnsafePath", f.file, err)
		}
	}
}
