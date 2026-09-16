// Package platform holds the per-OS integration points that AGENT.md §14
// requires to stay behind build tags: nothing in this tree may branch on
// runtime.GOOS outside this package.
package platform

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// TryLockFile attempts to take an exclusive, non-blocking advisory lock on
// f using LockFileEx — Windows's equivalent of flock(2) and the mechanism
// internal/lifecycle's single-instance lock (T-042) relies on for its
// correctness: Windows releases the lock automatically when the handle
// that holds it closes, including when the owning process is killed, so a
// stale lock from a dead process clears itself with no PID-liveness
// guesswork required.
//
// It reports ok=false, nil error when another handle already holds the
// lock — including one held by this same process via a different os.File,
// which is what makes concurrent-acquisition tests possible without
// spawning a second process. A non-nil error is reserved for an unexpected
// syscall failure.
func TryLockFile(f *os.File) (bool, error) {
	handle := windows.Handle(f.Fd())

	var overlapped windows.Overlapped
	err := windows.LockFileEx(
		handle,
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0,
		1, 0,
		&overlapped,
	)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, windows.ERROR_LOCK_VIOLATION):
		return false, nil
	default:
		return false, fmt.Errorf("LockFileEx: %w", err)
	}
}
