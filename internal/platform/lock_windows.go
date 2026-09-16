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

// lockSentinelOffset is the byte range LockFileEx locks to establish
// exclusivity. Unlike flock(2) on macOS/Linux — advisory and whole-file, so
// it never blocks a read or write from another descriptor — Windows'
// LockFileEx is mandatory and byte-range: any other handle's I/O that
// overlaps a locked range fails with ERROR_LOCK_VIOLATION, even from
// within the same process. internal/lifecycle's lock file also stores the
// holder's PID as text starting at offset 0 (see writeHolderPID/
// readHolderPID), so locking that same range would make the PID
// unreadable by anyone but the lock's own handle the instant it was
// written — exactly the content AcquireLock needs a second instance to be
// able to read to name the holder in its "already running" message. Using
// a fixed offset far past any realistic file size keeps the lock's
// exclusivity semantics while leaving the PID content freely readable (and
// writable, for stale-lock recovery) by every handle. Locking a range
// beyond current EOF is valid on Windows; the range need not exist yet.
const lockSentinelOffset = 1 << 30

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
	overlapped.Offset = lockSentinelOffset
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
