package anacrolix

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/engine"
)

// symlinkedDir makes a symlink beside dir that points to it and returns the
// link's path, skipping the test where the account may not make one
// (Windows without the privilege), never faking it.
func symlinkedDir(t *testing.T, dir string) string {
	t.Helper()

	link := dir + "-link"
	if err := os.Symlink(dir, link); err != nil {
		if isUnprivilegedSymlinkError(err) {
			t.Skipf("symlinks need a privilege this account lacks: %v", err)
		}

		t.Fatalf("symlink: %v", err)
	}

	return link
}

// caseVariantDir returns dir with its last element's case swapped, skipping
// the test unless the file system holding dir treats the two as one
// directory (macOS and Windows defaults; not Linux).
func caseVariantDir(t *testing.T, dir string) string {
	t.Helper()

	base := filepath.Base(dir)
	variant := filepath.Join(filepath.Dir(dir), swapCase(base))

	if variant == dir {
		t.Fatalf("%s has no letters to swap", base)
	}

	a, errA := os.Stat(dir)
	b, errB := os.Stat(variant)

	if errA != nil {
		t.Fatalf("stat %s: %v", dir, errA)
	}

	if errB != nil || !os.SameFile(a, b) {
		t.Skipf("the file system holding %s is case-sensitive", dir)
	}

	return variant
}

// swapCase swaps the case of every ASCII letter in s.
func swapCase(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		case r >= 'A' && r <= 'Z':
			return r - 'A' + 'a'
		default:
			return r
		}
	}, s)
}

// TestSameDir is T-9130: one directory by string, through a symlink, or by
// case on a case-insensitive file system is the same; two directories, or one
// that exists and one that does not, are different.
func TestSameDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	a, b := filepath.Join(root, "Alpha"), filepath.Join(root, "beta")

	for _, d := range []string{a, b} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	for _, tc := range []struct {
		name string
		x, y string
		want dirMatch
	}{
		{name: "one path", x: a, y: a, want: dirSame},
		{name: "two directories", x: a, y: b, want: dirDifferent},
		{name: "one missing", x: a, y: filepath.Join(root, "gamma"), want: dirDifferent},
	} {
		if got := sameDir(tc.x, tc.y); got != tc.want {
			t.Errorf("%s: sameDir(%s, %s) = %d, want %d", tc.name, tc.x, tc.y, got, tc.want)
		}
	}

	t.Run("symlink", func(t *testing.T) {
		t.Parallel()

		link := symlinkedDir(t, a)
		if got := sameDir(a, link); got != dirSame {
			t.Errorf("sameDir(dir, its symlink) = %d, want dirSame", got)
		}

		if got := sameDir(filepath.Join(a, "new"), filepath.Join(link, "new")); got != dirSame {
			t.Errorf("sameDir of a path not created yet through dir and its symlink = %d, want dirSame", got)
		}
	})

	t.Run("case variant", func(t *testing.T) {
		t.Parallel()

		if got := sameDir(a, caseVariantDir(t, a)); got != dirSame {
			t.Errorf("sameDir(dir, its case variant) = %d, want dirSame", got)
		}
	})
}

