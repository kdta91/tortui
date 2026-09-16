package lifecycle

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	bolterrors "go.etcd.io/bbolt/errors"

	"github.com/kdta91/tortui/internal/store"
)

// quarantineTimeFormat produces a sortable, filesystem-safe timestamp for
// quarantined store files.
const quarantineTimeFormat = "20060102T150405Z"

// OpenStore opens the bbolt-backed store at path. When the existing file
// exists but is corrupt or otherwise unreadable — a bad magic number, a
// truncated file from a crash mid-write, a decode failure in one of its
// records — it is renamed aside with a timestamp and a fresh, empty store
// is opened in its place, rather than blocking startup on it (AGENT.md
// §13, T-042). The returned notice is non-empty exactly when that
// recovery happened, so a caller can tell the user what happened and
// where the old file went.
//
// A store that is simply locked by another process (store.Open enforces a
// short bbolt open timeout) is never quarantined — that would destroy a
// live database out from under whoever holds it — and is returned as a
// plain error instead. In normal operation this should not be reachable:
// AcquireLock already refuses to start a second instance before OpenStore
// is ever called.
func OpenStore(path string, logger *slog.Logger) (*store.Store, string, error) {
	if logger == nil {
		logger = slog.Default()
	}

	st, err := store.Open(path)
	if err == nil {
		return st, "", nil
	}

	if errors.Is(err, bolterrors.ErrTimeout) {
		return nil, "", fmt.Errorf("lifecycle: store %s is locked by another process: %w", path, err)
	}

	if _, statErr := os.Stat(path); statErr != nil {
		// The file doesn't exist (or can't be stat'd) for a reason
		// unrelated to corrupt content — e.g. the parent directory itself
		// is unwritable. Quarantining would not help; surface the original
		// open error.
		return nil, "", fmt.Errorf("lifecycle: open store %s: %w", path, err)
	}

	quarantined := path + ".corrupt-" + time.Now().UTC().Format(quarantineTimeFormat)
	if renameErr := os.Rename(path, quarantined); renameErr != nil {
		return nil, "", fmt.Errorf("lifecycle: quarantine corrupt store %s: %w",
			path, errors.Join(renameErr, fmt.Errorf("original open error: %w", err)))
	}

	logger.Warn("lifecycle: store file was unreadable, quarantined and recreated empty",
		"original", path, "quarantined", quarantined, "error", err)

	fresh, freshErr := store.Open(path)
	if freshErr != nil {
		return nil, "", fmt.Errorf("lifecycle: recreate store %s after quarantining corrupt file: %w", path, freshErr)
	}

	notice := fmt.Sprintf(
		"your data file was unreadable and has been moved to %s; a fresh one was created (original error: %v)",
		quarantined, err,
	)
	return fresh, notice, nil
}
