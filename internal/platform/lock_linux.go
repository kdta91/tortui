// Package platform holds the per-OS integration points that AGENT.md §14
// requires to stay behind build tags: nothing in this tree may branch on
// runtime.GOOS outside this package.
package platform

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// TryLockFile attempts to take an exclusive, non-blocking advisory lock on
// f using flock(2) — the mechanism internal/lifecycle's single-instance
// lock (T-042) relies on for its correctness: the kernel releases the lock
// automatically when every file descriptor referencing it closes, including
// on a hard crash, so a stale lock from a dead process clears itself with
// no PID-liveness guesswork required.
//
// It reports ok=false, nil error when another open file description
// already holds the lock — including one held by this same process via a
// different os.File, which is what makes concurrent-acquisition tests
// possible without spawning a second process (flock(2): "an attempt to
// lock the file using [a different] file descriptor may be denied by a
// lock that the calling process has already placed via another file
// descriptor"). A non-nil error is reserved for an unexpected syscall
// failure.
func TryLockFile(f *os.File) (bool, error) {
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, syscall.EWOULDBLOCK):
		return false, nil
	default:
		return false, fmt.Errorf("flock: %w", err)
	}
}