// TestDirMatchUnknownIsEachCallersSafeSide is T-9130: a destination the
// engine could not compare counts as the same directory for the delete guard
// and as another directory for the left-data claim, so canonicalising only
// ever makes either more careful. A destination added after the comparison
// counts as the same for the guard.
func TestDirMatchUnknownIsEachCallersSafeSide(t *testing.T) {
	t.Parallel()

	matches := map[string]dirMatch{"/same": dirSame, "/other": dirDifferent, "/unsure": dirUnknown}

	for path, want := range map[string]bool{"/dest": true, "/same": true, "/other": false, "/unsure": true, "/added-since": true} {
		if got := mayBeSameDir("/dest", path, matches); got != want {
			t.Errorf("delete guard: mayBeSameDir(%s) = %v, want %v", path, got, want)
		}
	}

	for path, want := range map[string]bool{"/dest": true, "/same": true, "/other": false, "/unsure": false, "/added-since": false} {
		if got := isSameDir("/dest", path, matches); got != want {
			t.Errorf("left-data claim: isSameDir(%s) = %v, want %v", path, got, want)
		}
	}

	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"twin", "twin", true}, {"Twin", "tWIN", true}, {"twin", "twins", false}, {"", "", false},
	} {
		if got := mayBeSameName(tc.a, tc.b); got != tc.want {
			t.Errorf("mayBeSameName(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestAddThroughAnAliasOfARefusedEntrysDestinationIsNotLeftData is Backlog
// T-9130: a refused entry left data at a destination; adding the same torrent
// again to that same folder named another way — through a symlink to it, or
// a case variant on a case-insensitive file system — is an add to the same
// destination, not ErrLeftData. The refused entry is untracked and the new
// one starts; an add to a different folder is still refused.
func TestAddThroughAnAliasOfARefusedEntrysDestinationIsNotLeftData(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		alias func(t *testing.T, dir string) string
	}{
		{name: "symlinked destination", alias: symlinkedDir},
		{name: "case-variant destination", alias: caseVariantDir},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var free atomic.Uint64
			free.Store(1 << 40)

			e := newTestEngine(t, func(o *Options) {
				o.MetadataTimeout = time.Hour
				o.freeSpace = func(string) (uint64, error) { return free.Load(), nil }
			})

			ctx := context.Background()

			dest := filepath.Join(e.downloadDir, "Dest")
			if err := os.Mkdir(dest, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}

			elsewhere := filepath.Join(e.downloadDir, "elsewhere")
			alias := tc.alias(t, dest)
			info := buildInfo("left-alias", [][]string{{"a.bin"}})
			left := writePartial(t, dest, "left-alias", "a.bin")

			free.Store(0)

			refused, err := e.Restore(ctx, engine.ResumeData{
				ID: "an-80", Name: "left-alias", SavePath: dest, Metainfo: encodeTorrent(t, info),
			})
			if err != nil {
				t.Fatalf("Restore: %v", err)
			}

			free.Store(1 << 40)

			e.mu.Lock()
			leftName := e.torrents[refused].leftName
			e.mu.Unlock()

			if leftName != "left-alias" {
				t.Fatalf("refused entry left %q, want %q", leftName, "left-alias")
			}

			file := writeTorrentFile(t, info)

			_, err = e.Add(ctx, engine.AddSource{FilePath: file, SavePath: elsewhere})

			var leftErr *engine.LeftDataError
			if !errors.As(err, &leftErr) || leftErr.Path != left {
				t.Fatalf("Add to another folder = %v, want a LeftDataError naming %s", err, left)
			}

			id, err := e.Add(ctx, engine.AddSource{FilePath: file, SavePath: alias})
			if err != nil {
				t.Fatalf("Add through %s = %v, want it accepted as the same destination", tc.name, err)
			}

			if listed(e, refused) {
				t.Error("the refused entry is still tracked after an add to its own folder")
			}

			waitForStarted(t, e, id)
		})
	}
}

