// Package platform holds the per-OS integration points that AGENT.md §14
// requires to stay behind build tags: nothing in this tree may branch on
// runtime.GOOS outside this package.
package platform

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

// stillActive is the exit code Windows reports via GetExitCodeProcess for
// a process that has not yet terminated.
const stillActive = 259

// ProcessAlive reports whether a process with the given pid is currently
// running on Windows. internal/lifecycle's single-instance lock (T-042)
// uses this to decide whether the PID recorded in a held lock file names a
// process still worth naming in the "already running" message — the lock
// itself is held with an OS-level LockFileEx (see TryLockFile), which is
// what actually makes staleness self-correcting; this is purely
// informational.
//
// pid <= 0 is never a real process and reports false with no error. A pid
// that no longer exists (OpenProcess fails) also reports false, not an
// error — that is the expected, common case for a crashed process.
func ProcessAlive(pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}

	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return false, nil
		}
		// Access denied still implies the process exists (just owned by
		// another user/session) — treat it as alive rather than erroring.
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return true, nil
		}
		return false, nil
	}
	defer windows.CloseHandle(handle)

	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return false, fmt.Errorf("GetExitCodeProcess: %w", err)
	}

	return exitCode == stillActive, nil
}
