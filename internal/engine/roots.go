package engine

import (
	"fmt"
	"path/filepath"
)

// RootAdder is implemented by an Engine that checks every per-torrent
// destination against a set of known destination roots (AGENT.md §6.12).
// Like Queuer and Resumer it is optional rather than a change to the frozen
// Engine contract (AGENT.md §5): a caller discovers it with a type assertion.
//
// AddRoot is the only way that set grows after construction, and it is
// called only for a destination the user chose (T-074): a torrent's own
// contents never widen it.
type RootAdder interface {
	// AddRoot adds dir, which must be an absolute path, to the known
	// destination roots. Adding a root that is already known is a no-op.
	// A filesystem or volume root is refused: it would make every path
	// on that volume "contained" and so check nothing.
	AddRoot(dir string) error
}

// CheckDestinationRoot returns dir as a cleaned absolute path fit to be a
// destination root, or an error wrapping ErrUnsafePath: dir must be
// non-empty, absolute, free of NUL bytes, and not the root of a filesystem
// or volume ("/", `C:\`, `\\server\share\`).
func CheckDestinationRoot(dir string) (string, error) {
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("%w: destination %q is not an absolute path", ErrUnsafePath, dir)
	}

	abs, err := CleanAbsPath(dir)
	if err != nil {
		return "", err
	}

	if filepath.Dir(abs) == abs {
		return "", fmt.Errorf("%w: %s is the root of a drive; choose a folder", ErrUnsafePath, abs)
	}

	return abs, nil
}
