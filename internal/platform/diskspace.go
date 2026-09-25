package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// FreeSpace reports how many bytes an unprivileged process may still write
// to the filesystem holding path (T-034's free-space precheck). path need
// not exist yet — a per-torrent destination often does not until the first
// write — so the query is made against its nearest existing ancestor, which
// sits on the same filesystem the directory will be created on.
//
// The number is "available to the caller", not "free": on POSIX it excludes
// the blocks reserved for root, which a download can never use anyway.
func FreeSpace(path string) (uint64, error) {
	dir, err := nearestExisting(path)
	if err != nil {
		return 0, err
	}

	n, err := freeSpace(dir)
	if err != nil {
		return 0, fmt.Errorf("platform: free space of %s: %w", dir, err)
	}

	return n, nil
}

// nearestExisting walks up from path until it finds an entry that exists.
func nearestExisting(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("platform: resolve %q: %w", path, err)
	}

	dir := filepath.Clean(abs)
	for {
		_, statErr := os.Stat(dir)
		if statErr == nil {
			return dir, nil
		}

		if !errors.Is(statErr, os.ErrNotExist) {
			return "", fmt.Errorf("platform: stat %s: %w", dir, statErr)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("platform: no existing ancestor of %s: %w", abs, statErr)
		}

		dir = parent
	}
}