// TestRemoveWithDataOfARefusedEntrySparesATwinThroughAnAlias is Backlog
// T-9130 on the delete side: a refused entry's left data is kept when a
// torrent with the same name keeps its data in the same folder named another
// way, a symlink to it or a case variant on a case-insensitive file system.
func TestRemoveWithDataOfARefusedEntrySparesATwinThroughAnAlias(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		alias func(t *testing.T, dir string) string
	}{
		{name: "symlinked destination", alias: symlinkedDir},
		{name: "case-variant destination", alias: caseVariantDir},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var free atomic.Uint64
			free.Store(1 << 40)

			e := newTestEngine(t, func(o *Options) {
				o.MetadataTimeout = time.Hour
				o.freeSpace = func(string) (uint64, error) { return free.Load(), nil }
			})

			ctx := context.Background()

			dest := filepath.Join(e.downloadDir, "Dest")
			if err := os.Mkdir(dest, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}

			alias := tc.alias(t, dest)
			left := writePartial(t, dest, "alias-twin", "a.bin")

			free.Store(0)

			refused, err := e.Restore(ctx, engine.ResumeData{
				ID: "an-81", Name: "alias-twin", SavePath: dest,
				Metainfo: encodeTorrent(t, buildInfo("alias-twin", [][]string{{"a.bin"}})),
			})
			if err != nil {
				t.Fatalf("Restore: %v", err)
			}

			free.Store(1 << 40)

			// Same name, different files: a different torrent, in the same
			// folder named through the alias.
			twin, err := e.Add(ctx, engine.AddSource{
				FilePath: writeTorrentFile(t, buildInfo("alias-twin", [][]string{{"b.bin"}})), SavePath: alias,
			})
			if err != nil {
				t.Fatalf("Add(twin): %v", err)
			}

			waitForStarted(t, e, twin)
			removeKeeps(t, e, refused, left, false)
		})
	}
}

// TestSameDataPath is the PR #105 review's finding 2: discard keeps a file
// another torrent declares when the two paths may be one file. Equal paths
// are; a case-folded name that stats as the same file is; different base
// names are not; one path missing is not; and when a stat fails for another
// reason the answer fails closed, as one file.
func TestSameDataPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "Data.bin")
	other := filepath.Join(dir, "other.bin")

	for _, p := range []string{file, other} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	for _, tc := range []struct {
		name string
		a, b string
		want bool
	}{
		{name: "equal paths", a: file, b: file, want: true},
		{name: "different base names", a: file, b: other, want: false},
		{name: "one missing", a: file, b: filepath.Join(t.TempDir(), "Data.bin"), want: false},
		{name: "same base name, two files", a: file, b: writeFile(t, filepath.Join(t.TempDir(), "Data.bin")), want: false},
	} {
		if got := sameDataPath(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: sameDataPath = %v, want %v", tc.name, got, tc.want)
		}
	}

	if anySameData(file, []string{other}) || !anySameData(file, []string{other, file}) {
		t.Error("anySameData does not report exactly when one of the paths may be the file")
	}

	t.Run("case-folded name, same file", func(t *testing.T) {
		t.Parallel()

		variant := filepath.Join(dir, swapCase("Data.bin"))

		a, errA := os.Stat(file)
		b, errB := os.Stat(variant)

		if errA != nil {
			t.Fatalf("stat: %v", errA)
		}

		if errB != nil || !os.SameFile(a, b) {
			t.Skip("the file system is case-sensitive")
		}

		if !sameDataPath(file, variant) {
			t.Errorf("sameDataPath(%s, %s) = false, want true: one file on this file system", file, variant)
		}
	})

	t.Run("stat error fails closed", func(t *testing.T) {
		t.Parallel()

		locked := filepath.Join(t.TempDir(), "locked")
		hidden := writeFile(t, filepath.Join(locked, "Data.bin"))

		if err := os.Chmod(locked, 0o000); err != nil {
			t.Skipf("this file system will not drop the mode: %v", err)
		}

		t.Cleanup(func() {
			if err := os.Chmod(locked, 0o700); err != nil {
				t.Errorf("restore the mode: %v", err)
			}
		})

		// Root, and Windows, where the mode is only a read-only bit, can
		// still stat it: nothing to test there.
		if _, err := os.Stat(hidden); err == nil || errors.Is(err, os.ErrNotExist) {
			t.Skipf("a mode-0000 parent does not make a stat fail here (err %v)", err)
		}

		if !sameDataPath(file, hidden) {
			t.Error("sameDataPath with a stat that failed = false, want true (fail closed)")
		}
	})
}

// writeFile creates path, with its parent directories, holding one byte, and
// returns it.
func writeFile(t *testing.T, path string) string {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	return path
}
