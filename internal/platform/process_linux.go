// Package platform holds the per-OS integration points that AGENT.md §14
// requires to stay behind build tags: nothing in this tree may branch on
// runtime.GOOS outside this package.
package platform

import (
	"errors"
	"syscall"
)

// ProcessAlive reports whether a process with the given pid is currently
// running on Linux. internal/lifecycle's single-instance lock (T-042) uses
// this to decide whether the PID recorded in a held lock file names a
// process still worth naming in the "already running" message — the lock
// itself is held with an OS-level flock (see TryLockFile), which is what
// actually makes staleness self-correcting; this is purely informational.
//
// It sends signal 0 via kill(2), which performs the permission and
// existence checks a real signal would without delivering anything — the
// standard POSIX way to probe liveness without side effects. pid <= 0 is
// never a real process and reports false with no error.
func ProcessAlive(pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}

	err := syscall.Kill(pid, 0)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, syscall.ESRCH):
		return false, nil
	case errors.Is(err, syscall.EPERM):
		// The process exists but is owned by another user — still alive.
		return true, nil
	default:
		return false, err
	}
}
