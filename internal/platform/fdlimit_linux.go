// Package platform holds the per-OS integration points that AGENT.md §14
// requires to stay behind build tags: nothing in this tree may branch on
// runtime.GOOS outside this package.
package platform

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// fallbackFDCeiling is used when the kernel reports the hard limit as
// RLIM_INFINITY — a sane, generous cap rather than attempting to set an
// unbounded value.
const fallbackFDCeiling = 1 << 20

// FDLimits reports a process's open-file-descriptor limit before and after
// RaiseFDLimit attempted to raise it. AGENT.md §13 calls this out as a
// Darwin hazard specifically, but a distro with a low default soft limit
// (some container base images ship 1024) benefits from the same raise, so
// Linux gets the identical treatment.
type FDLimits struct {
	// Soft is the soft RLIMIT_NOFILE value observed before any raise
	// attempt.
	Soft int64
	// Hard is the ceiling the soft limit was raised towards. When the
	// kernel reports RLIM_INFINITY, Hard is fallbackFDCeiling instead of
	// the literal "infinite" value, since that is what RaiseFDLimit
	// actually requests.
	Hard int64
	// Raised is the soft limit after RaiseFDLimit ran. Equal to Soft when
	// the limit was already at its ceiling or the raise attempt failed.
	Raised int64
	// Supported is true on every platform with a comparable per-process
	// descriptor limit. Always true on Linux.
	Supported bool
}

// RaiseFDLimit reads the process's current RLIMIT_NOFILE and raises the
// soft limit to the hard ceiling, returning both the original and the
// resulting values so doctor (T-055) can report them. It never returns an
// error for a failed *raise* — a container or restricted process may not be
// allowed to raise its own limit, and that is worth reporting, not fatal —
// only for a failure to even read the current limit.
func RaiseFDLimit() (FDLimits, error) {
	var rlim unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &rlim); err != nil {
		return FDLimits{}, fmt.Errorf("getrlimit(RLIMIT_NOFILE): %w", err)
	}

	result := FDLimits{
		Soft:      int64(rlim.Cur),
		Hard:      int64(rlim.Max),
		Raised:    int64(rlim.Cur),
		Supported: true,
	}

	target := rlim.Max
	if target == unix.RLIM_INFINITY {
		target = fallbackFDCeiling
		result.Hard = int64(target)
	}

	if target <= rlim.Cur {
		return result, nil
	}

	newLim := unix.Rlimit{Cur: target, Max: rlim.Max}
	if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &newLim); err != nil {
		return result, nil
	}

	result.Raised = int64(target)

	return result, nil
}
