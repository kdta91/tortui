// Package platform holds the per-OS integration points that AGENT.md §14
// requires to stay behind build tags: nothing in this tree may branch on
// runtime.GOOS outside this package.
package platform

// FDLimits reports a process's open-file-descriptor limit before and after
// RaiseFDLimit attempted to raise it. Windows has no equivalent of
// POSIX's getrlimit(RLIMIT_NOFILE): handles are accounted against a
// process-wide (and system-wide) handle quota governed by available
// kernel memory, not a per-process soft/hard ulimit pair an application
// can read or raise. There is nothing to raise, so RaiseFDLimit is a
// documented no-op rather than a stub — Supported is false and every
// numeric field is -1, and doctor (T-055) reports that plainly instead of
// printing a fabricated number.
type FDLimits struct {
	// Soft is always -1 on Windows: there is no comparable value to read.
	Soft int64
	// Hard is always -1 on Windows: there is no comparable value to read.
	Hard int64
	// Raised is always -1 on Windows: nothing was raised.
	Raised int64
	// Supported is always false on Windows.
	Supported bool
}

// RaiseFDLimit reports that Windows has no per-process descriptor limit to
// raise. It always succeeds — there is nothing that can fail — and always
// returns FDLimits{Soft: -1, Hard: -1, Raised: -1, Supported: false}.
func RaiseFDLimit() (FDLimits, error) {
	return FDLimits{Soft: -1, Hard: -1, Raised: -1, Supported: false}, nil
}
