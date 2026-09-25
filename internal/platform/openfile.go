package platform

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
)

// ErrOutsideRoots reports a path OpenFile or RevealFile refused because,
// once every symlink in it is resolved, it does not sit inside any of the
// known destination roots it was checked against (AGENT.md §6.11, §6.12).
var ErrOutsideRoots = errors.New("platform: path is outside every known destination root")

// ErrUnsafeOpenPath reports a path OpenFile or RevealFile refused before
// even resolving it: empty, relative, or carrying a byte no filesystem path
// handed to an OS launcher should ever contain.
var ErrUnsafeOpenPath = errors.New("platform: unsafe path")

// OpenFile opens the file at path with the OS's default application — the
// downloads screen's `o` (AGENT.md §7, T-073): `open` on macOS, `xdg-open`
// on Linux, `explorer` on Windows.
//
// path is resolved through every symlink and must then sit inside one of
// roots, themselves resolved the same way (AGENT.md §6.12: the set of known
// destination roots, never a single download directory). Anything else —
// a path that does not exist, a relative path, a symlink that escapes every
// root — is refused and logged, and no process is started. The launcher is
// handed the resolved path, never the original, and always through
// exec.Command with an argument slice, never a shell (AGENT.md §13).
func OpenFile(path string, roots []string) error {
	resolved, err := resolveInsideRoots(path, roots)
	if err != nil {
		return err
	}

	if err := openFile(resolved); err != nil {
		return fmt.Errorf("platform: open %q: %w", resolved, err)
	}

	return nil
}

// RevealFile shows the folder containing path in the OS file manager — the
// downloads screen's `f` (AGENT.md §7, T-073): `open -R` on macOS (the file
// selected in Finder), `xdg-open` on its parent directory on Linux (the
// freedesktop verb has no select-this-file form), `explorer /select,` on
// Windows. path is checked exactly as OpenFile checks it.
func RevealFile(path string, roots []string) error {
	resolved, err := resolveInsideRoots(path, roots)
	if err != nil {
		return err
	}

	if err := revealFile(resolved); err != nil {
		return fmt.Errorf("platform: reveal %q: %w", resolved, err)
	}

	return nil
}

// resolveInsideRoots returns path with every symlink resolved, after
// confirming that resolved form exists and sits strictly inside at least one
// of roots (each resolved through its own symlinks too, so a download
// directory that is itself a symlink to another drive still works, while a
// symlink planted inside a torrent's tree that points elsewhere does not).
// Every refusal is logged at warn level, then returned.
func resolveInsideRoots(path string, roots []string) (string, error) {
	resolved, err := resolveTarget(path)
	if err != nil {
		slog.Default().Warn("platform: refused to launch for an unsafe path", "path", path, "error", err)
		return "", err
	}

	for _, root := range roots {
		resolvedRoot, ok := resolveRoot(root)
		if !ok {
			continue
		}

		if strictlyInside(resolvedRoot, resolved) {
			return resolved, nil
		}
	}

	refusal := fmt.Errorf("%w: %s", ErrOutsideRoots, resolved)
	slog.Default().Warn("platform: refused to launch for a path outside every known destination root",
		"path", path, "resolved", resolved, "roots", roots)

	return "", refusal
}

// resolveTarget validates path's shape and resolves it through every
// symlink. The target must exist: there is nothing to open otherwise, and
// EvalSymlinks can only see through links that are actually on disk.
func resolveTarget(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%w: empty path", ErrUnsafeOpenPath)
	}

	// A NUL truncates the path at the syscall boundary, and no launcher
	// argument has any business carrying a control character.
	if strings.ContainsFunc(path, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return "", fmt.Errorf("%w: %q contains a control character", ErrUnsafeOpenPath, path)
	}

	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: %q is not an absolute path", ErrUnsafeOpenPath, path)
	}

	resolved, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("%w: resolve %q: %w", ErrUnsafeOpenPath, path, err)
	}

	return filepath.Clean(resolved), nil
}

// resolveRoot resolves one destination root through its symlinks. A root
// that is empty, relative, or does not exist is skipped (ok false) rather
// than failing the whole check: another root may still contain the target.
func resolveRoot(root string) (string, bool) {
	if strings.TrimSpace(root) == "" || strings.ContainsRune(root, 0) || !filepath.IsAbs(root) {
		return "", false
	}

	resolved, err := filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return "", false
	}

	return filepath.Clean(resolved), true
}

// strictlyInside reports whether candidate is a descendant of root — never
// root itself, and never a sibling whose name merely shares root's prefix
// (/data-evil against /data). Both must already be cleaned absolute paths.
func strictlyInside(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}

	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}

	return !filepath.IsAbs(rel)
}
