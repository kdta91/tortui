package anacrolix

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrUnsafePath reports a path that is refused because it does not resolve
// inside the destination it was supposed to be written to, or because it uses
// a construct that is unsafe on at least one supported platform.
//
// This is the zip-slip class of defect AGENT.md §13 calls the single most
// serious security bug this application could ship: a .torrent is
// attacker-controlled data and routinely declares paths like "../../.ssh/" or
// "C:\Windows\...". Every check here is deliberately platform-independent —
// a Windows reserved name is refused on Linux too, because the download
// directory may be a network share, a synced folder, or simply be read on
// another machine later.
var ErrUnsafePath = errors.New("anacrolix: unsafe path")

// ErrOutsideRoots reports a destination that does not sit inside any of the
// engine's known destination roots (AGENT.md §6.12).
var ErrOutsideRoots = errors.New("anacrolix: destination is outside every known destination root")

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

// cleanAbsPath turns p into a cleaned absolute path, rejecting a NUL byte
// outright: a NUL truncates the path at the syscall boundary on POSIX, so a
// path that passes a Go-level containment check can still open a different
// file than the one that was checked.
func cleanAbsPath(p string) (string, error) {
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

// containedIn reports whether candidate resolves inside root. Both are
// expected to be cleaned absolute paths. The root itself counts as contained;
// a sibling whose name merely starts with the root's name (/data-evil against
// /data) does not, which is the mistake a naive strings.HasPrefix makes.
func containedIn(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}

	return !filepath.IsAbs(rel)
}

// resolveDestination resolves the per-torrent destination for an AddSource:
// savePath when it is set, otherwise fallback (the configured download
// directory). The result is a cleaned absolute path that has been confirmed
// to sit inside one of roots.
//
// engine.AddSource documents SavePath as already validated by the caller that
// constructed it. It is re-checked here anyway: AGENT.md §6.11 is explicit
// that a check at one call site is not a check at another, and this is the
// call site that hands a directory to code that will create files in it.
func resolveDestination(savePath, fallback string, roots []string) (string, error) {
	chosen := savePath
	if strings.TrimSpace(chosen) == "" {
		chosen = fallback
	}

	if strings.TrimSpace(chosen) == "" {
		return "", errors.New("anacrolix: no save path and no configured download directory")
	}

	abs, err := cleanAbsPath(chosen)
	if err != nil {
		return "", err
	}

	for _, root := range roots {
		cleanRoot, err := cleanAbsPath(root)
		if err != nil {
			continue
		}

		if containedIn(cleanRoot, abs) {
			return abs, nil
		}
	}

	return "", fmt.Errorf("%w: %s", ErrOutsideRoots, abs)
}

// checkComponent validates one path component declared by a torrent.
func checkComponent(c string) error {
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
	// Windows-looking "a\\b" are each a traversal vector on one platform.
	if strings.ContainsAny(c, `/\`) {
		return fmt.Errorf("%w: path component %q contains a path separator", ErrUnsafePath, c)
	}

	if strings.Contains(c, ":") {
		return fmt.Errorf("%w: path component %q contains a drive or stream separator", ErrUnsafePath, c)
	}

	if trimmed := strings.TrimRight(c, ". "); trimmed != c {
		return fmt.Errorf("%w: path component %q ends in a dot or space", ErrUnsafePath, c)
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

// checkTorrentPath validates one file's declared path components and returns
// the absolute path it would be written to, confirmed to sit inside dest.
//
// name is the torrent's own name, which the storage layer uses as a directory
// for a multi-file torrent and as the file name for a single-file one — it is
// as attacker-controlled as the file paths themselves and is checked with
// exactly the same rules.
func checkTorrentPath(dest, name string, components []string) (string, error) {
	if err := checkComponent(name); err != nil {
		return "", fmt.Errorf("torrent name: %w", err)
	}

	parts := make([]string, 0, len(components)+1)
	parts = append(parts, name)

	for _, c := range components {
		if err := checkComponent(c); err != nil {
			return "", err
		}

		parts = append(parts, c)
	}

	full := filepath.Clean(filepath.Join(append([]string{dest}, parts...)...))
	if !containedIn(dest, full) {
		return "", fmt.Errorf("%w: %s escapes %s", ErrUnsafePath, strings.Join(parts, "/"), dest)
	}

	return full, nil
}
