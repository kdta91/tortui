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

// getrlimitFunc, setrlimitFunc, and sysctlUint32Func are the actual syscalls
// RaiseFDLimit drives. They are unexported package variables — not part of
// the public API — so a test in this package can substitute a mocked
// starting rlimit and observe the resulting Setrlimit call, mirroring
// internal/tui/theme.DetectOptions.SizeFunc's seam for the same reason: the
// real syscalls can't be steered to a chosen "before" value in a live test
// process (Go's own runtime already raises RLIMIT_NOFILE's soft limit to
// the hard ceiling in an init() before main() runs — see doc comment on
// RaiseFDLimit — so a real Getrlimit call here can never observe a low
// starting value to exercise the raise branch against).
var (
	getrlimitFunc    = unix.Getrlimit
	setrlimitFunc    = unix.Setrlimit
	sysctlUint32Func = unix.SysctlUint32
)

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
// soft limit to the hard ceiling, returning both the observed and the
// resulting values so doctor (T-055) can report them. It never returns an
// error for a failed *raise* — a sandboxed or restricted process may not be
// allowed to raise its own limit, and that is worth reporting, not fatal —
// only for a failure to even read the current limit.
//
// Since Go 1.19, the Go runtime itself raises RLIMIT_NOFILE's soft limit to
// the hard ceiling in an init() (src/syscall/rlimit.go) that runs before
// main() on every Unix binary. That means the very first Getrlimit call
// below — however early it runs inside this program — observes a value the
// runtime has already raised, not a genuine pre-raise baseline; the
// Setrlimit call this function makes is consequently a no-op in practice on
// a stock Go toolchain (target <= rlim.Cur is already true) and exists as a
// defensive fallback for anything that changes that runtime behaviour, not
// as the mechanism actually responsible for the high limit a user observes.
// doctor's Format function words its FD section accordingly rather than
// implying tortui performed a raise the Go runtime already made unnecessary
// (see DEC-096 in TASK_TRACKER.md).
func RaiseFDLimit() (FDLimits, error) {
	var rlim unix.Rlimit
	if err := getrlimitFunc(unix.RLIMIT_NOFILE, &rlim); err != nil {
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
		if capVal, err := sysctlUint32Func("kern.maxfilesperproc"); err == nil {
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
	if err := setrlimitFunc(unix.RLIMIT_NOFILE, &newLim); err != nil {
		// Best-effort: report the original limits rather than failing
		// doctor outright over a permission-restricted environment.
		return result, nil
	}

	result.Raised = int64(target)

	return result, nil
}
