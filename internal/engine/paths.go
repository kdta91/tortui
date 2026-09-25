package engine

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kdta91/tortui/internal/platform"
)

// ErrUnsafePath reports a path that is refused because it does not resolve
// inside the destination it was supposed to be written to, or because it uses
// a construct that is unsafe on at least one supported platform.
//
// This is the zip-slip class of defect AGENT.md §13 calls the single most
// serious security bug this application could ship: a .torrent is
// attacker-controlled data and routinely declares paths like "../../.ssh/" or
// "C:\Windows\...". Every component check here is deliberately
// platform-independent — a Windows reserved name is refused on Linux too,
// because the download directory may be a network share, a synced folder, or
// simply be read on another machine later. Only the whole-path length limit
// is per-platform, since that is a property of the machine doing the write.
//
// It lives in this package, not in an engine implementation, so every call
// site that opens, reveals, or deletes a torrent's files (AGENT.md §6.11) can
// apply exactly the same rules.
var ErrUnsafePath = errors.New("unsafe path")

// MaxComponentBytes is the longest single path component accepted from a
// torrent: NAME_MAX (255) on every POSIX filesystem tortui targets. NTFS
// counts 255 UTF-16 units instead, and a UTF-8 byte count is never smaller
// than a UTF-16 unit count for the same name, so bytes is the strict bound
// for all three platforms.
const MaxComponentBytes = 255

// windowsReservedNames are the device names Windows refuses to create a file
// with, in any directory and with any extension. A torrent declaring one of
// them produces a file that cannot be written on Windows at all, so it is
// rejected everywhere rather than producing a target that works on two of the
// three tier-1 platforms (AGENT.md §14).
var windowsReservedNames = map[string]struct{}{
	"con": {}, "prn": {}, "aux": {}, "nul": {},
	"com1": {}, "com2": {}, "com3": {}, "com4": {}, "com5": {},
	"com6": {}, "com7": {}, "com8": {}, "com9": {},
	"lpt1": {}, "lpt2": {}, "lpt3": {}, "lpt4": {}, "lpt5": {},
	"lpt6": {}, "lpt7": {}, "lpt8": {}, "lpt9": {},
}

// CleanAbsPath turns p into a cleaned absolute path, rejecting a NUL byte
// outright: a NUL truncates the path at the syscall boundary on POSIX, so a
// path that passes a Go-level containment check can still open a different
// file than the one that was checked.
func CleanAbsPath(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("%w: empty path", ErrUnsafePath)
	}

	if strings.ContainsRune(p, 0) {
		return "", fmt.Errorf("%w: path contains a NUL byte", ErrUnsafePath)
	}

	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("%w: resolve %q: %w", ErrUnsafePath, p, err)
	}

	return filepath.Clean(abs), nil
}

// ContainedIn reports whether candidate resolves inside root. Both are
// expected to be cleaned absolute paths. The root itself counts as contained;
// a sibling whose name merely starts with the root's name (/data-evil against
// /data) does not, which is the mistake a naive strings.HasPrefix makes.
func ContainedIn(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}

	return !filepath.IsAbs(rel)
}

// CheckPathComponent validates one path component declared by a torrent.
func CheckPathComponent(c string) error {
	if c == "" {
		return fmt.Errorf("%w: empty path component", ErrUnsafePath)
	}

	if strings.ContainsRune(c, 0) {
		return fmt.Errorf("%w: path component contains a NUL byte", ErrUnsafePath)
	}

	if c == "." || c == ".." {
		return fmt.Errorf("%w: path component %q traverses out of the torrent", ErrUnsafePath, c)
	}

	// A component is a single name, so neither separator may appear in it.
	// Windows accepts both, which means a POSIX-looking "a/b" and a
	// Windows-looking "a\\b" are each a traversal vector on one platform —
	// and a leading one is an absolute path.
	if strings.ContainsAny(c, `/\`) {
		return fmt.Errorf("%w: path component %q contains a path separator", ErrUnsafePath, c)
	}

	if strings.Contains(c, ":") {
		return fmt.Errorf("%w: path component %q contains a drive or stream separator", ErrUnsafePath, c)
	}

	if trimmed := strings.TrimRight(c, ". "); trimmed != c {
		return fmt.Errorf("%w: path component %q ends in a dot or space", ErrUnsafePath, c)
	}

	if len(c) > MaxComponentBytes {
		return fmt.Errorf("%w: path component %.32q… is %d bytes, over the %d byte limit",
			ErrUnsafePath, c, len(c), MaxComponentBytes)
	}

	base := strings.ToLower(c)
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}

	if _, reserved := windowsReservedNames[base]; reserved {
		return fmt.Errorf("%w: path component %q is a reserved device name", ErrUnsafePath, c)
	}

	return nil
}

// CheckTorrentPath validates one file's declared path components and returns
// the absolute path it would be written to, confirmed to sit inside dest and
// to fit this platform's path length limit.
//
// name is the torrent's own name, which a storage layer uses as a directory
// for a multi-file torrent and as the file name for a single-file one — it is
// as attacker-controlled as the file paths themselves and is checked with
// exactly the same rules. dest must be a cleaned absolute path.
func CheckTorrentPath(dest, name string, components []string) (string, error) {
	if err := CheckPathComponent(name); err != nil {
		return "", fmt.Errorf("torrent name: %w", err)
	}

	parts := make([]string, 0, len(components)+1)
	parts = append(parts, name)

	for _, c := range components {
		if err := CheckPathComponent(c); err != nil {
			return "", err
		}

		parts = append(parts, c)
	}

	full := filepath.Clean(filepath.Join(append([]string{dest}, parts...)...))
	if !ContainedIn(dest, full) {
		return "", fmt.Errorf("%w: %s escapes %s", ErrUnsafePath, strings.Join(parts, "/"), dest)
	}

	if n := platform.PathLength(full); n > platform.MaxPathLength {
		return "", fmt.Errorf("%w: %s would be %d characters long at %s, over this platform's %d limit",
			ErrUnsafePath, strings.Join(parts, "/"), n, dest, platform.MaxPathLength)
	}

	return full, nil
}
