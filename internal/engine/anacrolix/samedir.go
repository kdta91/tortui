package anacrolix

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// dirMatch is how sure the engine is that two destinations are one
// directory (T-9130). Each caller reads dirUnknown on its own safe side: the
// delete guard as the same directory, so it keeps data it is unsure of, and
// the left-data claim as another directory, so it refuses an add it is
// unsure of. Canonicalising only ever moves a guard to its conservative side.
type dirMatch int

const (
	// dirDifferent: not one directory, as far as the filesystem says.
	dirDifferent dirMatch = iota
	// dirSame: the same string, the same path once symlinks are resolved, or
	// the same directory by file identity (a case-insensitive file system, a
	// symlinked alias).
	dirSame
	// dirUnknown: neither could be shown, e.g. a stat failed.
	dirUnknown
)

// sameDir compares destinations a and b, which are cleaned absolute paths.
// It does I/O, so it never runs under Engine.mu.
func sameDir(a, b string) dirMatch {
	if a == b {
		return dirSame
	}

	ra, errA := resolveSymlinks(a)
	rb, errB := resolveSymlinks(b)

	if errA == nil && errB == nil && ra == rb {
		return dirSame
	}

	fa, errA := os.Stat(a)
	fb, errB := os.Stat(b)

	switch {
	case errA == nil && errB == nil:
		if os.SameFile(fa, fb) {
			return dirSame
		}

		return dirDifferent
	case errA == nil && errors.Is(errB, fs.ErrNotExist), errB == nil && errors.Is(errA, fs.ErrNotExist):
		// One is there and the other is not: not one directory now.
		return dirDifferent
	default:
		return dirUnknown
	}
}

// dirMatches compares dest with each of paths, once per distinct path.
func dirMatches(dest string, paths []string) map[string]dirMatch {
	out := make(map[string]dirMatch, len(paths))

	for _, p := range paths {
		if _, ok := out[p]; !ok {
			out[p] = sameDir(dest, p)
		}
	}

	return out
}

// mayBeSameDir reports, for the delete guard, whether savePath may be the
// directory the matches were taken against: the same string, dirSame, or
// dirUnknown, which includes a path added since the matches were taken.
func mayBeSameDir(dest, savePath string, matches map[string]dirMatch) bool {
	if savePath == dest {
		return true
	}

	m, ok := matches[savePath]

	return !ok || m != dirDifferent
}

// isSameDir reports, for the left-data claim, whether savePath is shown to be
// the directory the matches were taken against: the same string or dirSame.
func isSameDir(dest, savePath string, matches map[string]dirMatch) bool {
	return savePath == dest || matches[savePath] == dirSame
}

// mayBeSameName reports whether two data names may name one entry in one
// directory. Names that differ only in case do on a case-insensitive file
// system, so the delete guard treats them as one wherever it runs: that only
// ever keeps more.
func mayBeSameName(a, b string) bool {
	return a != "" && b != "" && (a == b || strings.EqualFold(a, b))
}

// destMatches snapshots, under Engine.mu, the destination of torrent id (or
// dest itself when id is "") and of every tracked torrent, then compares them
// with no lock held. It returns the destination compared against ("" when id
// is not tracked) and the matches.
func (e *Engine) destMatches(id, dest string) (string, map[string]dirMatch) {
	e.mu.Lock()

	if id != "" {
		tr, ok := e.torrents[id]
		if !ok {
			e.mu.Unlock()
			return "", nil
		}

		dest = tr.savePath
	}

	paths := make([]string, 0, len(e.torrents))
	for _, tr := range e.torrents {
		if tr.savePath != dest {
			paths = append(paths, tr.savePath)
		}
	}
	e.mu.Unlock()

	return dest, dirMatches(dest, paths)
}

// sameDataPath reports whether a and b, two data file paths, may be one file:
// the same string, or names that match ignoring case and stat as one file, or
// that cannot both be checked. It does I/O.
func sameDataPath(a, b string) bool {
	if a == b {
		return true
	}

	if !strings.EqualFold(filepath.Base(a), filepath.Base(b)) {
		return false
	}

	fa, errA := os.Stat(a)
	fb, errB := os.Stat(b)

	switch {
	case errA == nil && errB == nil:
		return os.SameFile(fa, fb)
	case errors.Is(errA, fs.ErrNotExist) || errors.Is(errB, fs.ErrNotExist):
		return false
	default:
		return true
	}
}
