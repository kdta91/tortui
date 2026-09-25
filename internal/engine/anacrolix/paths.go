package anacrolix

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kdta91/tortui/internal/engine"
)

// ErrUnsafePath is engine.ErrUnsafePath: the component and containment rules
// live in internal/engine so every call site that creates, opens, reveals, or
// deletes a torrent's files applies the same ones (AGENT.md §6.11). It is
// re-exported here so callers of this package can test for it without a
// second import.
var ErrUnsafePath = engine.ErrUnsafePath

// ErrOutsideRoots reports a destination that does not sit inside any of the
// engine's known destination roots (AGENT.md §6.12).
var ErrOutsideRoots = errors.New("anacrolix: destination is outside every known destination root")

// cleanAbsPath is engine.CleanAbsPath.
func cleanAbsPath(p string) (string, error) { return engine.CleanAbsPath(p) }

// containedIn is engine.ContainedIn.
func containedIn(root, candidate string) bool { return engine.ContainedIn(root, candidate) }

// checkComponent is engine.CheckPathComponent.
func checkComponent(c string) error { return engine.CheckPathComponent(c) }

// checkTorrentPath is engine.CheckTorrentPath.
func checkTorrentPath(dest, name string, components []string) (string, error) {
	return engine.CheckTorrentPath(dest, name, components)
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

// resolveSymlinks resolves p to its real, symlink-free form as far as an
// existing filesystem entry allows. p need not exist: EvalSymlinks is walked
// up the ancestor chain until it finds a component that does exist, that
// prefix is resolved, and the remaining components — which by definition
// cannot themselves be symlinks, since nothing has been created there yet —
// are rejoined onto it literally.
//
// This is what lets a delete-time containment check see through a symlinked
// *directory component*, not just a symlinked leaf (AGENT.md §6.11, §6.12):
// a destination root or an intermediate directory swapped for a symlink
// after Add would otherwise defeat a check that only resolved the final
// path element.
func resolveSymlinks(p string) (string, error) {
	clean := filepath.Clean(p)

	var suffix []string

	dir := clean
	for {
		resolved, err := filepath.EvalSymlinks(dir)
		if err == nil {
			full := resolved
			for i := len(suffix) - 1; i >= 0; i-- {
				full = filepath.Join(full, suffix[i])
			}

			return filepath.Clean(full), nil
		}

		if !os.IsNotExist(err) {
			return "", fmt.Errorf("resolve symlinks in %q: %w", p, err)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Nothing on this path exists at all; there is nothing left
			// to resolve.
			return clean, nil
		}

		suffix = append(suffix, filepath.Base(dir))
		dir = parent
	}
}

// containedInRoot reports whether target sits inside root once both are
// resolved through any symlinks present in their existing path components.
// It never treats root itself as a valid target: Remove must delete only a
// torrent's own files/dir under a root, never the root (AGENT.md §6.12).
func containedInRoot(root, target string) (bool, error) {
	resolvedRoot, err := resolveSymlinks(root)
	if err != nil {
		return false, err
	}

	resolvedTarget, err := resolveSymlinks(target)
	if err != nil {
		return false, err
	}

	if resolvedTarget == resolvedRoot {
		return false, nil
	}

	return containedIn(resolvedRoot, resolvedTarget), nil
}
