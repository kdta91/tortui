// Package platform holds the per-OS integration points that AGENT.md §14
// requires to stay behind build tags: nothing in this tree may branch on
// runtime.GOOS outside this package.
package platform

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// fallbackFDCeiling is used when the kernel reports the hard limit as
// RLIM_INFINITY and no more specific ceiling (kern.maxfilesperproc) can be
// read — a sane, generous cap rather than attempting to set an unbounded
// value, which setrlimit(2) rejects on Darwin.
const fallbackFDCeiling = 1 << 20

// FDLimits reports a process's open-file-descriptor limit before and after
// RaiseFDLimit attempted to raise it (AGENT.md §13: "macOS file descriptor
// limits. The soft ulimit -n is often 256 ... Raise the soft limit to the
// hard limit at startup on darwin and log the result.").
type FDLimits struct {
	// Soft is the soft RLIMIT_NOFILE value observed before any raise
	// attempt.
	Soft int64
	// Hard is the ceiling the soft limit was raised towards. On Darwin the
	// kernel often reports RLIM_INFINITY here; in that case Hard is the
	// more specific kern.maxfilesperproc ceiling (or fallbackFDCeiling if
	// that sysctl could not be read), never the literal "infinite" value.
	Hard int64
	// Raised is the soft limit after RaiseFDLimit ran. Equal to Soft when
	// the limit was already at its ceiling or the raise attempt failed.
	Raised int64
	// Supported is true on every platform with a comparable per-process
	// descriptor limit. Always true on Darwin.
	Supported bool
}

// RaiseFDLimit reads the process's current RLIMIT_NOFILE and raises the
// soft limit to the hard ceiling, returning both the original and the
// resulting values so doctor (T-055) can report them. It never returns an
// error for a failed *raise* — a sandboxed or restricted process may not be
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
		if capVal, err := unix.SysctlUint32("kern.maxfilesperproc"); err == nil {
			target = uint64(capVal)
		} else {
			target = fallbackFDCeiling
		}
		result.Hard = int64(target)
	}

	if target <= rlim.Cur {
		return result, nil
	}

	newLim := unix.Rlimit{Cur: target, Max: rlim.Max}
	if err := unix.Setrlimit(unix.RLIMIT_NOFILE, &newLim); err != nil {
		// Best-effort: report the original limits rather than failing
		// doctor outright over a permission-restricted environment.
		return result, nil
	}

	result.Raised = int64(target)

	return result, nil
}
